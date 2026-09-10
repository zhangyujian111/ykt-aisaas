package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// ExtractWorker 实体抽取处理器。
type ExtractWorker struct {
	svc           *Service
	extractor     *LLMExtractor
	quotaGuard    *quota.Guard
	meterRecorder *metering.Recorder
}

// NewExtractWorker 构造。
func NewExtractWorker(svc *Service, extractor *LLMExtractor) *ExtractWorker {
	return &ExtractWorker{svc: svc, extractor: extractor}
}

// ProcessExtractTask 处理抽取任务。
func (w *ExtractWorker) ProcessExtractTask(ctx context.Context, task *ExtractTask) error {
	slog.Info("processing extract task",
		"taskId", task.TaskID,
		"deviceId", task.DeviceID,
		"msgCount", len(task.Messages),
	)

	// 注入租户上下文
	ctx = tenant.With(ctx, task.TenantID)

	// 估算 token 用量
	estimated := w.estimateToken(task.Messages)

	// 调用 LLM 抽取
	result, err := w.extractor.ExtractEntities(ctx, task.Messages, task.ExtractTypes)
	if err != nil {
		// C-1 退款 + C-2 metering（失败也要记录）
		w.handleRefund(ctx, task.TenantID, estimated, 0)
		w.handleMetering(ctx, task.TenantID, task.TaskID, 0)
		return fmt.Errorf("extract entities: %w", err)
	}

	// C-1 退款（估算 - 实际）+ C-2 metering
	actual := estimated // 使用估算值作为实际值（简化实现）
	w.handleRefund(ctx, task.TenantID, estimated, actual)
	w.handleMetering(ctx, task.TenantID, task.TaskID, actual)

	now := time.Now()

	// 写入实体
	for _, ent := range result.Entities {
		contentJSON := map[string]any{
			"name":       ent.Name,
			"attributes": ent.Attributes,
		}
		contentBytes, _ := json.Marshal(contentJSON)

		do := &MemoryDO{
			BaseDO:         database.BaseDO{ID: NextID(), CreateTime: now, UpdateTime: now},
			DeviceID:       task.DeviceID,
			EntityType:     ent.Type,
			EntityKey:      ent.Name,
			Content:        string(contentBytes),
			Importance:     clampImportance(ent.Importance),
			Version:        1,
			Status:         1,
			LastAccessTime: &now,
		}
		// GORM 租户插件自动填充 tenantId
		if err := w.svc.InsertOrUpdateEntity(ctx, do); err != nil {
			slog.Warn("upsert entity failed", "entityType", ent.Type, "entityKey", ent.Name, "err", err)
			continue
		}

		// 写入关系
		for _, rel := range result.Relations {
			if rel.Source != ent.Name {
				continue
			}
			// 查找目标实体
			targetEntity, err := w.svc.GetEntityByKey(ctx, task.DeviceID, ent.Type, rel.Target)
			if err != nil {
				// 目标实体不存在时用同名临时实体
				targetEntity = &MemoryDO{
					BaseDO:         database.BaseDO{ID: NextID(), CreateTime: now, UpdateTime: now},
					DeviceID:       task.DeviceID,
					EntityType:     ent.Type,
					EntityKey:      rel.Target,
					Content:        "{}",
					Importance:     0.3,
					Version:        1,
					Status:         1,
					LastAccessTime: &now,
				}
				if err := w.svc.InsertOrUpdateEntity(ctx, targetEntity); err != nil {
					slog.Warn("upsert target entity failed", "entityKey", rel.Target, "err", err)
					continue
				}
			}

			relDO := &MemoryRelationDO{
				BaseDO:          database.BaseDO{ID: NextID(), CreateTime: now, UpdateTime: now},
				SourceEntityID:  do.ID,
				RelationType:    rel.Type,
				TargetEntityID:  targetEntity.ID,
				Weight:          clampImportance(rel.Weight),
				Properties:      "{}",
				Version:         1,
				Status:          1,
			}
			if err := w.svc.UpsertRelation(ctx, relDO); err != nil {
				slog.Warn("upsert relation failed", "relationType", rel.Type, "err", err)
			}
		}
	}

	slog.Info("extract task completed",
		"taskId", task.TaskID,
		"entities", len(result.Entities),
		"relations", len(result.Relations),
	)
	return nil
}

func clampImportance(v float64) float64 {
	if v < 0 {
		return 0.01
	}
	if v > 1 {
		return 1.0
	}
	return v
}

// estimateToken 估算 token 用量（字符数/3）。
func (w *ExtractWorker) estimateToken(messages []MessageItem) int64 {
	var chars int
	for _, m := range messages {
		chars += len(m.Content) + 20 // 每条消息额外开销
	}
	tok := int64(chars / 3)
	if tok < 1 {
		tok = 1
	}
	if tok > 4096 {
		tok = 4096
	}
	return tok
}

// handleRefund 处理配额退款（C-1）。
func (w *ExtractWorker) handleRefund(ctx context.Context, tenantID int64, estimated, actual int64) {
	if w.quotaGuard == nil || estimated <= actual {
		return
	}
	refund := estimated - actual
	bg := context.Background()
	if err := w.quotaGuard.Refund(bg, tenantID, redisx.DimLLMTokensIn, refund); err != nil {
		slog.Error("extract refund failed", "tenantID", tenantID, "refund", refund, "err", err)
	}
}

// handleMetering 处理用量记录（C-2）。
func (w *ExtractWorker) handleMetering(ctx context.Context, tenantID int64, taskID string, tokens int64) {
	if w.meterRecorder == nil || tokens <= 0 {
		return
	}
	bg := context.Background()
	w.meterRecorder.Record(bg, metering.Record{
		TenantID:  tenantID,
		BizType:   metering.BizLLM,
		Dimension: redisx.DimLLMTokensIn,
		Amount:    tokens,
		ModelID:   "",
		RequestID: taskID,
		Status:    1,
	})
}