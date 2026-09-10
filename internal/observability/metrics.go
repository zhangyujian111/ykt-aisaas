// Package observability 提供 OTel metrics + logs + trace 基础设施。
//
// V4-O 阶段新增：
//   - OTel metrics（OTLP gRPC exporter）
//   - 5 类业务指标：HTTP / AI / Quota / DB / Outbox
//   - Gin middleware 自动采集 HTTP 指标
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// =============================================================================
// 配置
// =============================================================================

// MetricsConfig OTel metrics 初始化配置。
type MetricsConfig struct {
	ExporterURL string        // "otel-collector:4317"
	ServiceName string        // 服务名
	Interval    time.Duration // 采集间隔，默认 15s
}

// InitMetrics 初始化 OTel metrics（OTLP gRPC exporter）。
//
// 返回 shutdown 函数用于优雅关闭。若 ExporterURL 为空，返回 noop shutdown。
func InitMetrics(cfg MetricsConfig) (func(context.Context) error, error) {
	if cfg.ExporterURL == "" {
		return func(context.Context) error { return nil }, nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Second
	}

	exporter, err := otlpmetricgrpc.New(context.Background(),
		otlpmetricgrpc.WithEndpoint(cfg.ExporterURL),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(cfg.Interval),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

// =============================================================================
// A. HTTP 请求指标
// =============================================================================

var (
	HttpRequestsTotal, _ = otel.Meter("http").Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total HTTP requests"),
	)
	HttpRequestDuration, _ = otel.Meter("http").Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request latency"),
		metric.WithUnit("s"),
	)
	HttpActiveRequests, _ = otel.Meter("http").Int64UpDownCounter(
		"http_active_requests",
		metric.WithDescription("Currently active HTTP requests"),
	)
)

// HttpAttrs 返回 HTTP 指标的标准属性。
func HttpAttrs(method, path string, status int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status", status),
	}
}

// =============================================================================
// B. AI 调用指标
// =============================================================================

var (
	AICallsTotal, _ = otel.Meter("ai").Int64Counter(
		"ai_calls_total",
		metric.WithDescription("Total AI calls"),
	)
	AITokensTotal, _ = otel.Meter("ai").Int64Counter(
		"ai_tokens_total",
		metric.WithDescription("Total AI tokens consumed"),
	)
	AILatencySeconds, _ = otel.Meter("ai").Float64Histogram(
		"ai_latency_seconds",
		metric.WithDescription("AI call latency"),
		metric.WithUnit("s"),
	)
)

// AIAttrs 返回 AI 调用指标的标准属性。
func AIAttrs(model, provider, callType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("model", model),
		attribute.String("provider", provider),
		attribute.String("type", callType),
	}
}

// =============================================================================
// C. Quota 配额指标
// =============================================================================

var (
	QuotaUsed, _ = otel.Meter("quota").Int64UpDownCounter(
		"quota_used",
		metric.WithDescription("Current quota usage"),
	)
	QuotaDeductTotal, _ = otel.Meter("quota").Int64Counter(
		"quota_deduct_total",
		metric.WithDescription("Total quota deductions"),
	)
	QuotaRefundTotal, _ = otel.Meter("quota").Int64Counter(
		"quota_refund_total",
		metric.WithDescription("Total quota refunds"),
	)
)

// QuotaAttrs 返回配额指标的标准属性。
func QuotaAttrs(tenantID int64, quotaType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int64("tenant_id", tenantID),
		attribute.String("quota_type", quotaType),
	}
}

// =============================================================================
// D. Database 数据库指标
// =============================================================================

var (
	DBQueriesTotal, _ = otel.Meter("db").Int64Counter(
		"db_queries_total",
		metric.WithDescription("Total DB queries"),
	)
	DBConnectionsActive, _ = otel.Meter("db").Int64UpDownCounter(
		"db_connections_active",
		metric.WithDescription("Active DB connections"),
	)
	DBQueryDuration, _ = otel.Meter("db").Float64Histogram(
		"db_query_duration_seconds",
		metric.WithDescription("DB query duration"),
		metric.WithUnit("s"),
	)
)

// DBAttrs 返回数据库指标的标准属性。
func DBAttrs(operation, table string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("table", table),
	}
}

// =============================================================================
// E. Outbox 消息队列指标
// =============================================================================

var (
	OutboxPending, _ = otel.Meter("outbox").Int64UpDownCounter(
		"outbox_pending",
		metric.WithDescription("Pending outbox messages"),
	)
	OutboxProcessedTotal, _ = otel.Meter("outbox").Int64Counter(
		"outbox_processed_total",
		metric.WithDescription("Total processed outbox messages"),
	)
	OutboxFailedTotal, _ = otel.Meter("outbox").Int64Counter(
		"outbox_failed_total",
		metric.WithDescription("Total failed outbox messages"),
	)
)

// OutboxAttrs 返回 outbox 指标的标准属性。
func OutboxAttrs(eventType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("event_type", eventType),
	}
}