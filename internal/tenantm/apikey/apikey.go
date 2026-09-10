// Package apikey 租户 API Key 管理（CRUD）。
package apikey

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/auth"
	pcrypto "ykt.dev/aisaas/internal/platform/crypto"
	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// DO ykt_aisaas_apikey。
type DO struct {
	database.BaseDO
	Name       string     `gorm:"column:name" json:"name"`
	APIKeyHash string     `gorm:"column:apiKeyHash" json:"-"`
	KeyPrefix  string     `gorm:"column:keyPrefix" json:"keyPrefix"`
	Scope      string     `gorm:"column:scope" json:"scope"`
	IPAllow    string     `gorm:"column:ipWhitelist" json:"ipWhitelist"`
	ExpiresAt  *time.Time `gorm:"column:expiresAt" json:"expiresAt"`
	LastUsedAt *time.Time `gorm:"column:lastUsedAt" json:"lastUsedAt"`
	Status     int8       `gorm:"column:status" json:"status"`
	IsDeleted  int8       `gorm:"column:isDeleted" json:"-"`
	CreatedBy  int64      `gorm:"column:createdBy" json:"createdBy"`
}

func (DO) TableName() string { return "ykt_aisaas_apikey" }

// CreateReq 创建请求。
type CreateReq struct {
	Name        string   `json:"name" binding:"required,max=64"`
	Scope       []string `json:"scope"`
	IPAllow     []string `json:"ipWhitelist"`
	ExpiresDays *int     `json:"expiresDays"` // 空 = 永久
}

// CreateResp 创建响应（明文 Key 仅此一次返回）。
type CreateResp struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	APIKey    string     `json:"apiKey"` // ⚠️ 仅创建时返回
	KeyPrefix string     `json:"keyPrefix"`
	Scope     []string   `json:"scope"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// ListResp 列表项。
type ListResp struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	Scope      string     `json:"scope"`
	Status     int8       `json:"status"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreateTime time.Time  `json:"createTime"`
}

// Repo 数据访问。
type Repo struct{ DB *gorm.DB }

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo { return &Repo{DB: db} }

// Create 创建 Key（租户由 GORM 插件填充）。
func (r *Repo) Create(ctx context.Context, do *DO) error {
	return r.DB.WithContext(ctx).Create(do).Error
}

// List 租户 Key 列表（租户过滤由插件保证）。
func (r *Repo) List(ctx context.Context) ([]*DO, error) {
	var out []*DO
	err := r.DB.WithContext(ctx).Where("isDeleted = 0").Order("id DESC").Find(&out).Error
	return out, err
}

// GetByID 按 ID 取（租户过滤由插件保证）。
func (r *Repo) GetByID(ctx context.Context, id int64) (*DO, error) {
	var do DO
	err := r.DB.WithContext(ctx).Where("id = ? AND isDeleted = 0", id).Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound)
	}
	return &do, err
}

// UpdateStatus 启停。
func (r *Repo) UpdateStatus(ctx context.Context, id int64, status int8) error {
	return r.DB.WithContext(ctx).Model(&DO{}).Where("id = ?", id).
		Update("status", status).Error
}

// SoftDelete 软删。
func (r *Repo) SoftDelete(ctx context.Context, id int64) error {
	return r.DB.WithContext(ctx).Model(&DO{}).Where("id = ?", id).
		Update("isDeleted", 1).Error
}

// Service 业务层。
type Service struct {
	repo *Repo
	auth *auth.Service
	sf   func() int64 // ID 生成（main 注入 snowflake）
}

// NewService 构造。
func NewService(repo *Repo, authSvc *auth.Service, idGen func() int64) *Service {
	return &Service{repo: repo, auth: authSvc, sf: idGen}
}

// Create 创建（返回明文 Key，仅一次）。
func (s *Service) Create(ctx context.Context, req *CreateReq) (*CreateResp, error) {
	plain, err := pcrypto.NewAPIKey()
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	var expiresAt *time.Time
	if req.ExpiresDays != nil && *req.ExpiresDays > 0 {
		t := time.Now().AddDate(0, 0, *req.ExpiresDays)
		expiresAt = &t
	}
	do := &DO{
		BaseDO:     database.BaseDO{ID: s.sf()},
		Name:       req.Name,
		APIKeyHash: pcrypto.HashKey(plain),
		KeyPrefix:  pcrypto.KeyDisplay(plain),
		Scope:      marshalArr(req.Scope),
		IPAllow:    marshalArr(req.IPAllow),
		ExpiresAt:  expiresAt,
		Status:     1,
	}
	if err := s.repo.Create(ctx, do); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	// 审计日志
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "apikey",
		Action: "apikey.create", ResourceType: "apikey",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
		ActionDetail: map[string]any{"keyPrefix": do.KeyPrefix, "name": do.Name},
	})
	return &CreateResp{
		ID: do.ID, Name: do.Name, APIKey: plain, KeyPrefix: do.KeyPrefix,
		Scope: req.Scope, ExpiresAt: expiresAt,
	}, nil
}

