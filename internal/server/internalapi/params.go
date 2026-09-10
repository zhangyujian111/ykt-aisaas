package internalapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/web"
)

func tenantIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("tenantId"), 10, 64)
	if err != nil || id <= 0 {
		web.Abort(c, err)
		return 0, false
	}
	return id, true
}

// QuotaRefundReq 配额退款请求。
type QuotaRefundReq struct {
	TenantID  int64  `json:"tenantId" binding:"required"`
	Dimension string `json:"dimension" binding:"required"`
	Estimated int64  `json:"estimated" binding:"required,min=0"` // 预扣量
	Actual    int64  `json:"actual" binding:"required,min=0"`    // 实际用量
	RequestID string `json:"requestId" binding:"required"`       // 溯源
}
