// Package quota 配额预检（Redis Lua 原子预扣，防超卖）。
package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/auth"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"

	oai "ykt.dev/aisaas/pkg/openaiclient"
)

// Guard 配额守卫。
type Guard struct {
	Rdb     *redisx.Client
	Enabled bool
}

// NewGuard 构造。
func NewGuard(rdb *redisx.Client, enabled bool) *Guard {
	return &Guard{Rdb: rdb, Enabled: enabled}
}

// EstimateChat 估算输入 token（字符/3 中英混合近似 + 每条消息开销）。
func EstimateChat(msgs []oai.Message) int64 {
	var chars int
	for _, m := range msgs {
		chars += len(m.Content) + 8
	}
	tok := int64(chars / 3)
	if tok < 1 {
		tok = 1
	}
	if tok > 2048 {
		tok = 2048 // 预扣上限
	}
	return tok
}

// Precheck LLM 输入维度预扣。
func (g *Guard) Precheck(ctx context.Context, estimated int64) bool {
	return g.PrecheckDim(ctx, redisx.DimLLMTokensIn, estimated)
}

// PrecheckDim 指定维度预扣。返回 false = 配额不足。
func (g *Guard) PrecheckDim(ctx context.Context, dim string, estimated int64) bool {
	if !g.Enabled || auth.UnlimitedFrom(ctx) {
		return true
	}
	tid, ok := tenant.FromSafe(ctx)
	if !ok {
		return false
	}
	remaining, err := g.Rdb.QuotaDeduct(ctx, tid, dim, estimated)
	if err != nil {
		if errors.Is(err, redisx.ErrQuotaExceeded) {
			// 抽样 1/100 写审计日志
			if rand.Intn(100) == 0 {
				audit.Record(ctx, audit.AuditEntry{
					TenantID: tid, ActorType: "apikey",
					Action: "quota.check", ResourceType: "quota",
					ResourceID: dim, Result: "failure",
					ErrorCode: "40201", ErrorMessage: "配额不足",
					ActionDetail: map[string]any{"dim": dim, "estimated": estimated},
				})
			}
			return false
		}
		// Redis 故障：放行（最终一致，事后对账）
	}
	// 抽样 1/100 写审计日志
	if rand.Intn(100) == 0 {
		audit.Record(ctx, audit.AuditEntry{
			TenantID: tid, ActorType: "apikey",
			Action: "quota.check", ResourceType: "quota",
			ResourceID: dim, Result: "success",
			ActionDetail: map[string]any{"dim": dim, "estimated": estimated, "remaining": remaining},
		})
	}
	return true
}

// Middleware 路由级预检。
func (g *Guard) Middleware(est func(*gin.Context) int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !g.Enabled || auth.UnlimitedFrom(c.Request.Context()) {
			c.Next()
			return
		}
		tid, ok := tenant.FromSafe(c.Request.Context())
		if !ok {
			web.AbortOpenAI(c, errs.New(errs.TenantContextLost))
			return
		}
		if _, err := g.Rdb.QuotaDeduct(c.Request.Context(), tid, redisx.DimLLMTokensIn, est(c)); err != nil {
			if errors.Is(err, redisx.ErrQuotaExceeded) {
				web.AbortOpenAI(c, errs.New(errs.QuotaExceeded))
				return
			}
		}
		c.Next()
	}
}

// Refund 配额退款（DecrBy Redis used 计数器）。
// amount 必须 > 0。
// 实现 pending 标记机制：写 pending key → incr 配额 → 删除 pending key。
// 失败时 pending key 保留，供后续 reconciler 处理（P1 阶段）。
func (g *Guard) Refund(ctx context.Context, tenantID int64, dim string, amount int64) error {
	if amount <= 0 {
		return nil
	}

	// 生成 pending key（24h TTL）
	pendingKey := fmt.Sprintf("aisaas:quota:refund:%d:%s:%d", tenantID, dim, time.Now().UnixNano())
	if err := g.Rdb.Set(ctx, pendingKey, "1", 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("mark pending: %w", err)
	}

	// 执行退款（IncrBy）
	ym := time.Now().Format("200601")
	if err := g.Rdb.IncrBy(ctx, redisx.KeyQuotaUsed(tenantID, dim, ym), amount).Err(); err != nil {
		// 退款失败，pending key 保留供 reconciler 处理
		slog.Error("refund incrby failed, pending remains for reconciler",
			"key", pendingKey, "tenantID", tenantID, "dim", dim, "amount", amount, "err", err)
		return fmt.Errorf("incr quota: %w", err)
	}

	// 成功，删除 pending key
	if err := g.Rdb.Del(ctx, pendingKey).Err(); err != nil {
		slog.Warn("refund pending cleanup failed", "key", pendingKey, "err", err)
	}
	return nil
}