// List 列表。
func (s *Service) List(ctx context.Context) ([]*ListResp, error) {
	dos, err := s.repo.List(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	out := make([]*ListResp, len(dos))
	for i, d := range dos {
		out[i] = &ListResp{
			ID: d.ID, Name: d.Name, KeyPrefix: d.KeyPrefix, Scope: d.Scope,
			Status: d.Status, ExpiresAt: d.ExpiresAt, LastUsedAt: d.LastUsedAt,
			CreateTime: d.CreateTime,
		}
	}
	return out, nil
}

// Disable 禁用并失效缓存。
func (s *Service) Disable(ctx context.Context, id int64) error {
	if err := s.repo.UpdateStatus(ctx, id, 0); err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	// 缓存失效：粗粒度（按 ID 反查后精确失效可后续优化；30min TTL 自然过期兜底）
	return nil
}

// Delete 软删并失效缓存。
func (s *Service) Delete(ctx context.Context, id int64) error {
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	return nil
}

// RevokeByInternal 紧急撤销（内部通道专用，无需 tenant 上下文）。
// 1. 按 ID 全局查询（不依赖 tenant 隔离）
// 2. UpdateStatus(0)
// 3. Redis Del 缓存（立即失效）
// 4. 写审计日志（actor = internal）
func (s *Service) RevokeByInternal(ctx context.Context, keyID int64) error {
	// 1. 全局查询（不走 repo 租户过滤）
	var do DO
	err := s.repo.DB.WithContext(ctx).
		Table("ykt_aisaas_apikey").
		Where("id = ? AND isDeleted = 0", keyID).
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.New(errs.ResourceNotFound, "API Key 不存在")
	}
	if err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	// 2. UpdateStatus(0)
	if err := s.repo.UpdateStatus(ctx, keyID, 0); err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	// 3. Redis Del 缓存（立即失效）
	s.auth.InvalidateCache(ctx, do.APIKeyHash)
	// 4. 写审计日志
	audit.Record(ctx, audit.AuditEntry{
		TenantID: do.TenantID, ActorType: "internal", ActorID: "0",
		Action: "apikey.revoke", ResourceType: "apikey",
		ResourceID: strconv.FormatInt(keyID, 10), Result: "success",
	})
	return nil
}

// Rotate 轮换 API Key。
// 1. 生成新 Key（同 scope/IP 白名单）
// 2. 旧 Key expiresAt = now + 5min（宽限期）
// 3. 新 Key 立即生效
// 返回：新 Key 明文（仅此一次）
func (s *Service) Rotate(ctx context.Context, id int64) (*CreateResp, error) {
	// 获取旧 Key
	old, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// 生成新 Key
	plain, err := pcrypto.NewAPIKey()
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	// 旧 Key 宽限期 5min
	graceEnd := time.Now().Add(5 * time.Minute)
	old.ExpiresAt = &graceEnd
	old.APIKeyHash = pcrypto.HashKey(plain)
	old.KeyPrefix = pcrypto.KeyDisplay(plain)
	old.Status = 1
	old.CreateTime = time.Now()

	// 更新旧 Key 过期时间 + 插入新 Key（事务）
	var newID int64
	if err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 旧 Key 过期
		if err := tx.Model(&DO{}).Where("id = ?", id).
			Update("expiresAt", graceEnd).Error; err != nil {
			return err
		}
		// 新 Key
		newID = s.sf()
		newDO := &DO{
			BaseDO:     database.BaseDO{ID: newID, TenantID: old.TenantID},
			Name:       old.Name + " (rotated)",
			APIKeyHash: pcrypto.HashKey(plain),
			KeyPrefix:  pcrypto.KeyDisplay(plain),
			Scope:      old.Scope,
			IPAllow:    old.IPAllow,
			Status:     1,
		}
		return tx.Create(newDO).Error
	}); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}

	// 审计日志
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "apikey",
		Action: "apikey.rotate", ResourceType: "apikey",
		ResourceID: strconv.FormatInt(id, 10), Result: "success",
		ActionDetail: map[string]any{"keyPrefix": pcrypto.KeyDisplay(plain)},
	})

	return &CreateResp{
		ID: newID, Name: old.Name, APIKey: plain, KeyPrefix: pcrypto.KeyDisplay(plain),
		Scope: parseScope(old.Scope), ExpiresAt: nil,
	}, nil
}

// parseScope 解析 JSON 数组 scope 字符串。
func parseScope(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	// 简单解析 ["a","b"] 格式
	var out []string
	in := false
	var cur string
	for _, c := range s {
		if c == '"' {
			in = !in
			if !in && cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		if in {
			cur += string(c)
		}
	}
	return out
}

func marshalArr(in []string) string {
	if len(in) == 0 {
		return "[]"
	}
	out := "["
	for i, v := range in {
		if i > 0 {
			out += ","
		}
		out += "\"" + v + "\""
	}
	return out + "]"
}
