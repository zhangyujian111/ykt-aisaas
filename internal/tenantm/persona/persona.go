package persona

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// Repo 数据访问层（persona + persona_bind 双表）。
type Repo struct {
	DB *gorm.DB
}

// NewRepo 构造。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{DB: db}
}

// ============================================================
// Persona CRUD
// ============================================================

// GetByID 按 ID 查询 Persona（租户过滤由 GORM 插件保证）。
func (r *Repo) GetByID(ctx context.Context, id int64) (*PersonaDO, error) {
	var do PersonaDO
	err := r.DB.WithContext(ctx).
		Where("id = ? AND isDeleted = 0 AND status = 1", id).
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "Persona 不存在")
	}
	return &do, err
}

// GetByCode 按租户内编码查询（含已删除，用于唯一性校验）。
func (r *Repo) GetByCode(ctx context.Context, code string) (*PersonaDO, error) {
	var do PersonaDO
	err := r.DB.WithContext(ctx).
		Where("code = ? AND isDeleted = 0", code).
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "Persona 不存在")
	}
	return &do, err
}

// List 按租户列出所有 Persona。
func (r *Repo) List(ctx context.Context) ([]*PersonaDO, error) {
	var out []*PersonaDO
	err := r.DB.WithContext(ctx).
		Where("isDeleted = 0 AND status = 1").
		Order("id DESC").
		Find(&out).Error
	return out, err
}

// Create 创建 Persona。
func (r *Repo) Create(ctx context.Context, do *PersonaDO) error {
	return r.DB.WithContext(ctx).Create(do).Error
}

// Update 更新 Persona。
func (r *Repo) Update(ctx context.Context, do *PersonaDO) error {
	return r.DB.WithContext(ctx).
		Model(&PersonaDO{}).
		Where("id = ? AND isDeleted = 0", do.ID).
		Updates(map[string]any{
			"name":                   do.Name,
			"description":            do.Description,
			"systemPrompt":           do.SystemPrompt,
			"personalityTraits":      do.PersonalityTraits,
			"voicePreference":        do.VoicePreference,
			"relationshipStages":     do.RelationshipStages,
			"defaultModelId":         do.DefaultModelID,
			"defaultKnowledgeBaseIds": do.DefaultKnowledgeBaseIDs,
			"temperature":            do.Temperature,
			"topP":                   do.TopP,
			"maxTokens":              do.MaxTokens,
			"presencePenalty":        do.PresencePenalty,
			"frequencyPenalty":       do.FrequencyPenalty,
			"tags":                   do.Tags,
			"status":                 do.Status,
		}).Error
}

// SoftDelete 软删 Persona。
func (r *Repo) SoftDelete(ctx context.Context, id int64) error {
	return r.DB.WithContext(ctx).
		Model(&PersonaDO{}).
		Where("id = ?", id).
		Update("isDeleted", 1).Error
}

// ============================================================
// PersonaBind CRUD
// ============================================================

// GetBindByDevice 按设备 ID 查询绑定（租户过滤由 GORM 插件保证）。
func (r *Repo) GetBindByDevice(ctx context.Context, deviceID string) (*PersonaBindDO, error) {
	var do PersonaBindDO
	err := r.DB.WithContext(ctx).
		Where("deviceId = ? AND isDeleted = 0 AND status = 1", deviceID).
		Order("priority DESC, id ASC").
		Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "设备未绑定 Persona")
	}
	return &do, err
}

// BindDevice 绑定设备到 Persona（upsert：同一设备唯一绑定）。
func (r *Repo) BindDevice(ctx context.Context, do *PersonaBindDO) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先解绑旧绑定（软删）
		if err := tx.Model(&PersonaBindDO{}).
			Where("deviceId = ? AND tenantId = ? AND isDeleted = 0", do.DeviceID, do.TenantID).
			Updates(map[string]any{"status": 0, "isDeleted": 1}).Error; err != nil {
			return err
		}
		// 创建新绑定
		return tx.Create(do).Error
	})
}

// UnbindDevice 解绑设备。
func (r *Repo) UnbindDevice(ctx context.Context, deviceID string) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&PersonaBindDO{}).
			Where("deviceId = ? AND isDeleted = 0", deviceID).
			Updates(map[string]any{"status": 0, "isDeleted": 1}).Error
	})
}

// ============================================================
// Service
// ============================================================

// Service Persona 业务层。
type Service struct {
	repo          *Repo
	promptBuilder *SystemPromptBuilder
}

// NewService 构造。
func NewService(repo *Repo, cfg PersonaConfig) *Service {
	return &Service{
		repo:          repo,
		promptBuilder: NewSystemPromptBuilder(cfg),
	}
}

// GetByID 按 ID 查询 Persona 并审计。
func (s *Service) GetByID(ctx context.Context, id int64) (*PersonaDO, error) {
	do, err := s.repo.GetByID(ctx, id)
	if err != nil {
		audit.Record(ctx, audit.AuditEntry{
			ActorType: "api_key", Action: "persona.read", ResourceType: "persona",
			ResourceID: strconv.FormatInt(id, 10), Result: "failure",
			ErrorCode: fmt.Sprintf("%d", errs.ResourceNotFound),
		})
		return nil, err
	}
	// 审计：persona.read（轻量操作，仅审计不预扣）
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "api_key",
		Action: "persona.read", ResourceType: "persona",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
	})
	return do, nil
}

