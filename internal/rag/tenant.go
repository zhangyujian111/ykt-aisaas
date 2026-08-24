package rag

import (
	"context"

	"ykt.dev/aisaas/internal/platform/tenant"
)

func tenantFromCtx(ctx context.Context) (int64, bool) {
	return tenant.FromSafe(ctx)
}
