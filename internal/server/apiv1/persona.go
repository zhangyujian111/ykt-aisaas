// Package apiv1 SaaS 自有接口（/api/v1/*）。
package apiv1

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/audit"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/persona"
)

// PersonaHandler /api/v1/personas。
type PersonaHandler struct {
	Svc     *persona.Service
	Audit   *audit.Recorder
}

// GetPersona GET /api/v1/personas/:id — 按 ID 读取人设详情。
func (h *PersonaHandler) GetPersona(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "id 必须为正整数"))
		return
	}

	do, err := h.Svc.GetByID(c.Request.Context(), id)
	if err != nil {
		web.Abort(c, err)
		return
	}

	web.OK(c, do.ToResp())
}

// GetPersonaByDevice GET /api/v1/personas?deviceId=... — 按设备查询绑定的 Persona。
func (h *PersonaHandler) GetPersonaByDevice(c *gin.Context) {
	deviceID := c.Query("deviceId")
	if deviceID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "deviceId 不能为空"))
		return
	}

	do, bind, err := h.Svc.GetPersonaWithBind(c.Request.Context(), deviceID)
	if err != nil {
		web.Abort(c, err)
		return
	}

	// 直接返回数组，前端无需解析 {items, hasMore}
	resp := &persona.PersonaWithBindResp{
		PersonaResp: do.ToResp(),
	}
	if bind != nil {
		resp.PersonaBind = bind.ToBindResp()
	}

	web.OK(c, []*persona.PersonaWithBindResp{resp})
}