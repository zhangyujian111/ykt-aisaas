package internalapi

import (
	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// Recharge POST /internal/api/v1/tenants/:tenantId/recharge（管理端手动记账）。
func (h *Handler) Recharge(c *gin.Context) {
	tid, ok := tenantIDParam(c)
	if !ok {
		return
	}
	var req struct {
		AmountCents int64  `json:"amountCents" binding:"required,gt=0"`
		Remark      string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	bal, err := h.Billing.Recharge(c.Request.Context(), tid, req.AmountCents, req.Remark)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, bal)
}
