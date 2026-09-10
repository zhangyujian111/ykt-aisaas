// Package ai AI 推理分布式追踪（V6-T 阶段）。
//
// 为每个 AI 推理请求生成完整 trace，包含：
//   - ai.inference 父 span（provider/model/operation）
//   - ai.step.* 子 span（tokenize / api_call / parse_stream）
//   - ai.embedding span（RAG 向量化）
//   - ai.vector_search span（RAG 检索）
//
// 与 V4-O 的 metrics+logs 配合，形成完整的可观测性三大支柱。
package ai

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// AITracer AI 推理分布式追踪器。
// 通过 otel.Tracer("ykt-aisaas/ai") 创建 span，支持嵌套子 span。
type AITracer struct {
	tracer trace.Tracer
}

// NewAITracer 构造 AI Tracer。使用全局 TracerProvider 注册的 "ykt-aisaas/ai" tracer。
func NewAITracer() *AITracer {
	return &AITracer{tracer: otel.Tracer("ykt-aisaas/ai")}
}

// Tracer 返回底层 trace.Tracer，供外部创建自定义 span。
func (t *AITracer) Tracer() trace.Tracer { return t.tracer }

// InferenceResult AI 推理完整结果。
type InferenceResult struct {
	Content      string        // 响应文本
	TotalTokens  int           // 总 token
	InputTokens  int           // 输入 token
	OutputTokens int           // 输出 token
	Duration     time.Duration // 实际耗时
}

// TraceInference 追踪 AI 推理全流程。
//
// 创建一个 ai.inference 父 span（SpanKindClient），在回调中执行推理逻辑。
// 自动记录 provider/model/operation 属性，以及 token 用量和耗时。
// 回调返回 error 时自动设置 span 状态为 Error 并记录错误。
//
// 用法：
//
//	result, err := tracer.TraceInference(ctx, "openai", "gpt-4", func(ctx context.Context) (*InferenceResult, error) {
//	    resp, err := client.Complete(ctx, req)
//	    if err != nil { return nil, err }
//	    return &InferenceResult{
//	        Content: resp.Choices[0].Message.Content,
//	        TotalTokens: resp.Usage.TotalTokens,
//	        InputTokens: resp.Usage.PromptTokens,
//	        OutputTokens: resp.Usage.CompletionTokens,
//	    }, nil
//	})
func (t *AITracer) TraceInference(ctx context.Context, provider, model string, fn func(ctx context.Context) (*InferenceResult, error)) (*InferenceResult, error) {
	start := time.Now()
	ctx, span := t.tracer.Start(ctx, "ai.inference",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("ai.provider", provider),
			attribute.String("ai.model", model),
			attribute.String("ai.operation", "chat"),
		),
	)
	defer span.End()

	result, err := fn(ctx)
	duration := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(
			attribute.Float64("ai.duration_ms", float64(duration.Milliseconds())),
		)
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("ai.tokens.total", result.TotalTokens),
		attribute.Int("ai.tokens.input", result.InputTokens),
		attribute.Int("ai.tokens.output", result.OutputTokens),
		attribute.Float64("ai.duration_ms", float64(duration.Milliseconds())),
	)
	span.SetStatus(codes.Ok, "")

	return result, nil
}

// TraceStep 追踪 AI 推理的单个步骤（嵌套子 span）。
//
// 在父 span 下创建 ai.step.{name} 子 span，追踪每个步骤的成败。
// 用于分解 tokenize / api_call / parse_stream 等子步骤。
//
// 用法：
//
//	tracer.TraceStep(ctx, "tokenize", func(ctx context.Context) error {
//	    tokens, err = tokenizer.Encode(prompt)
//	    return err
//	})
func (t *AITracer) TraceStep(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx, span := t.tracer.Start(ctx, "ai.step."+name)
	defer span.End()

	if err := fn(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceStepWithResult 追踪带返回值的步骤。
func (t *AITracer) TraceStepWithResult(ctx context.Context, name string, fn func(context.Context) (any, error)) (any, error) {
	ctx, span := t.tracer.Start(ctx, "ai.step."+name)
	defer span.End()

	result, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}