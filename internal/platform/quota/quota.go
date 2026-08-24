// Package quota 配额预检（Redis Lua 原子预扣，防超卖）。
package quota

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

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
	if _, err := g.Rdb.QuotaDeduct(ctx, tid, dim, estimated); err != nil {
		if errors.Is(err, redisx.ErrQuotaExceeded) {
			return false
		}
		// Redis 故障：放行（最终一致，事后对账）
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
