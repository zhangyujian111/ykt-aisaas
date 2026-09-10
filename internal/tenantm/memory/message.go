package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// AppendMessage 写入短期会话消息，并检查是否需要触发自动抽取。
// 返回：消息 DO + 是否应触发抽取。
func (s *Service) AppendMessage(ctx context.Context, req *WriteMessageReq) (*SessionMessageDO, bool, error) {
	now := time.Now()
	do := &SessionMessageDO{
		BaseDO:       database.BaseDO{ID: NextID(), CreateTime: now, UpdateTime: now},
		SessionID:    0, // 后续 session 模块接入后关联
		MessageIndex: 0,
		Role:         req.Role,
		Content:      req.Content,
		TokensIn:     0,
		TokensOut:    0,
		Metadata:     "{}",
		SourceType:   "user_input",
	}
	if req.Metadata != nil {
		do.TokensIn = req.Metadata.Tokens
		metaMap := map[string]any{
			"model":     req.Metadata.Model,
			"tokens":    req.Metadata.Tokens,
			"latencyMs": req.Metadata.LatencyMs,
		}
		metaJSON, _ := json.Marshal(metaMap)
		do.Metadata = string(metaJSON)
	}
	if req.Role == "assistant" {
		do.SourceType = "llm_response"
	}

	if err := s.InsertMessage(ctx, do); err != nil {
		return nil, false, fmt.Errorf("insert message: %w", err)
	}

	// 检查是否需要自动触发抽取（≥10 条新消息 OR 距上次抽取 ≥30min）
	shouldExtract := s.shouldTriggerExtract(ctx)

	slog.Debug("message appended",
		"id", do.ID,
		"role", do.Role,
		"shouldExtract", shouldExtract,
	)
	return do, shouldExtract, nil
}

// GetRecentMessages 获取最近 N 条消息（C-5: 用于累积抽取）。
func (s *Service) GetRecentMessages(ctx context.Context, deviceID string, limit int) ([]MessageItem, error) {
	if limit <= 0 {
		limit = 10
	}
	dos, _, err := s.ListMessages(ctx, deviceID, limit, 0)
	if err != nil {
		return nil, err
	}
	// 反转顺序（ListMessages 返回倒序）
	messages := make([]MessageItem, len(dos))
	for i, d := range dos {
		messages[len(dos)-1-i] = MessageItem{
			Role:    d.Role,
			Content: d.Content,
		}
	}
	return messages, nil
}

// shouldTriggerExtract 检查是否应触发自动抽取。
func (s *Service) shouldTriggerExtract(ctx context.Context) bool {
	tid, ok := tenant.FromSafe(ctx)
	if !ok {
		return false
	}
	// 简单策略：按累计消息数阈值（配置化）
	count, err := s.CountNewMessagesSince(ctx, "", time.Now().Add(-time.Duration(s.cfg.ExtractIntervalMinutes)*time.Minute))
	if err != nil {
		slog.Warn("count new messages failed", "err", err)
		return false
	}
	_ = tid
	return count >= int64(s.cfg.ExtractThreshold)
}

// BuildMessagesForList 构建列表响应。
func BuildMessagesForList(dos []*SessionMessageDO) []*MemoryMessage {
	items := make([]*MemoryMessage, len(dos))
	for i, d := range dos {
		item := &MemoryMessage{
			ID:        d.ID,
			SessionID: d.SessionID,
			Role:      d.Role,
			Content:   d.Content,
			CreatedAt: d.CreateTime,
		}
		if d.Metadata != "" && d.Metadata != "{}" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(d.Metadata), &meta); err == nil {
				item.Metadata = meta
			}
		}
		items[i] = item
	}
	return items
}