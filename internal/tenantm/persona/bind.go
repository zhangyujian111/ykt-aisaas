package persona

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// Bind 绑定设备到 Persona。
// 1. 校验 Persona 存在且启用
// 2. 解绑旧绑定（同设备唯一）
// 3. 创建新绑定
// 4. 审计
func (s *Service) Bind(ctx context.Context, req *BindReq) (*PersonaBindDO, error) {
	// 1. 校验 Persona 存在
	if _, err := s.repo.GetByID(ctx, req.PersonaID); err != nil {
		return nil, err
	}

	// 2. 构造绑定
	bindType := req.BindType
	if bindType == "" {
		bindType = "default"
	}
	do := &PersonaBindDO{
		BaseDO:        database.BaseDO{ID: ids.Next()},
		DeviceID:      req.DeviceID,
		PersonaID:     req.PersonaID,
		BindType:      bindType,
		Priority:      req.Priority,
		BindCondition: req.BindCondition,
		Status:        1,
	}

	// 3. 执行绑定（事务内 upsert）
	if err := s.repo.BindDevice(ctx, do); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}

	// 4. 审计
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "admin",
		Action: "persona.bind", ResourceType: "persona_bind",
		ResourceID: strconv.FormatInt(do.ID, 10), Result: "success",
		ActionDetail: map[string]any{
			"deviceId":  req.DeviceID,
			"personaId": req.PersonaID,
			"bindType":  bindType,
		},
	})
	return do, nil
}

// Unbind 解绑设备。
// 1. 查询现有绑定
// 2. 软删
// 3. 审计
func (s *Service) Unbind(ctx context.Context, deviceID string) error {
	// 1. 查询现有绑定（确认存在）
	bind, err := s.repo.GetBindByDevice(ctx, deviceID)
	if err != nil {
		return err
	}

	// 2. 软删
	if err := s.repo.UnbindDevice(ctx, deviceID); err != nil {
		return errs.Wrap(errs.Internal, err)
	}

	// 3. 审计
	tid, _ := tenant.FromSafe(ctx)
	audit.Record(ctx, audit.AuditEntry{
		TenantID: tid, ActorType: "admin",
		Action: "persona.unbind", ResourceType: "persona_bind",
		ResourceID: strconv.FormatInt(bind.ID, 10), Result: "success",
		ActionDetail: map[string]any{
			"deviceId":  deviceID,
			"personaId": bind.PersonaID,
		},
	})
	return nil
}

// GetBindByDevice 查询设备绑定信息（公开方法，供 handler 使用）。
func (s *Service) GetBindByDevice(ctx context.Context, deviceID string) (*PersonaBindDO, error) {
	return s.repo.GetBindByDevice(ctx, deviceID)
}

// BindWithDB 使用指定 DB 实例绑定（供事务场景使用）。
func (r *Repo) BindWithDB(ctx context.Context, db *gorm.DB, do *PersonaBindDO) error {
	return db.WithContext(ctx).Create(do).Error
}