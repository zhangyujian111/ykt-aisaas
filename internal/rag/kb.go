package rag

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
)

// KBDO ykt_aisaas_knowledge_base。
type KBDO struct {
	database.BaseDO
	Name           string `gorm:"column:name" json:"name"`
	Description    string `gorm:"column:description" json:"description"`
	EmbeddingModel string `gorm:"column:embeddingModel" json:"embeddingModel"`
	EmbeddingDim   int    `gorm:"column:embeddingDim" json:"embeddingDim"`
	ChunkSize      int    `gorm:"column:chunkSize" json:"chunkSize"`
	ChunkOverlap   int    `gorm:"column:chunkOverlap" json:"chunkOverlap"`
	DocCount       int    `gorm:"column:docCount" json:"docCount"`
	ChunkCount     int    `gorm:"column:chunkCount" json:"chunkCount"`
	Status         int8   `gorm:"column:status" json:"status"`
	IsDeleted      int8   `gorm:"column:isDeleted" json:"-"`
}

func (KBDO) TableName() string { return "ykt_aisaas_knowledge_base" }

// DocDO ykt_aisaas_knowledge_document（时间字段由 BaseDO 提供，勿重复声明）。
type DocDO struct {
	database.BaseDO
	KnowledgeBaseID int64  `gorm:"column:knowledgeBaseId" json:"knowledgeBaseId"`
	FileName        string `gorm:"column:fileName" json:"fileName"`
	FileType        string `gorm:"column:fileType" json:"fileType"`
	FileSize        int64  `gorm:"column:fileSize" json:"fileSize"`
	TextContent     string `gorm:"column:textContent" json:"-"`
	ChunkCount      int    `gorm:"column:chunkCount" json:"chunkCount"`
	Status          int8   `gorm:"column:status" json:"status"` // 0待处理 1处理中 2就绪 3失败
	ErrorMsg        string `gorm:"column:errorMsg" json:"errorMsg"`
	IsDeleted       int8   `gorm:"column:isDeleted" json:"-"`
}

func (DocDO) TableName() string { return "ykt_aisaas_knowledge_document" }

// ---- Repo ----

type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

func (r *Repo) CreateKB(ctx context.Context, do *KBDO) error {
	return r.db.WithContext(ctx).Create(do).Error
}

func (r *Repo) ListKBs(ctx context.Context) ([]*KBDO, error) {
	var out []*KBDO
	err := r.db.WithContext(ctx).Where("isDeleted = 0").Order("id DESC").Find(&out).Error
	return out, err
}

func (r *Repo) GetKB(ctx context.Context, id int64) (*KBDO, error) {
	var do KBDO
	err := r.db.WithContext(ctx).Where("id = ? AND isDeleted = 0", id).Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "知识库不存在")
	}
	return &do, err
}

func (r *Repo) DeleteKB(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&KBDO{}).Where("id = ?", id).Update("isDeleted", 1).Error
}

func (r *Repo) CreateDoc(ctx context.Context, do *DocDO) error {
	return r.db.WithContext(ctx).Create(do).Error
}

func (r *Repo) ListDocs(ctx context.Context, kbID int64) ([]*DocDO, error) {
	var out []*DocDO
	err := r.db.WithContext(ctx).
		Where("knowledgeBaseId = ? AND isDeleted = 0", kbID).
		Order("id DESC").Find(&out).Error
	return out, err
}

func (r *Repo) UpdateDocStatus(ctx context.Context, docID int64, status int8, errMsg string, chunkCount int) error {
	updates := map[string]any{"status": status, "errorMsg": errMsg}
	if chunkCount >= 0 {
		updates["chunkCount"] = chunkCount
	}
	return r.db.WithContext(ctx).Model(&DocDO{}).Where("id = ?", docID).Updates(updates).Error
}

func (r *Repo) BumpKBCounts(ctx context.Context, kbID int64, docDelta, chunkDelta int) error {
	return r.db.WithContext(ctx).Model(&KBDO{}).Where("id = ?", kbID).
		Updates(map[string]any{
			"docCount":   gorm.Expr("docCount + ?", docDelta),
			"chunkCount": gorm.Expr("chunkCount + ?", chunkDelta),
		}).Error
}

// ---- Service（KB/Doc CRUD）----

type CreateKBReq struct {
	Name           string `json:"name" binding:"required,max=128"`
	Description    string `json:"description"`
	EmbeddingModel string `json:"embeddingModel" binding:"required"`
	EmbeddingDim   int    `json:"embeddingDim"`
	ChunkSize      int    `json:"chunkSize"`
	ChunkOverlap   int    `json:"chunkOverlap"`
}

type Service struct {
	repo  *Repo
	store *Store
}

func NewService(repo *Repo, store *Store) *Service {
	return &Service{repo: repo, store: store}
}

func (s *Service) CreateKB(ctx context.Context, req *CreateKBReq) (*KBDO, error) {
	do := &KBDO{
		BaseDO: database.BaseDO{ID: ids.Next()},
		Name:   req.Name, Description: req.Description,
		EmbeddingModel: req.EmbeddingModel,
		ChunkSize:      800, ChunkOverlap: 100, Status: 1,
	}
	if req.ChunkSize > 0 {
		do.ChunkSize = req.ChunkSize
	}
	if req.ChunkOverlap >= 0 {
		do.ChunkOverlap = req.ChunkOverlap
	}
	do.EmbeddingDim = req.EmbeddingDim
	if do.EmbeddingDim <= 0 {
		do.EmbeddingDim = 1024
	}
	if err := s.repo.CreateKB(ctx, do); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	if err := s.store.EnsureTable(ctx, tenantIDOf(ctx), do.ID, do.EmbeddingDim); err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return do, nil
}

func (s *Service) ListKBs(ctx context.Context) ([]*KBDO, error) {
	out, err := s.repo.ListKBs(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return out, nil
}

func (s *Service) GetKB(ctx context.Context, id int64) (*KBDO, error) {
	return s.repo.GetKB(ctx, id)
}

func (s *Service) DeleteKB(ctx context.Context, id int64) error {
	if _, err := s.repo.GetKB(ctx, id); err != nil {
		return err
	}
	if err := s.repo.DeleteKB(ctx, id); err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	return s.store.DropTable(ctx, tenantIDOf(ctx), id)
}

func tenantIDOf(ctx context.Context) int64 {
	if tid, ok := tenantFrom(ctx); ok {
		return tid
	}
	return 0
}

// tenantFrom 从 ctx 取租户（薄封装避免 handler 直接依赖 tenant 包判断）。
func tenantFrom(ctx context.Context) (int64, bool) {
	return tenantFromCtx(ctx)
}
