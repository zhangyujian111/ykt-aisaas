package apiv1

import (
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/rag"
)

// KBHandler /api/v1/knowledge-bases。
type KBHandler struct {
	Svc       *rag.Service
	Ingest    *rag.Ingestor
	Repo      *rag.Repo
	Retriever *rag.Retriever
}

// Create POST /api/v1/knowledge-bases。
func (h *KBHandler) Create(c *gin.Context) {
	var req rag.CreateKBReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	kb, err := h.Svc.CreateKB(c.Request.Context(), &req)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, kb)
}

// List GET /api/v1/knowledge-bases。
func (h *KBHandler) List(c *gin.Context) {
	kbs, err := h.Svc.ListKBs(c.Request.Context())
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, kbs)
}

// Delete DELETE /api/v1/knowledge-bases/:id。
func (h *KBHandler) Delete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if err := h.Svc.DeleteKB(c.Request.Context(), id); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"id": id, "deleted": true})
}

// Docs GET /api/v1/knowledge-bases/:id/documents。
func (h *KBHandler) Docs(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	docs, err := h.Repo.ListDocs(c.Request.Context(), id)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}
	web.OK(c, docs)
}

// UploadDoc POST /api/v1/knowledge-bases/:id/documents（multipart 或 JSON 文本）。
func (h *KBHandler) UploadDoc(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	var fileName, text string
	if fh, err := c.FormFile("file"); err == nil {
		f, err := fh.Open()
		if err != nil {
			web.Abort(c, errs.Wrap(errs.Internal, err))
			return
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 10<<20))
		if err != nil {
			web.Abort(c, errs.Wrap(errs.Internal, err))
			return
		}
		fileName, text = fh.Filename, string(b)
	} else {
		var body struct {
			FileName string `json:"fileName" binding:"required"`
			Content  string `json:"content" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			web.Abort(c, errs.New(errs.InvalidParam, "需 multipart file 字段或 JSON fileName+content"))
			return
		}
		fileName, text = body.FileName, body.Content
	}
	if strings.TrimSpace(text) == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "文档内容为空"))
		return
	}

	doc, err := h.Ingest.Upload(ctx, id, fileName, text)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, doc)
}

// Search POST /api/v1/knowledge-bases/:id/search。
func (h *KBHandler) Search(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var req rag.SearchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	cits, err := h.Retriever.Search(c.Request.Context(), id, &req)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, cits)
}
