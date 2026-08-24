package rag

import (
	"context"
	"fmt"
	"strings"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// Citation 检索引用（chat 的 x-rag-citations / search 接口共用）。
type Citation struct {
	KBID       int64   `json:"kbId"`
	KBName     string  `json:"kbName"`
	DocID      int64   `json:"docId"`
	DocName    string  `json:"docName"`
	ChunkIndex int     `json:"chunkIndex"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

// Retriever 检索服务（chat 集成与 search 接口共用）。
type Retriever struct {
	repo  *Repo
	reg   *llm.Registry
	store *Store
	meter *metering.Recorder
}

func NewRetriever(repo *Repo, reg *llm.Registry, store *Store, meter *metering.Recorder) *Retriever {
	return &Retriever{repo: repo, reg: reg, store: store, meter: meter}
}

// SearchReq 检索请求。
type SearchReq struct {
	Query string `json:"query" binding:"required"`
	TopK  int    `json:"topK"`
}

// Search 单库检索。
func (r *Retriever) Search(ctx context.Context, kbID int64, req *SearchReq) ([]Citation, error) {
	kb, err := r.repo.GetKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	cits, err := r.retrieveFromKB(ctx, kb, req.Query, req.TopK)
	if err != nil {
		return nil, err
	}
	if r.meter != nil {
		r.meter.RecordWithCtx(ctx, metering.Record{
			BizType: "rag", Dimension: "rag_calls", Amount: 1,
			ModelID: kb.EmbeddingModel, Status: 1,
		})
	}
	return cits, nil
}

// RetrieveForChat 多库检索（chat 的 x-knowledge-base-ids 入口）。
// 单库失败跳过（不影响对话），全部失败返回空。
func (r *Retriever) RetrieveForChat(ctx context.Context, kbIDs []int64, query string, topK int) []Citation {
	var out []Citation
	for _, id := range kbIDs {
		kb, err := r.repo.GetKB(ctx, id)
		if err != nil {
			continue
		}
		cits, err := r.retrieveFromKB(ctx, kb, query, topK)
		if err != nil {
			continue
		}
		out = append(out, cits...)
	}
	return out
}

func (r *Retriever) retrieveFromKB(ctx context.Context, kb *KBDO, query string, topK int) ([]Citation, error) {
	if topK <= 0 {
		topK = 5
	}
	resolved, err := r.reg.ResolveFor(ctx, kb.EmbeddingModel, "embedding")
	if err != nil {
		return nil, err
	}
	resp, err := resolved.Client.Embed(ctx, &openaiclient.EmbeddingRequest{
		Model: resolved.UpstreamModel, Input: []string{query},
	})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}
	qv := make([]float32, len(resp.Data[0].Embedding))
	for i, f := range resp.Data[0].Embedding {
		qv[i] = float32(f)
	}

	hits, err := r.store.Search(ctx, kb.TenantID, kb.ID, qv, topK)
	if err != nil {
		return nil, err
	}
	docNames := r.docNames(ctx, hits)
	out := make([]Citation, len(hits))
	for i, h := range hits {
		out[i] = Citation{
			KBID: kb.ID, KBName: kb.Name,
			DocID: h.DocID, DocName: docNames[h.DocID], ChunkIndex: h.ChunkIndex,
			Content: h.Content, Score: h.Score,
		}
	}
	return out, nil
}

// docNames 批量取文档名（检索引用展示用；失败静默，不影响主链路）。
func (r *Retriever) docNames(ctx context.Context, hits []ScoredChunk) map[int64]string {
	ids := make(map[int64]bool, len(hits))
	for _, h := range hits {
		ids[h.DocID] = true
	}
	out := make(map[int64]string, len(ids))
	for id := range ids {
		var name string
		if err := r.repo.db.WithContext(ctx).
			Table("ykt_aisaas_knowledge_document").
			Select("fileName").Where("id = ?", id).
			Scan(&name).Error; err == nil {
			out[id] = name
		}
	}
	return out
}

// BuildContext 拼接 RAG 上下文 system prompt 片段。
func (r *Retriever) BuildContext(cits []Citation) string {
	if len(cits) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("以下是参考知识，回答时优先依据这些内容，无法从中找到答案时如实说明：\n")
	for i, c := range cits {
		fmt.Fprintf(&sb, "【%d】(来自 %s#%d)\n%s\n\n", i+1, c.KBName, c.ChunkIndex, c.Content)
	}
	return sb.String()
}