// GetByDevice 按设备 ID 查询绑定的 Persona。
func (s *Service) GetByDevice(ctx context.Context, deviceID string) (*PersonaDO, error) {
	bind, err := s.repo.GetBindByDevice(ctx, deviceID)
	if err != nil {
		audit.Record(ctx, audit.AuditEntry{
			ActorType: "api_key", Action: "persona.read", ResourceType: "persona",
			ResourceID: deviceID, Result: "failure",
			ActionDetail: map[string]any{"deviceId": deviceID},
		})
		return nil, err
	}
	do, err := s.repo.GetByID(ctx, bind.PersonaID)
	if err != nil {
		return nil, err
	}
	// 审计
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "api_key",
		Action: "persona.read", ResourceType: "persona",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
		ActionDetail: map[string]any{"deviceId": deviceID, "bindId": bind.ID},
	})
	return do, nil
}

// GetPersonaWithBind 按设备 ID 查询绑定的 Persona（含绑定信息，避免重复查询）。
func (s *Service) GetPersonaWithBind(ctx context.Context, deviceID string) (*PersonaDO, *PersonaBindDO, error) {
	bind, err := s.repo.GetBindByDevice(ctx, deviceID)
	if err != nil {
		audit.Record(ctx, audit.AuditEntry{
			ActorType: "api_key", Action: "persona.read", ResourceType: "persona",
			ResourceID: deviceID, Result: "failure",
			ActionDetail: map[string]any{"deviceId": deviceID},
		})
		return nil, nil, err
	}
	do, err := s.repo.GetByID(ctx, bind.PersonaID)
	if err != nil {
		return nil, nil, err
	}
	// 审计
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "api_key",
		Action: "persona.read", ResourceType: "persona",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
		ActionDetail: map[string]any{"deviceId": deviceID, "bindId": bind.ID},
	})
	return do, bind, nil
}

// Create 创建 Persona（admin 用，不暴露 API）。
func (s *Service) Create(ctx context.Context, req *CreatePersonaReq) (*PersonaDO, error) {
	// 唯一性校验
	if _, err := s.repo.GetByCode(ctx, req.Code); err == nil {
		return nil, errs.New(errs.Conflict, "Persona 编码已存在: "+req.Code)
	}

	do := &PersonaDO{
		Code:                    req.Code,
		Name:                    req.Name,
		Description:             req.Description,
		SystemPrompt:            req.SystemPrompt,
		PersonalityTraits:       defaultJSON(req.PersonalityTraits),
		VoicePreference:         req.VoicePreference,
		RelationshipStages:      defaultJSON(req.RelationshipStages),
		DefaultModelID:          req.DefaultModelID,
		DefaultKnowledgeBaseIDs: defaultJSON(req.DefaultKnowledgeBaseIDs),
		Temperature:             ptrFloat64(req.Temperature, 0.70),
		TopP:                    ptrFloat64(req.TopP, 0.90),
		MaxTokens:               ptrInt(req.MaxTokens, 4096),
		PresencePenalty:         ptrFloat64(req.PresencePenalty, 0.00),
		FrequencyPenalty:        ptrFloat64(req.FrequencyPenalty, 0.00),
		Status:                  1,
		Tags:                    defaultJSON(req.Tags),
	}
	do.ID = ids.Next()

	if err := s.repo.Create(ctx, do); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}

	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "admin",
		Action: "persona.create", ResourceType: "persona",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
		ActionDetail: map[string]any{"code": do.Code, "name": do.Name},
	})
	return do, nil
}

// Update 更新 Persona（admin 用，不暴露 API）。
func (s *Service) Update(ctx context.Context, req *UpdatePersonaReq) error {
	existing, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}

	applyIfNotNil(&existing.Name, req.Name)
	applyIfNotNil(&existing.Description, req.Description)
	applyIfNotNil(&existing.SystemPrompt, req.SystemPrompt)
	if req.PersonalityTraits != nil {
		existing.PersonalityTraits = req.PersonalityTraits
	}
	applyIfNotNil(&existing.VoicePreference, req.VoicePreference)
	if req.RelationshipStages != nil {
		existing.RelationshipStages = req.RelationshipStages
	}
	applyIfNotNil(&existing.DefaultModelID, req.DefaultModelID)
	if req.DefaultKnowledgeBaseIDs != nil {
		existing.DefaultKnowledgeBaseIDs = req.DefaultKnowledgeBaseIDs
	}
	applyIfNotNil(&existing.Temperature, req.Temperature)
	applyIfNotNil(&existing.TopP, req.TopP)
	applyIfNotNil(&existing.MaxTokens, req.MaxTokens)
	applyIfNotNil(&existing.PresencePenalty, req.PresencePenalty)
	applyIfNotNil(&existing.FrequencyPenalty, req.FrequencyPenalty)
	if req.Tags != nil {
		existing.Tags = req.Tags
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}

	if err := s.repo.Update(ctx, existing); err != nil {
		return errs.Wrap(errs.Internal, err)
	}

	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "admin",
		Action: "persona.update", ResourceType: "persona",
		ResourceID: strconv.FormatInt(req.ID, 10), Result: "success",
	})
	return nil
}

// Delete 软删 Persona（admin 用，不暴露 API）。
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return errs.Wrap(errs.Internal, err)
	}

	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "admin",
		Action: "persona.delete", ResourceType: "persona",
		ResourceID: strconv.FormatInt(id, 10), Result: "success",
	})
	return nil
}

// List 列出所有 Persona（admin 用，不暴露 API）。
func (s *Service) List(ctx context.Context) ([]*PersonaDO, error) {
	return s.repo.List(ctx)
}

// ============================================================
// 辅助函数
// ============================================================

func ptrFloat64(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

func ptrInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func applyIfNotNil[T any](dest *T, src *T) {
	if src != nil {
		*dest = *src
	}
}

// parsePersonalityTraits 将 JSON 解析为可读 map（用于 prompt 生成）。
func parsePersonalityTraits(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	return m
}