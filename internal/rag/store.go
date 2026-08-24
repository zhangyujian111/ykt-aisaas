// Package rag 知识库：切片、PgVector 存储、向量化 ingest、检索。
package rag

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Store PgVector 动态表存储：rag_chunk_tenant_{tid}_kb_{kbid}。
// 表名由 int64 拼接，无注入面。
type Store struct {
	pool *pgxpool.Pool
}

// NewStore 构造（pool 由 main 创建）。
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// TableName 动态表名。
func TableName(tenantID, kbID int64) string {
	return fmt.Sprintf("rag_chunk_tenant_%d_kb_%d", tenantID, kbID)
}

// EnsureTable 建表 + 索引（幂等）。
func (s *Store) EnsureTable(ctx context.Context, tenantID, kbID int64, dim int) error {
	t := TableName(tenantID, kbID)
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id         BIGSERIAL PRIMARY KEY,
			docId      BIGINT NOT NULL,
			chunkIndex INT NOT NULL,
			content    TEXT NOT NULL,
			embedding  vector(%d) NOT NULL,
			createTime TIMESTAMP DEFAULT now()
		)`, t, dim)); err != nil {
		return fmt.Errorf("create %s: %w", t, err)
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS idx_%s_doc ON %s (docId)", t, t)); err != nil {
		return fmt.Errorf("index doc: %w", err)
	}
	// ivfflat 在小数据集（< 数万行）会漏召回，规模化后再按 lists≈rows/1000 创建
	return nil
}

// DropTable 删表（删 KB 时）。
func (s *Store) DropTable(ctx context.Context, tenantID, kbID int64) error {
	_, err := s.pool.Exec(ctx, "DROP TABLE IF EXISTS "+TableName(tenantID, kbID))
	return err
}

// InsertChunks 批量写入切片。
func (s *Store) InsertChunks(ctx context.Context, tenantID, kbID, docID int64, contents []string, embeddings [][]float32) error {
	t := TableName(tenantID, kbID)
	batch := &pgx.Batch{}
	for i, c := range contents {
		batch.Queue(fmt.Sprintf("INSERT INTO %s (docId, chunkIndex, content, embedding) VALUES ($1,$2,$3,$4)", t),
			docID, i, c, pgvector.NewVector(embeddings[i]))
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// DeleteDocChunks 删除文档全部切片（重建索引用）。
func (s *Store) DeleteDocChunks(ctx context.Context, tenantID, kbID, docID int64) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM "+TableName(tenantID, kbID)+" WHERE docId = $1", docID)
	return err
}

// ScoredChunk 检索命中。
type ScoredChunk struct {
	DocID      int64   `json:"docId"`
	ChunkIndex int     `json:"chunkIndex"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"` // cosine similarity = 1 - cosine distance
}

// Search 余弦相似度 topK。
func (s *Store) Search(ctx context.Context, tenantID, kbID int64, queryVec []float32, topK int) ([]ScoredChunk, error) {
	if topK <= 0 {
		topK = 5
	}
	t := TableName(tenantID, kbID)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT docId, chunkIndex, content, 1 - (embedding <=> $1) AS score
		FROM %s ORDER BY embedding <=> $1 LIMIT $2`, t),
		pgvector.NewVector(queryVec), topK)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", t, err)
	}
	defer rows.Close()
	var out []ScoredChunk
	for rows.Next() {
		var sc ScoredChunk
		if err := rows.Scan(&sc.DocID, &sc.ChunkIndex, &sc.Content, &sc.Score); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
