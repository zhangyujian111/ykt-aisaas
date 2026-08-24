// Package internalapi 内部超级租户接口（/internal/api/v1/*，X-Internal-Token + loopback）。
package internalapi

import (
	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/apikey"
)

// Handler 内部接口。
type Handler struct {
	KeySvc  *apikey.Service
	Billing *billing.Service
}

// IssueAPIKey POST /internal/api/v1/tenants/:tenantId/apikeys
// 运营后台为租户签发首个 API Key（租户后续自助管理走 /api/v1）。
func (h *Handler) IssueAPIKey(c *gin.Context) {
	tid, ok := tenantIDParam(c)
	if !ok {
		return
	}
	var req apikey.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}
	// 以目标租户身份创建（GORM 插件据此填充 tenantId + 隔离）
	ctx := tenant.With(c.Request.Context(), tid)
	resp, err := h.KeySvc.Create(ctx, &req)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, resp)
}
