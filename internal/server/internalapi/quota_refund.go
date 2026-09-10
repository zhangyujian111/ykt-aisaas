package internalapi

import (
	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// RefundQuota POST /internal/api/v1/quota/refund
// 退款接口：estimated > actual 时退还差额。
func (h *Handler) RefundQuota(c *gin.Context) {
	var req QuotaRefundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}
	refund := req.Estimated - req.Actual
	if refund <= 0 {
		web.OK(c, gin.H{"refund": 0, "note": "no refund needed"})
		return
	}
	if err := h.QuotaSvc.Refund(c.Request.Context(), req.TenantID, req.Dimension, refund); err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, gin.H{"refund": refund, "tenantId": req.TenantID, "dimension": req.Dimension})
}