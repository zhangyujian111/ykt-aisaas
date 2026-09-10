package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/tenantm/persona"
)

// Repo session 数据访问层。
type Repo struct {
	DB *gorm.DB
}

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{DB: db}
}

// Create 创建会话记录。
func (r *Repo) Create(ctx context.Context, do *SessionDO) error {
	return r.DB.WithContext(ctx).Create(do).Error
}

// GetByID 按 ID 查询会话（租户过滤由插件保证）。
func (r *Repo) GetByID(ctx context.Context, id int64) (*SessionDO, error) {
	var do SessionDO
	err := r.DB.WithContext(ctx).
		Where("id = ? AND isDeleted = 0", id).
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "会话不存在")
	}
	return &do, err
}

// GetByIDGlobal 按 ID 全局查询（内部使用，不依赖租户上下文）。
func (r *Repo) GetByIDGlobal(ctx context.Context, id int64) (*SessionDO, error) {
	var do SessionDO
	err := r.DB.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(SessionDO{}.TableName()).
		Where("id = ? AND isDeleted = 0", id).
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "会话不存在")
	}
	return &do, err
}

// ListByDevice 按设备查询活跃会话列表（按创建时间倒序，跳过租户 hook）。
func (r *Repo) ListByDevice(ctx context.Context, deviceID string, limit int) ([]*SessionDO, error) {
	var out []*SessionDO
	err := r.DB.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Where("deviceId = ? AND isDeleted = 0 AND status IN (?,?)", deviceID, StatusCreating, StatusActive).
		Order("createTime DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// UpdateEnd 结束会话：填充 actualCost、endTime、status。
func (r *Repo) UpdateEnd(ctx context.Context, id int64, actualCost string, status int8, quotaUsed int64) error {
	now := gorm.Expr("NOW()")
	return r.DB.WithContext(ctx).Model(&SessionDO{}).Where("id = ?", id).Updates(map[string]any{
		"actualCost": actualCost,
		"status":     status,
		"quotaUsed":  quotaUsed,
		"endTime":    now,
	}).Error
}

// ---- Service ----

// Service 会话业务层。
type Service struct {
	repo *Repo
	rdb  *redisx.Client
	cfg  Config
}

// NewService 构造。
func NewService(repo *Repo, rdb *redisx.Client, cfg Config) *Service {
	if cfg.DefaultQuotaInitial <= 0 {
		cfg.DefaultQuotaInitial = 5000
	}
	if cfg.MaxHistoryLimit <= 0 {
		cfg.MaxHistoryLimit = 100
	}
	return &Service{repo: repo, rdb: rdb, cfg: cfg}
}

// ListByDevice 按设备 ID 查询会话列表（供 Handler 使用，跳过租户 hook）。
func (s *Service) ListByDevice(ctx context.Context, deviceID string, limit int) ([]*SessionDO, error) {
	return s.repo.ListByDevice(ctx, deviceID, limit)
}

// ---- Handler ----

// Handler 会话 HTTP 处理器。
type Handler struct {
	Svc            *Service
	PersonaBindSvc *persona.Service
}

// NewHandler 构造。
func NewHandler(svc *Service, bindSvc *persona.Service) *Handler {
	return &Handler{Svc: svc, PersonaBindSvc: bindSvc}
}

// ---- 内部辅助 ----

// genSessionID 生成雪花 ID 作为会话 ID。
func genSessionID() int64 {
	return ids.Next()
}

// formatSessionID 将 int64 会话 ID 转为 API 字符串。
func formatSessionID(id int64) string {
	return strconv.FormatInt(id, 0)
}

// parseSessionID 解析 API 字符串为 int64。
func parseSessionID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid session id: %s", s)
	}
	return id, nil
}

// writeAudit 写审计日志薄封装。
func writeAudit(ctx context.Context, action, resourceID string, result string, tenantID int64, detail map[string]any) {
	audit.Record(ctx, audit.AuditEntry{
		TenantID:     tenantID,
		ActorType:    "device",
		Action:       action,
		ResourceType: "session",
		ResourceID:   resourceID,
		Result:       result,
		ActionDetail: detail,
	})
}

// extractTenant 从 context 提取租户 ID（安全）。
func extractTenant(ctx context.Context) (int64, error) {
	tid, ok := tenant.FromSafe(ctx)
	if !ok {
		return 0, errs.New(errs.TenantContextLost)
	}
	return tid, nil
}

// validDimension 校验配额维度。
func validDimension(dim string) bool {
	switch dim {
	case redisx.DimLLMTokensIn, redisx.DimLLMTokensOut, redisx.DimTTSChars, redisx.DimASRSeconds:
		return true
	}
	return false
}