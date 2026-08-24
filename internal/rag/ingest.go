package rag

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// Ingestor 文档向量化管道：切片 → embedding（批量16）→ PgVector。
// 异步执行（Upload 触发后立即返回，状态机落 DB）。
type Ingestor struct {
	repo         *Repo
	reg          *llm.Registry
	sem          chan struct{}
	wg           sync.WaitGroup
	deleteInsert deleteInsertFn
}

func NewIngestor(repo *Repo, reg *llm.Registry) *Ingestor {
	return &Ingestor{repo: repo, reg: reg, sem: make(chan struct{}, 4)}
}

// docStatus 常量。
const (
	DocPending    int8 = 0
	DocProcessing int8 = 1
	DocReady      int8 = 2
	DocFailed     int8 = 3
)

// Upload 创建文档并异步向量化。
func (ing *Ingestor) Upload(ctx context.Context, kbID int64, fileName, text string) (*DocDO, error) {
	kb, err := ing.repo.GetKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	doc := &DocDO{
		BaseDO:          databaseBase(ids.Next()),
		KnowledgeBaseID: kbID,
		FileName:        fileName,
		FileType:        fileTypeOf(fileName),
		FileSize:        int64(len(text)),
		TextContent:     text,
		Status:          DocPending,
	}
	if err := ing.repo.CreateDoc(ctx, doc); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}

	ing.wg.Add(1)
	go ing.process(tenantIDOf(ctx), kb, doc)
	return doc, nil
}

func (ing *Ingestor) process(tid int64, kb *KBDO, doc *DocDO) {
	defer ing.wg.Done()
	ing.sem <- struct{}{}
	defer func() { <-ing.sem }()

	// 异步任务必须显式携带租户上下文（租户隔离由 ctx 驱动）
	ctx := tenant.With(context.Background(), tid)
	if err := ing.ingestDoc(ctx, kb, doc); err != nil {
		slog.Error("ingest failed", "docId", doc.ID, "kbId", kb.ID, "err", err)
		_ = ing.repo.UpdateDocStatus(ctx, doc.ID, DocFailed, truncStr(err.Error(), 500), -1)
		return
	}
	_ = ing.repo.BumpKBCounts(ctx, kb.ID, 1, doc.ChunkCount)
}

func (ing *Ingestor) ingestDoc(ctx context.Context, kb *KBDO, doc *DocDO) error {

	chunks := Chunk(doc.TextContent, kb.ChunkSize, kb.ChunkOverlap)
	if len(chunks) == 0 {
		return fmt.Errorf("文档无有效文本")
	}

	resolved, err := ing.reg.ResolveFor(ctx, kb.EmbeddingModel, "embedding")
	if err != nil {
		return err
	}

	_ = ing.repo.UpdateDocStatus(ctx, doc.ID, DocProcessing, "", -1)

	vecs := make([][]float32, len(chunks))
	const batchSize = 16
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		resp, err := resolved.Client.Embed(ctx, &openaiclient.EmbeddingRequest{
			Model: resolved.UpstreamModel,
			Input: chunks[i:end],
		})
		if err != nil {
			return fmt.Errorf("embed batch %d: %w", i/batchSize, err)
		}
		if len(resp.Data) != end-i {
			return fmt.Errorf("embed batch %d: 返回 %d 条，需要 %d", i/batchSize, len(resp.Data), end-i)
		}
		for j, d := range resp.Data {
			v := make([]float32, len(d.Embedding))
			for k, f := range d.Embedding {
				v[k] = float32(f)
			}
			vecs[i+j] = v
		}
	}

	tid := int64(kb.TenantID)
	if err := ing.storeDeleteInsert(ctx, tid, kb.ID, doc.ID, chunks, vecs); err != nil {
		return err
	}
	return ing.repo.UpdateDocStatus(ctx, doc.ID, DocReady, "", len(chunks))
}

// storeDeleteInsert 重建语义：先清旧切片再插入（幂等 reindex）。
// Ingestor 持有 store 引用会导致与 Service 循环依赖，这里通过注入函数解耦。
func (ing *Ingestor) storeDeleteInsert(ctx context.Context, tid, kbID, docID int64, chunks []string, vecs [][]float32) error {
	if ing.deleteInsert == nil {
		return fmt.Errorf("ingestor store 未装配")
	}
	return ing.deleteInsert(ctx, tid, kbID, docID, chunks, vecs)
}

type deleteInsertFn func(ctx context.Context, tid, kbID, docID int64, chunks []string, vecs [][]float32) error

// BindStore main 装配时注入（打破 Service↔Ingestor 循环依赖）。
func (ing *Ingestor) BindStore(fn deleteInsertFn) { ing.deleteInsert = fn }

func fileTypeOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i+1:])
	}
	return "txt"
}

func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
