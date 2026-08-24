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
