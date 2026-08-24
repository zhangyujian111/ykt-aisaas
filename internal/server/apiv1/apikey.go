// Package apiv1 SaaS 自有接口（/api/v1/*）。
package apiv1

import (
	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/apikey"
)

// APIKeyHandler /api/v1/apikeys。
type APIKeyHandler struct{ Svc *apikey.Service }

// Create POST /api/v1/apikeys。
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req apikey.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}
	resp, err := h.Svc.Create(c.Request.Context(), &req)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, resp)
}

// List GET /api/v1/apikeys。
func (h *APIKeyHandler) List(c *gin.Context) {
	list, err := h.Svc.List(c.Request.Context())
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, list)
}

// Disable PATCH /api/v1/apikeys/:id/status。
func (h *APIKeyHandler) Disable(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if err := h.Svc.Disable(c.Request.Context(), id); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"id": id, "status": 0})
}

// Delete DELETE /api/v1/apikeys/:id。
func (h *APIKeyHandler) Delete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if err := h.Svc.Delete(c.Request.Context(), id); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"id": id, "deleted": true})
}
