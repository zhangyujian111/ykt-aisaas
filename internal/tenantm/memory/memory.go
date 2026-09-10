package memory

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// Service 记忆模块核心服务。
type Service struct {
	db  *gorm.DB
	rdb *redisx.Client
	cfg Config
}

// NewService 构造记忆服务。
func NewService(db *gorm.DB, rdb *redisx.Client, cfg Config) *Service {
	if cfg.ExtractThreshold <= 0 {
		cfg = DefaultConfig()
	}
	return &Service{db: db, rdb: rdb, cfg: cfg}
}

// ---- session_message 操作 ----

// InsertMessage 插入短期消息。
func (s *Service) InsertMessage(ctx context.Context, msg *SessionMessageDO) error {
	return s.db.WithContext(ctx).Create(msg).Error
}

// ListMessages 列出短期消息（游标分页，按时间倒序）。
func (s *Service) ListMessages(ctx context.Context, deviceID string, limit int, cursor int64) ([]*SessionMessageDO, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var dos []*SessionMessageDO
	q := s.db.WithContext(ctx).Order("id DESC")
	if cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	if err := q.Limit(limit + 1).Find(&dos).Error; err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	hasMore := len(dos) > limit
	if hasMore {
		dos = dos[:limit]
	}
	return dos, hasMore, nil
}

// CountNewMessagesSince 统计自某时间点以来的新消息数。
func (s *Service) CountNewMessagesSince(ctx context.Context, deviceID string, since time.Time) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&SessionMessageDO{}).
		Where("createTime >= ? AND isDeleted = 0", since).
		Count(&count).Error
	return count, err
}

// ---- memory 实体操作 ----

// InsertOrUpdateEntity 插入或更新实体（ON DUPLICATE KEY UPDATE）。
func (s *Service) InsertOrUpdateEntity(ctx context.Context, do *MemoryDO) error {
	// 使用原生 UPSERT（deviceId + entityType + entityKey 唯一键）
	now := time.Now()
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO ykt_aisaas_memory
			(id, tenantId, deviceId, entityType, entityKey, content, importance, sourceMessageId, version, status, lastAccessTime, createTime, updateTime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			content = VALUES(content),
			importance = VALUES(importance),
			version = version + 1,
			lastAccessTime = VALUES(lastAccessTime),
			updateTime = VALUES(updateTime)
	`, do.ID, do.TenantID, do.DeviceID, do.EntityType, do.EntityKey,
		do.Content, do.Importance, do.SourceMsgID,
		now, now, now).Error
}

// UpsertRelation 插入或更新关系。
func (s *Service) UpsertRelation(ctx context.Context, do *MemoryRelationDO) error {
	now := time.Now()
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO ykt_aisaas_memory_relation
			(id, tenantId, sourceEntityId, relationType, targetEntityId, weight, properties, sourceMessageId, version, status, createTime, updateTime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)
		ON DUPLICATE KEY UPDATE
			weight = VALUES(weight),
			properties = VALUES(properties),
			version = version + 1,
			updateTime = VALUES(updateTime)
	`, do.ID, do.TenantID, do.SourceEntityID, do.RelationType, do.TargetEntityID,
		do.Weight, do.Properties, do.SourceMsgID,
		now, now).Error
}

// GetEntityByKey 按设备+类型+Key 查实体。
func (s *Service) GetEntityByKey(ctx context.Context, deviceID, entityType, entityKey string) (*MemoryDO, error) {
	var do MemoryDO
	err := s.db.WithContext(ctx).
		Where("deviceId = ? AND entityType = ? AND entityKey = ? AND isDeleted = 0", deviceID, entityType, entityKey).
		Take(&do).Error
	return &do, err
}

// GetEntitiesByDevice 按设备查实体（按 importance DESC, lastAccessTime DESC）。
func (s *Service) GetEntitiesByDevice(ctx context.Context, deviceID string, limit int, entityType string) ([]*MemoryDO, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var dos []*MemoryDO
	q := s.db.WithContext(ctx).
		Where("deviceId = ? AND status = 1 AND isDeleted = 0", deviceID).
		Order("importance DESC, lastAccessTime DESC")
	if entityType != "" {
		q = q.Where("entityType = ?", entityType)
	}
	if err := q.Limit(limit).Find(&dos).Error; err != nil {
		return nil, fmt.Errorf("get entities by device: %w", err)
	}
	return dos, nil
}

// GetRelationsByEntity 按实体 ID 查关系。
func (s *Service) GetRelationsByEntity(ctx context.Context, entityID int64) ([]*MemoryRelationDO, error) {
	var dos []*MemoryRelationDO
	err := s.db.WithContext(ctx).
		Where("(sourceEntityId = ? OR targetEntityId = ?) AND status = 1 AND isDeleted = 0", entityID, entityID).
		Find(&dos).Error
	return dos, err
}

// UpdateLastAccess 更新最后访问时间。
func (s *Service) UpdateLastAccess(ctx context.Context, ids []int64) {
	now := time.Now()
	s.db.WithContext(ctx).Model(&MemoryDO{}).
		Where("id IN ?", ids).
		Update("lastAccessTime", now)
}

// NextID 生成雪花 ID。
func NextID() int64 { return ids.Next() }