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

// SummarizeWorker 摘要处理器。
type SummarizeWorker struct {
	svc           *Service
	extractor     *LLMExtractor
	quotaGuard    *quota.Guard
	meterRecorder *metering.Recorder
}

// NewSummarizeWorker 构造。
func NewSummarizeWorker(svc *Service, extractor *LLMExtractor) *SummarizeWorker {
	return &SummarizeWorker{svc: svc, extractor: extractor}
}

// ProcessSummarizeTask 处理摘要任务。
func (w *SummarizeWorker) ProcessSummarizeTask(ctx context.Context, task *SummarizeTask) error {
	slog.Info("processing summarize task",
		"taskId", task.TaskID,
		"deviceId", task.DeviceID,
	)

	ctx = tenant.With(ctx, task.TenantID)

	// 获取最近消息
	entities, err := w.svc.GetEntitiesByDevice(ctx, task.DeviceID, 50, "")
	if err != nil {
		return fmt.Errorf("get recent messages for summarize: %w", err)
	}

	// 构建消息列表
	messages := make([]MessageItem, 0, len(entities))
	for _, e := range entities {
		if e.Content == "" || e.Content == "{}" {
			continue
		}
		var content map[string]any
		if err := json.Unmarshal([]byte(e.Content), &content); err != nil {
			continue
		}
		if summaryText, ok := content["summary"].(string); ok && summaryText != "" {
			messages = append(messages, MessageItem{
				Role:    "system",
				Content: summaryText,
			})
		}
		if facts, ok := content["facts"].([]any); ok {
			var factText string
			for _, f := range facts {
				if s, ok := f.(string); ok {
					factText += s + " "
				}
			}
			if factText != "" {
				messages = append(messages, MessageItem{
					Role:    "system",
					Content: factText,
				})
			}
		}
	}

	if len(messages) == 0 {
		slog.Info("no content to summarize", "taskId", task.TaskID)
		return nil
	}

	// 估算 token 用量
	estimated := w.estimateToken(messages)

	// 调用 LLM 摘要
	result, err := w.extractor.Summarize(ctx, messages, task.SummarizeTypes, task.TopicFocus)
	if err != nil {
		// C-1 退款 + C-2 metering（失败也要记录）
		w.handleRefund(ctx, task.TenantID, estimated, 0)
		w.handleMetering(ctx, task.TenantID, task.TaskID, 0)
		return fmt.Errorf("summarize: %w", err)
	}

	// C-1 退款（估算 - 实际）+ C-2 metering
	actual := estimated
	w.handleRefund(ctx, task.TenantID, estimated, actual)
	w.handleMetering(ctx, task.TenantID, task.TaskID, actual)

	// 写入摘要实体
	now := time.Now()
	contentJSON := map[string]any{
		"summary":    result.Summary,
		"topics":     result.Topics,
		"keyEvents":  result.KeyEvents,
	}
	contentBytes, _ := json.Marshal(contentJSON)

	do := &MemoryDO{
		BaseDO:         database.BaseDO{ID: NextID(), CreateTime: now, UpdateTime: now},
		DeviceID:       task.DeviceID,
		EntityType:     "SUMMARY",
		EntityKey:      fmt.Sprintf("summary-%s", now.Format("20060102")),
		Content:        string(contentBytes),
		Importance:     clampImportance(result.Importance),
		Version:        1,
		Status:         1,
		LastAccessTime: &now,
	}
	if err := w.svc.InsertOrUpdateEntity(ctx, do); err != nil {
		return fmt.Errorf("upsert summary entity: %w", err)
	}

	slog.Info("summarize task completed",
		"taskId", task.TaskID,
		"summaryLen", len(result.Summary),
		"topics", result.Topics,
	)
	return nil
}

// estimateToken 估算 token 用量（字符数/3）。
func (w *SummarizeWorker) estimateToken(messages []MessageItem) int64 {
	var chars int
	for _, m := range messages {
		chars += len(m.Content) + 20
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
func (w *SummarizeWorker) handleRefund(ctx context.Context, tenantID int64, estimated, actual int64) {
	if w.quotaGuard == nil || estimated <= actual {
		return
	}
	refund := estimated - actual
	bg := context.Background()
	if err := w.quotaGuard.Refund(bg, tenantID, redisx.DimLLMTokensIn, refund); err != nil {
		slog.Error("summarize refund failed", "tenantID", tenantID, "refund", refund, "err", err)
	}
}

// handleMetering 处理用量记录（C-2）。
func (w *SummarizeWorker) handleMetering(ctx context.Context, tenantID int64, taskID string, tokens int64) {
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