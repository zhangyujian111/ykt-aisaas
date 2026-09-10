package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracingConfig OTel tracing 初始化配置（V6-T 阶段）。
type TracingConfig struct {
	ExporterURL string  // "otel-collector:4317"
	ServiceName string  // 服务名
	SampleRatio float64 // trace 采样率，默认 0.1（10%）
}

// InitTracing 初始化 OTel tracing（OTLP gRPC exporter）。
//
// 与 V4-O 的 InitMetrics/InitLogs 并列，补齐 traces + metrics + logs 三大支柱。
// 使用 ParentBased(TraceIDRatioBased) 采样器，确保同 trace 的 span 一致性。
//
// 返回 shutdown 函数用于优雅关闭。若 ExporterURL 为空，返回 noop shutdown。
func InitTracing(cfg TracingConfig) (func(context.Context) error, error) {
	if cfg.ExporterURL == "" {
		return func(context.Context) error { return nil }, nil
	}
	if cfg.SampleRatio <= 0 {
		cfg.SampleRatio = 0.1
	}

	exporter, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(cfg.ExporterURL),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}