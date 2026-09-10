// Package auth 提供 API Key 校验与鉴权中间件。
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/audit"
	pcrypto "ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
)

// ApiKeyBO 鉴权后的 Key 快照。
type ApiKeyBO struct {
	ID        int64
	TenantID  int64
	Scope     []string // ["llm","tts","asr","rag","mcp"]
	IPAllow   []string // CIDR 白名单；空 = 不限
	ExpiresAt *time.Time
	Status    int8 // 1 正常
	Unlimited bool // 内部超级租户：跳过配额
}

// HasScope 是否具备权限域。
func (b *ApiKeyBO) HasScope(s string) bool {
	if len(b.Scope) == 0 {
		return true // 未声明 scope = 全量（向后兼容）
	}
	for _, v := range b.Scope {
		if v == s || v == "*" {
			return true
		}
	}
	return false
}

// IPAllowed 来源 IP 校验。
func (b *ApiKeyBO) IPAllowed(ip string) bool {
	if len(b.IPAllow) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	for _, cidr := range b.IPAllow {
		if _, nw, err := net.ParseCIDR(cidr); err == nil && nw.Contains(parsed) {
			return true
		}
		if cidr == ip { // 兼容裸 IP
			return true
		}
	}
	return false
}

// keyRow 直接映射 ykt_aisaas_apikey（鉴权专用最小列集；表在租户豁免名单外，
// 但本查询按 sha256 唯一键定位行、不依赖租户过滤，故走 Session 裸查询）。
type keyRow struct {
	ID        int64      `gorm:"column:id"`
	TenantID  int64      `gorm:"column:tenantId"`
	Scope     string     `gorm:"column:scope"`
	IPAllow   string     `gorm:"column:ipWhitelist"`
	ExpiresAt *time.Time `gorm:"column:expiresAt"`
	Status    int8       `gorm:"column:status"`
}

// TenantStatus 读取租户状态所需的最小接口（避免循环依赖，由 main 注入实现）。
type TenantStatus func(ctx context.Context, tenantID int64) (int8, error)

// Service API Key 校验服务：Redis 缓存 → MySQL。
type Service struct {
	db       *gorm.DB
	rdb      *redisx.Client
	tenantOf TenantStatus
}

// NewService 构造。
func NewService(db *gorm.DB, rdb *redisx.Client, tenantOf TenantStatus) *Service {
	return &Service{db: db, rdb: rdb, tenantOf: tenantOf}
}

const cacheTTL = 30 * time.Minute

// Validate 校验明文 Key → ApiKeyBO。
func (s *Service) Validate(ctx context.Context, plain string) (*ApiKeyBO, error) {
	hash := pcrypto.HashKey(plain)

	// 1) Redis 反查索引 → BO JSON 由调用侧缓存 tenant+hash 两份
	if bo := s.cacheGet(ctx, hash); bo != nil {
		return bo, s.postCheck(ctx, bo)
	}

	// 2) DB 查询（按唯一键，无租户上下文——鉴权发生在上下文注入之前）
	var row keyRow
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_apikey").
		Select("id", "tenantId", "scope", "ipWhitelist", "expiresAt", "status").
		Where("apiKeyHash = ? AND isDeleted = 0", hash).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.InvalidAPIKey)
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}

	bo := &ApiKeyBO{
		ID:        row.ID,
		TenantID:  row.TenantID,
		Scope:     parseJSONArr(row.Scope),
		IPAllow:   parseJSONArr(row.IPAllow),
		ExpiresAt: row.ExpiresAt,
		Status:    row.Status,
	}
	if err := s.postCheck(ctx, bo); err != nil { // 校验通过才缓存，避免坏 BO 滞留 30min
		return nil, err
	}
	s.cachePut(ctx, hash, bo)
	return bo, nil
}

func (s *Service) postCheck(ctx context.Context, bo *ApiKeyBO) error {
	if bo.Status != 1 {
		return errs.New(errs.InvalidAPIKey, "API Key 已禁用")
	}
	if bo.ExpiresAt != nil && bo.ExpiresAt.Before(time.Now()) {
		return errs.New(errs.APIKeyExpired)
	}
	if s.tenantOf != nil {
		st, err := s.tenantOf(ctx, bo.TenantID)
		if err != nil {
			return errs.Wrap(errs.Internal, err)
		}
		if st != 1 {
			return errs.New(errs.TenantFrozen)
		}
	}
	return nil
}

// InvalidateCache 禁用/删除 Key 后失效缓存。
func (s *Service) InvalidateCache(ctx context.Context, plainOrHash string) {
	hash := plainOrHash
	if strings.HasPrefix(plainOrHash, pcrypto.KeyPrefix) {
		hash = pcrypto.HashKey(plainOrHash)
	}
	s.rdb.Del(ctx, redisx.KeyAPIKeyByHash(hash))
}

// ---- 缓存（简单 JSON 编码；hash 索引一份即可）----

func (s *Service) cacheGet(ctx context.Context, hash string) *ApiKeyBO {
	raw, err := s.rdb.Get(ctx, redisx.KeyAPIKeyByHash(hash)).Result()
	if err != nil || raw == "" {
		return nil
	}
	var bo ApiKeyBO
	if err := jsonUnmarshal([]byte(raw), &bo); err != nil {
		return nil
	}
	return &bo
}

func (s *Service) cachePut(ctx context.Context, hash string, bo *ApiKeyBO) {
	if b, err := jsonMarshal(bo); err == nil {
		s.rdb.Set(ctx, redisx.KeyAPIKeyByHash(hash), b, cacheTTL)
	}
}

