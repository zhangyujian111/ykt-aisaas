// Package tenant 提供多租户上下文：基于 context.Context 显式传播。
// 这是全平台唯一租户传递机制——编译器保证每一层都经过，不存在 ThreadLocal 丢失问题。
package tenant

import (
	"context"

	"ykt.dev/aisaas/internal/platform/errs"
)

type ctxKey struct{}

// With 返回携带租户 ID 的 ctx。调用点：ApiKey/InternalToken 中间件、异步任务入口、测试。
func With(ctx context.Context, tenantID int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, tenantID)
}

// From 提取租户 ID；缺失即 panic（fail-fast，严禁静默放行导致串数据）。
// 仅用于已过鉴权中间件的链路。
func From(ctx context.Context) int64 {
	if v, ok := ctx.Value(ctxKey{}).(int64); ok {
		return v
	}
	panic(errs.New(errs.TenantContextLost))
}

// FromSafe 提取租户 ID，不 panic。
func FromSafe(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(ctxKey{}).(int64)
	return v, ok
}
