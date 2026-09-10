package internalapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// RotateAPIKey POST /internal/api/v1/apikeys/:id/rotate
// 轮换 API Key：旧 Key 5min 宽限期，新 Key 立即生效。
func (h *Handler) RotateAPIKey(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	resp, err := h.KeySvc.Rotate(c.Request.Context(), id)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, resp)
}

// RevokeAPIKey POST /internal/api/v1/apikeys/:id/revoke
// 紧急撤销 API Key：立即失效 + Redis 缓存清除。
func (h *Handler) RevokeAPIKey(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.KeySvc.RevokeByInternal(c.Request.Context(), id); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"revoked": true, "id": id})
}

// parseIDParam 从 URL 参数提取 int64 ID。
func parseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid id"))
		return 0, false
	}
	return id, true
}