// ---- 中间件 ----

// actor 注入的调用者信息。
type actorCtxKey struct{}

// Actor 调用者身份。
type Actor struct {
	Type string // apikey / internal / device / user
	ID   int64
	Name string // 设备身份时为 deviceId
}

// ActorFrom 从 ctx 取 Actor。
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorCtxKey{}).(Actor)
	return a, ok
}

// Middleware API Key 鉴权中间件。scope 为空则不校验权限域。
func Middleware(svc *Service, scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		plain := extractKey(c)
		if plain == "" {
			audit.Record(c.Request.Context(), audit.AuditEntry{
				ActorType: "apikey", ActorIP: c.ClientIP(),
				Action: "auth.fail", Result: "failure",
				ErrorCode: "40101", ErrorMessage: "缺少 Authorization: Bearer sk-aisaas-...",
			})
			web.AbortOpenAI(c, errs.New(errs.InvalidAPIKey, "缺少 Authorization: Bearer sk-aisaas-..."))
			return
		}
		bo, err := svc.Validate(c.Request.Context(), plain)
		if err != nil {
			audit.Record(c.Request.Context(), audit.AuditEntry{
				ActorType: "apikey", ActorIP: c.ClientIP(),
				Action: "auth.fail", Result: "failure",
				ErrorCode: errCode(err), ErrorMessage: err.Error(),
				ActionDetail: map[string]any{"keyPrefix": safeKeyPrefix(plain)},
			})
			web.AbortOpenAI(c, err)
			return
		}
		if scope != "" && !bo.HasScope(scope) {
			audit.Record(c.Request.Context(), audit.AuditEntry{
				ActorType: "apikey", ActorID: fmt.Sprintf("%d", bo.ID),
				TenantID: bo.TenantID, ActorIP: c.ClientIP(),
				Action: "auth.fail", Result: "failure",
				ErrorCode: "40302", ErrorMessage: "API Key 无 " + scope + " 权限",
			})
			web.AbortOpenAI(c, errs.New(errs.Forbidden, "API Key 无 "+scope+" 权限"))
			return
		}
		if !bo.IPAllowed(c.ClientIP()) {
			audit.Record(c.Request.Context(), audit.AuditEntry{
				ActorType: "apikey", ActorID: fmt.Sprintf("%d", bo.ID),
				TenantID: bo.TenantID, ActorIP: c.ClientIP(),
				Action: "auth.fail", Result: "failure",
				ErrorCode: "40103", ErrorMessage: "IP 不在白名单",
			})
			web.AbortOpenAI(c, errs.New(errs.IPNotAllowed))
			return
		}

		// 鉴权成功
		audit.Record(c.Request.Context(), audit.AuditEntry{
			TenantID: bo.TenantID, ActorType: "apikey", ActorID: fmt.Sprintf("%d", bo.ID),
			ActorIP: c.ClientIP(), Action: "auth.success", Result: "success",
		})

		ctx := tenant.With(c.Request.Context(), bo.TenantID)
		ctx = context.WithValue(ctx, actorCtxKey{}, Actor{Type: "apikey", ID: bo.ID})
		ctx = context.WithValue(ctx, unlimitedKey{}, bo.Unlimited)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// InternalMiddleware 内部超级租户鉴权：X-Internal-Token + 本机来源。
func InternalMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Internal-Token") != token {
			web.Abort(c, errs.New(errs.TokenInvalid, "internal token 错误"))
			return
		}
		// 同物理机约束：仅允许 loopback 或 RFC1918 私有网（Docker/K8s 网络）
		ip := c.ClientIP()
		if !isAllowedInternalIP(ip) {
			web.Abort(c, errs.New(errs.IPNotAllowed, "internal API 仅限本机或私有网调用"))
			return
		}
		ctx := tenant.With(c.Request.Context(), 1) // 内部租户 ID = 1
		ctx = context.WithValue(ctx, actorCtxKey{}, Actor{Type: "internal", ID: 0})
		ctx = context.WithValue(ctx, unlimitedKey{}, true)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

type unlimitedKey struct{}

// isAllowedInternalIP \u68c0\u67e5 IP \u662f\u5426\u5728\u5141\u8bb8\u7684\u5185\u90e8\u8c03\u7528\u8303\u56f4\uff08loopback + RFC1918 \u79c1\u6709\u7f51\uff09\u3002
func isAllowedInternalIP(ip string) bool {
	if ip == "" {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	if parsed.IsLoopback() {
		return true
	}
	if parsed.IsPrivate() {
		return true
	}
	return false
}

// UnlimitedFrom ctx 中读取"不限配额"标记。
func UnlimitedFrom(ctx context.Context) bool {
	v, _ := ctx.Value(unlimitedKey{}).(bool)
	return v
}

func extractKey(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if q := c.Query("api_key"); q != "" { // WebSocket 场景
		return q
	}
	return ""
}

func parseJSONArr(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var _ = slog.Info // 保持 import（简化版未用 slog 的编译保险）
var _ = fmt.Sprintf
var _ = http.StatusOK

// errCode 从 error 提取错误码字符串。
func errCode(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return fmt.Sprintf("%d", e.Code)
	}
	return "unknown"
}

// safeKeyPrefix 安全提取 Key 前缀（脱敏）。
func safeKeyPrefix(plain string) string {
	if len(plain) > 16 {
		return plain[:16]
	}
	return plain
}
