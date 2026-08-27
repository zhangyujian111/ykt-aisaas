package auth

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
)

// DeviceTenantResolver 设备→租户解析（tenantm.DeviceTenantService 实现）。
type DeviceTenantResolver interface {
	EnsureDeviceTenant(ctx context.Context, deviceID string) (int64, error)
}

// InternalDeviceMiddleware 设备即租户：X-Internal-Token + X-Device-Id
// → 自动开户（幂等）→ 注入该设备的租户上下文。xiaozhi-server 专用。
func InternalDeviceMiddleware(token string, resolver DeviceTenantResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Internal-Token") != token {
			web.Abort(c, errs.New(errs.TokenInvalid, "internal token 错误"))
			return
		}
		ip := c.ClientIP()
		if ip != "127.0.0.1" && ip != "::1" {
			web.Abort(c, errs.New(errs.IPNotAllowed, "internal API 仅限本机调用"))
			return
		}
		deviceID := strings.TrimSpace(c.GetHeader("X-Device-Id"))
		if deviceID == "" {
			web.Abort(c, errs.New(errs.InvalidParam, "缺少 X-Device-Id"))
			return
		}
		tid, err := resolver.EnsureDeviceTenant(c.Request.Context(), deviceID)
		if err != nil {
			web.Abort(c, err)
			return
		}
		ctx := tenant.With(c.Request.Context(), tid)
		ctx = context.WithValue(ctx, actorCtxKey{}, Actor{Type: "device", Name: deviceID})
		ctx = context.WithValue(ctx, unlimitedKey{}, false) // 设备租户正常配额计费
		c.Request = c.Request.WithContext(ctx)
		c.Set("deviceId", deviceID)
		c.Next()
	}
}
