package ai

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"ykt.dev/aisaas/pkg/openaiclient"
)

// TracedProvider 带追踪的 AI Provider 包装。
//
// 包装 openaiclient.Client，在每次 AI 调用时自动创建 OTel span，
// 记录 provider/model/token 用量等属性。
//
// 支持：
//   - OpenAI 兼容端点（GPT-4 / GPT-4o / DeepSeek / Qwen / 智谱）
//   - Anthropic（通过 OpenAI 兼容代理）
//   - 自建 vllm 端点
//
// 用法：
//
//	client := openaiclient.New("https://api.openai.com/v1", apiKey)
//	provider := ai.NewTracedProvider(client, "openai", "gpt-4")
//	resp, err := provider.Complete(ctx, req)
type TracedProvider struct {
	client   *openaiclient.Client
	tracer   *AITracer
	provider string
	model    string
}

// NewTracedProvider 构造带追踪的 Provider。
func NewTracedProvider(client *openaiclient.Client, provider, model string) *TracedProvider {
	return &TracedProvider{
		client:   client,
		tracer:   NewAITracer(),
		provider: provider,
		model:    model,
	}
}

// Client 返回底层 openaiclient.Client（用于需要直接访问的场景）。
func (p *TracedProvider) Client() *openaiclient.Client { return p.client }

// Provider 返回 provider 名称。
func (p *TracedProvider) Provider() string { return p.provider }

// Model 返回模型名称。
func (p *TracedProvider) Model() string { return p.model }

// Complete 非流式调用（带追踪）。
//
// 创建 ai.inference span，自动记录 provider/model/token 用量/耗时。
// 失败时自动记录错误并设置 span 状态为 Error。
func (p *TracedProvider) Complete(ctx context.Context, req *openaiclient.ChatRequest) (*openaiclient.ChatResponse, error) {
	var resp *openaiclient.ChatResponse

	_, err := p.tracer.TraceInference(ctx, p.provider, p.model, func(ctx context.Context) (*InferenceResult, error) {
		var callErr error
		resp, callErr = p.client.Complete(ctx, req)
		if callErr != nil {
			return nil, callErr
		}

		content := ""
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}

		return &InferenceResult{
			Content:      content,
			TotalTokens:  resp.Usage.TotalTokens,
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Stream 流式调用（带追踪）。
//
// 返回原始 chunk channel + StreamTracer。
// 调用方需在消费完 channel 后调用 streamTracer.Finish(ctx) 完成追踪。
//
// 用法：
//
//	ch, st, err := provider.Stream(ctx, req)
//	if err != nil { return err }
//	var idx int
//	for chunk := range ch {
//	    if chunk.Err != nil { ... }
//	    st.RecordToken(ctx, chunk.Chunk.Choices[0].Delta.Content, idx)
//	    idx++
//	}
//	st.Finish(ctx)
func (p *TracedProvider) Stream(ctx context.Context, req *openaiclient.ChatRequest) (<-chan openaiclient.StreamChunk, *StreamTracer, error) {
	ctx, span := p.tracer.tracer.Start(ctx, "ai.inference",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("ai.provider", p.provider),
			attribute.String("ai.model", p.model),
			attribute.String("ai.operation", "chat"),
			attribute.Bool("ai.stream", true),
		),
	)

	streamTracer := NewStreamTracer()

	rawCh := p.client.Stream(ctx, req)
	outCh := make(chan openaiclient.StreamChunk, 32)

	go func() {
		defer close(outCh)
		defer span.End()

		var hasError bool
		var totalTokens int

		for chunk := range rawCh {
			if chunk.Err != nil {
				span.RecordError(chunk.Err)
				span.SetStatus(codes.Error, chunk.Err.Error())
				hasError = true
				outCh <- chunk
				continue
			}
			if chunk.Done {
				break
			}
			if chunk.Chunk != nil {
				// 追踪 token 内容
				for _, c := range chunk.Chunk.Choices {
					if c.Delta.Content != "" {
						streamTracer.RecordToken(ctx, c.Delta.Content, totalTokens)
						totalTokens++
					}
				}
				// 记录 usage 尾包
				if chunk.Chunk.Usage != nil {
					totalTokens = chunk.Chunk.Usage.TotalTokens
				}
			}
			outCh <- chunk
		}

		if !hasError {
			streamTracer.Finish(ctx)
			span.SetStatus(codes.Ok, "")
		}
	}()

	return outCh, streamTracer, nil
}

// Embed 向量化调用（带追踪）。
func (p *TracedProvider) Embed(ctx context.Context, req *openaiclient.EmbeddingRequest) (*openaiclient.EmbeddingResponse, error) {
	text := ""
	if len(req.Input) > 0 {
		text = req.Input[0]
	}

	ctx, span := p.tracer.tracer.Start(ctx, "ai.embedding",
		trace.WithAttributes(
			attribute.String("ai.provider", p.provider),
			attribute.String("ai.model", req.Model),
			attribute.String("embedding.operation", "encode"),
		),
	)
	defer span.End()

	resp, err := p.client.Embed(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if len(resp.Data) > 0 {
		span.SetAttributes(
			attribute.Int("embedding.dimensions", len(resp.Data[0].Embedding)),
		)
	}
	_ = text
	span.SetStatus(codes.Ok, "")
	return resp, nil
}