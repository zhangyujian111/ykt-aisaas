package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"ykt.dev/aisaas/internal/platform/outbox"
)

// OutboxHandler 实现 outbox.MessageHandler，将信箱消息路由到现有 memory service。
// 消息类型 → 现有方法映射：
//
//	memory_write      → Service.AppendMessage()
//	memory_extract    → ExtractWorker.ProcessExtractTask()
//	memory_summarize  → SummarizeWorker.ProcessSummarizeTask()
type OutboxHandler struct {
	svc       *Service
	extractW  *ExtractWorker
	summarizeW *SummarizeWorker
}

// NewOutboxHandler 构造信箱处理器。
func NewOutboxHandler(svc *Service, extractW *ExtractWorker, summarizeW *SummarizeWorker) *OutboxHandler {
	return &OutboxHandler{
		svc:        svc,
		extractW:   extractW,
		summarizeW: summarizeW,
	}
}

// Handle 实现 outbox.MessageHandler 接口。
func (h *OutboxHandler) Handle(ctx context.Context, msg outbox.OutboxMessage) error {
	switch msg.Type {
	case outbox.TypeMemoryWrite:
		return h.handleMemoryWrite(ctx, msg)
	case outbox.TypeMemoryExtract:
		return h.handleMemoryExtract(ctx, msg)
	case outbox.TypeMemorySummarize:
		return h.handleMemorySummarize(ctx, msg)
	default:
		return fmt.Errorf("%w: %s", outbox.ErrHandlerNotFound, msg.Type)
	}
}

// handleMemoryWrite 处理 memory_write 类型消息。
func (h *OutboxHandler) handleMemoryWrite(ctx context.Context, msg outbox.OutboxMessage) error {
	payloadBytes, ok := msg.Payload.(json.RawMessage)
	if !ok {
		return fmt.Errorf("%w: payload is not json.RawMessage", outbox.ErrPayloadUnmarshal)
	}

	var req WriteMessageReq
	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		return fmt.Errorf("%w: %w", outbox.ErrPayloadUnmarshal, err)
	}

	_, _, appErr := h.svc.AppendMessage(ctx, &req)
	if appErr != nil {
		return fmt.Errorf("outbox memory_write: %w", appErr)
	}

	slog.Debug("outbox memory_write handled",
		"deviceId", msg.DeviceID,
		"role", req.Role,
	)
	return nil
}

// handleMemoryExtract 处理 memory_extract 类型消息。
func (h *OutboxHandler) handleMemoryExtract(ctx context.Context, msg outbox.OutboxMessage) error {
	payloadBytes, ok := msg.Payload.(json.RawMessage)
	if !ok {
		return fmt.Errorf("%w: payload is not json.RawMessage", outbox.ErrPayloadUnmarshal)
	}

	var task ExtractTask
	if err := json.Unmarshal(payloadBytes, &task); err != nil {
		return fmt.Errorf("%w: %w", outbox.ErrPayloadUnmarshal, err)
	}

	if err := h.extractW.ProcessExtractTask(ctx, &task); err != nil {
		return fmt.Errorf("outbox memory_extract: %w", err)
	}

	slog.Debug("outbox memory_extract handled",
		"taskId", task.TaskID,
		"deviceId", task.DeviceID,
	)
	return nil
}

// handleMemorySummarize 处理 memory_summarize 类型消息。
func (h *OutboxHandler) handleMemorySummarize(ctx context.Context, msg outbox.OutboxMessage) error {
	payloadBytes, ok := msg.Payload.(json.RawMessage)
	if !ok {
		return fmt.Errorf("%w: payload is not json.RawMessage", outbox.ErrPayloadUnmarshal)
	}

	var task SummarizeTask
	if err := json.Unmarshal(payloadBytes, &task); err != nil {
		return fmt.Errorf("%w: %w", outbox.ErrPayloadUnmarshal, err)
	}

	if err := h.summarizeW.ProcessSummarizeTask(ctx, &task); err != nil {
		return fmt.Errorf("outbox memory_summarize: %w", err)
	}

	slog.Debug("outbox memory_summarize handled",
		"taskId", task.TaskID,
		"deviceId", task.DeviceID,
	)
	return nil
}