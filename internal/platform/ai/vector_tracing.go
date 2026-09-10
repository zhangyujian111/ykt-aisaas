package ai

import (
	"context"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// VectorSearchResult 向量检索结果。
type VectorSearchResult struct {
	ID       string            // 文档 ID
	Score    float64           // 相似度分数
	Content  string            // 文本内容
	Metadata map[string]string // 扩展元数据
}

// TraceVectorSearch 追踪向量检索（RAG 检索步骤）。
//
// 创建 ai.vector_search span，记录：
//   - vector.query：检索查询文本
//   - vector.top_k：返回数量
//   - vector.results_count：实际命中数
//   - vector.similarity_avg/max：平均/最大相似度
//   - vector.match event × top-5：每个命中的 rank/doc_id/similarity
//
// 用法：
//
//	docs, err := tracer.TraceVectorSearch(ctx, query, 10, func(ctx context.Context) ([]VectorSearchResult, error) {
//	    return pgVector.Search(ctx, embedding, 10)
//	})
func (t *AITracer) TraceVectorSearch(ctx context.Context, query string, topK int, fn func(context.Context) ([]VectorSearchResult, error)) ([]VectorSearchResult, error) {
	ctx, span := t.tracer.Start(ctx, "ai.vector_search",
		trace.WithAttributes(
			attribute.String("vector.query", query),
			attribute.Int("vector.top_k", topK),
		),
	)
	defer span.End()

	results, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("vector.results_count", len(results)),
	)

	// 计算平均/最大相似度，记录 top-5 命中详情
	if len(results) > 0 {
		var sum float64
		maxScore := results[0].Score
		for _, r := range results {
			sum += r.Score
			if r.Score > maxScore {
				maxScore = r.Score
			}
		}
		span.SetAttributes(
			attribute.Float64("vector.similarity_avg", sum/float64(len(results))),
			attribute.Float64("vector.similarity_max", maxScore),
		)

		// 记录 top-5 命中详情
		limit := len(results)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			r := results[i]
			span.AddEvent("vector.match", trace.WithAttributes(
				attribute.Int("rank", i+1),
				attribute.String("doc_id", r.ID),
				attribute.Float64("similarity", r.Score),
			))
		}
	}

	span.SetStatus(codes.Ok, "")
	return results, nil
}

// TraceEmbedding 追踪 embedding 向量生成。
//
// 创建 ai.embedding span，记录：
//   - ai.model：embedding 模型名
//   - embedding.text_length：输入文本长度
//   - embedding.dimensions：输出向量维度
//
// 用法：
//
//	vec, err := tracer.TraceEmbedding(ctx, "text-embedding-3-small", text, func(ctx context.Context) ([]float32, error) {
//	    return client.Embed(ctx, req)
//	})
func (t *AITracer) TraceEmbedding(ctx context.Context, model string, text string, fn func(context.Context) ([]float32, error)) ([]float32, error) {
	ctx, span := t.tracer.Start(ctx, "ai.embedding",
		trace.WithAttributes(
			attribute.String("ai.model", model),
			attribute.String("embedding.operation", "encode"),
			attribute.String("embedding.text_length", strconv.Itoa(len(text))),
		),
	)
	defer span.End()

	embedding, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("embedding.dimensions", len(embedding)),
	)
	span.SetStatus(codes.Ok, "")
	return embedding, nil
}

// TraceRAGPipeline 追踪完整的 RAG 流水线。
//
// 在一个 trace 中串联 embedding → vector_search → augmented inference，
// 便于全链路分析 RAG 各环节耗时。
//
// 用法：
//
//	result, err := tracer.TraceRAGPipeline(ctx, provider, model, embeddingModel, query, topK,
//	    func(ctx context.Context) ([]float32, error) {
//	        return client.Embed(ctx, query)
//	    },
//	    func(ctx context.Context, embedding []float32) ([]VectorSearchResult, error) {
//	        return pgVector.Search(ctx, embedding, topK)
//	    },
//	    func(ctx context.Context, docs []VectorSearchResult) (*InferenceResult, error) {
//	        return augmentedLLM.Complete(ctx, buildPrompt(query, docs))
//	    },
//	)
func (t *AITracer) TraceRAGPipeline(
	ctx context.Context,
	provider, model, embeddingModel, query string,
	topK int,
	embedFn func(context.Context) ([]float32, error),
	searchFn func(context.Context, []float32) ([]VectorSearchResult, error),
	inferFn func(context.Context, []VectorSearchResult) (*InferenceResult, error),
) (*InferenceResult, error) {
	ctx, span := t.tracer.Start(ctx, "ai.rag_pipeline",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("ai.provider", provider),
			attribute.String("ai.model", model),
			attribute.String("ai.embedding_model", embeddingModel),
			attribute.String("ai.operation", "rag"),
			attribute.Int("vector.top_k", topK),
		),
	)
	defer span.End()

	// Step 1: Embedding
	embedding, err := t.TraceEmbedding(ctx, embeddingModel, query, func(ctx context.Context) ([]float32, error) {
		return embedFn(ctx)
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "embedding failed: "+err.Error())
		return nil, err
	}

	// Step 2: Vector Search
	docs, err := t.TraceVectorSearch(ctx, query, topK, func(ctx context.Context) ([]VectorSearchResult, error) {
		return searchFn(ctx, embedding)
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "vector_search failed: "+err.Error())
		return nil, err
	}

	// Step 3: Augmented Inference
	result, err := inferFn(ctx, docs)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "inference failed: "+err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("ai.tokens.total", result.TotalTokens),
		attribute.Int("ai.tokens.input", result.InputTokens),
		attribute.Int("ai.tokens.output", result.OutputTokens),
		attribute.Int("vector.results_count", len(docs)),
	)
	span.SetStatus(codes.Ok, "")

	return result, nil
}