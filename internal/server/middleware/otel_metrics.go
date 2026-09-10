// Package middleware Gin 中间件：OTel metrics 自动采集。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/metric"

	"ykt.dev/aisaas/internal/observability"
)

// MetricsMiddleware Gin OTel metrics 中间件。
//
// 自动采集每个 HTTP 请求的：
//   - http_requests_total（按 method/path/status 分组）
//   - http_request_duration_seconds（请求耗时）
//   - http_active_requests（当前活跃请求数）
//
// 使用 c.FullPath() 作为 path 标签（路由模版），避免高基数问题。
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 记录活跃请求 +1
		observability.HttpActiveRequests.Add(c.Request.Context(), 1)

		c.Next()

		// 请求完成后记录指标
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path // fallback for unmatched routes
		}
		status := c.Writer.Status()

		attrs := observability.HttpAttrs(method, path, status)
		observability.HttpRequestsTotal.Add(c.Request.Context(), 1, metric.WithAttributes(attrs...))
		observability.HttpRequestDuration.Record(c.Request.Context(), time.Since(start).Seconds(), metric.WithAttributes(attrs...))
		observability.HttpActiveRequests.Add(c.Request.Context(), -1)
	}
}