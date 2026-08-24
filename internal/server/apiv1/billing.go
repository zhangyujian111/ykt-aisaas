package apiv1

import (
	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
)

// BillingHandler /api/v1/billing/* 与 /api/v1/usage/*。
type BillingHandler struct {
	Svc *billing.Service
	Rdb *redisx.Client
}

// Balance GET /api/v1/billing/balance。
func (h *BillingHandler) Balance(c *gin.Context) {
	tid, _ := tenant.FromSafe(c.Request.Context())
	bal, err := h.Svc.GetBalance(c.Request.Context(), tid)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, bal)
}

// Transactions GET /api/v1/billing/transactions?limit=20。
func (h *BillingHandler) Transactions(c *gin.Context) {
	tid, _ := tenant.FromSafe(c.Request.Context())
	limit := intQuery(c, "limit", 20)
	txns, err := h.Svc.ListTxns(c.Request.Context(), tid, limit)
	if err != nil {
		web.Abort(c, err)
		return
	}
	web.OK(c, txns)
}

// UsageOverview GET /api/v1/usage/overview。
func (h *BillingHandler) UsageOverview(c *gin.Context) {
	ctx := c.Request.Context()
	tid, _ := tenant.FromSafe(ctx)
	ov, err := h.Svc.UsageOverview(ctx, tid, nil)
	if err != nil {
		web.Abort(c, err)
		return
	}
	// 附 Redis 实时余量
	remains := map[string]int64{}
	for _, dim := range []string{redisx.DimLLMTokensIn, redisx.DimLLMTokensOut, redisx.DimTTSChars, redisx.DimASRSeconds} {
		limit, used := h.Rdb.QuotaRemaining(ctx, tid, dim)
		remains[dim] = limit - used
	}
	web.OK(c, gin.H{
		"period": ov.Period, "balance": ov.Balance,
		"dimensions": ov.Dimensions, "quotaRemaining": remains,
	})
}

func intQuery(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

var _ = errs.InvalidParam
