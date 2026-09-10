package ai

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// StreamTraceEvent 流式追踪事件（用于导出分析）。
type StreamTraceEvent struct {
	TraceID    string    `json:"trace_id"`
	SpanID     string    `json:"span_id"`
	EventType  string    `json:"event_type"` // "first_token" / "token" / "completed"
	TokenText  string    `json:"token_text"`
	TokenIndex int       `json:"token_index"`
	Timestamp  time.Time `json:"timestamp"`
}

// StreamTracer 流式追踪器。
//
// 追踪 SSE 流式响应的每个 token 到达时间，记录关键指标：
//   - first_token_latency_ms：首 token 延迟（TTFT，Time To First Token）
//   - tokens_per_second：生成速率
//   - 每个 token 的 event（采样记录，避免 event 爆炸）
//
// 用法：
//
//	st := NewStreamTracer(span)
//	for chunk := range stream {
//	    st.RecordToken(ctx, chunk.Text, idx)
//	    idx++
//	}
//	st.Finish(ctx, totalTokens)
type StreamTracer struct {
	events             []StreamTraceEvent
	mu                 sync.Mutex
	startTime          time.Time
	firstTokenLatency  time.Duration
	firstTokenRecorded bool
	totalTokens        int
}

// NewStreamTracer 构造流式追踪器。
func NewStreamTracer() *StreamTracer {
	return &StreamTracer{
		startTime: time.Now(),
		events:    make([]StreamTraceEvent, 0, 256),
	}
}

// RecordToken 记录单个 token 到达事件。
//
// 首 token 记录 first_token_received 事件（关键性能指标）。
// 后续 token 每 10 个采样记录一次 event，避免 trace 数据膨胀。
// 所有 token 累积到内部 events 列表供导出分析。
func (t *StreamTracer) RecordToken(ctx context.Context, text string, index int) {
	span := trace.SpanFromContext(ctx)
	now := time.Now()

	t.mu.Lock()
	t.totalTokens = index + 1

	// 记录首 token 延迟（TTFT 关键指标）
	if !t.firstTokenRecorded {
		t.firstTokenLatency = now.Sub(t.startTime)
		t.firstTokenRecorded = true
		span.AddEvent("first_token_received", trace.WithAttributes(
			attribute.Float64("ai.first_token_latency_ms", float64(t.firstTokenLatency.Milliseconds())),
		))
	}

	// 采样记录 token event（前 5 个 + 每 10 个）
	if index < 5 || index%10 == 0 {
		span.AddEvent("token_received", trace.WithAttributes(
			attribute.String("token.text", text),
			attribute.Int("token.index", index),
		))
	}

	// 累积事件列表
	t.events = append(t.events, StreamTraceEvent{
		TraceID:    span.SpanContext().TraceID().String(),
		SpanID:     span.SpanContext().SpanID().String(),
		EventType:  "token",
		TokenText:  text,
		TokenIndex: index,
		Timestamp:  now,
	})
	t.mu.Unlock()
}

// Finish 完成流式追踪，设置最终 span 属性。
//
// 记录：
//   - ai.tokens.total：总 token 数
//   - ai.duration_ms：总耗时
//   - ai.tokens_per_second：生成速率
//   - ai.first_token_latency_ms：首 token 延迟
//   - stream_completed 事件
func (t *StreamTracer) Finish(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	duration := time.Since(t.startTime)

	t.mu.Lock()
	totalTokens := t.totalTokens
	firstTokenLatency := t.firstTokenLatency
	t.mu.Unlock()

	attrs := []attribute.KeyValue{
		attribute.Int("ai.tokens.total", totalTokens),
		attribute.Float64("ai.duration_ms", float64(duration.Milliseconds())),
	}

	if duration.Seconds() > 0 && totalTokens > 0 {
		attrs = append(attrs,
			attribute.Float64("ai.tokens_per_second", float64(totalTokens)/duration.Seconds()),
		)
	}

	if t.firstTokenRecorded {
		attrs = append(attrs,
			attribute.Float64("ai.first_token_latency_ms", float64(firstTokenLatency.Milliseconds())),
		)
	}

	span.SetAttributes(attrs...)
	span.AddEvent("stream_completed", trace.WithAttributes(
		attribute.Int("ai.stream.total_tokens", totalTokens),
		attribute.Float64("ai.stream.duration_ms", float64(duration.Milliseconds())),
	))
}

// Events 返回累积的事件列表（用于导出分析）。
func (t *StreamTracer) Events() []StreamTraceEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]StreamTraceEvent, len(t.events))
	copy(out, t.events)
	return out
}

// FirstTokenLatency 返回首 token 延迟（TTFT）。
func (t *StreamTracer) FirstTokenLatency() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.firstTokenLatency
}

// TotalTokens 返回累积的 token 总数。
func (t *StreamTracer) TotalTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalTokens
}