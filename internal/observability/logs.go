package observability

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// =============================================================================
// 配置
// =============================================================================

// LogsConfig OTel logs 初始化配置。
type LogsConfig struct {
	ExporterURL string // "otel-collector:4317"
	ServiceName string // 服务名
}

// InitLogs 初始化 OTel logs exporter（OTLP gRPC）。
//
// 返回 shutdown 函数用于优雅关闭。若 ExporterURL 为空，返回 noop shutdown。
func InitLogs(cfg LogsConfig) (func(context.Context) error, error) {
	if cfg.ExporterURL == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlploggrpc.New(context.Background(),
		otlploggrpc.WithEndpoint(cfg.ExporterURL),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	return provider.Shutdown, nil
}