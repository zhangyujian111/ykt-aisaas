package ebpf

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// EBPFExporter 将 eBPF 事件转换为 OTel metrics（V8-E 阶段）。
//
// 指标列表：
//   - tcp_retransmit_total：TCP 重传累计计数
//   - tcp_connections_active：活动连接数（UpDownCounter）
//
// 所有指标命名遵循 Prometheus 风格（_total 后缀、snake_case）。
type EBPFExporter struct {
	tcpRetransmitTotal    metric.Int64Counter
	tcpConnectionsActive  metric.Int64UpDownCounter
	tcpRetransmitDuration metric.Float64Histogram // 暂未启用，预留
}

// NewEBPFExporter 构造 OTel exporter。
//
// meter 推荐传入 otel.Meter("ebpf")，与 V4-O 的业务指标命名空间区分。
func NewEBPFExporter(meter metric.Meter) *EBPFExporter {
	return &EBPFExporter{
		tcpRetransmitTotal: meter.Int64Counter(
			"tcp_retransmit_total",
			metric.WithDescription("Total TCP retransmission events captured by eBPF"),
		),
		tcpConnectionsActive: meter.Int64UpDownCounter(
			"tcp_connections_active",
			metric.WithDescription("Currently active TCP connections observed by eBPF"),
		),
	}
}

// ExportTCPEvent 上报单条 TCP 重传事件。
//
// Attributes 标签：
//   - comm：进程名（用于热点进程定位）
//   - dest_ip：目的 IP（用于异常目的地址聚合）
//   - dest_port：目的端口（用于协议识别，如 443/80）
func (e *EBPFExporter) ExportTCPEvent(ctx context.Context, event TCPEvent) {
	if e == nil || e.tcpRetransmitTotal == nil {
		return
	}

	e.tcpRetransmitTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("comm", event.Comm),
		attribute.String("dest_ip", event.DestIP()),
		attribute.Int("dest_port", int(event.DPort)),
		attribute.Int("pid", int(event.PID)),
	))
}

// IncConnections / DecConnections 维护活动连接计数。
//
// 由 connection-tracking tracepoint 调用（V8 扩展时启用），
// 此处保留接口以避免后续重构。
func (e *EBPFExporter) IncConnections(ctx context.Context, event TCPEvent) {
	if e == nil || e.tcpConnectionsActive == nil {
		return
	}
	e.tcpConnectionsActive.Add(ctx, 1, metric.WithAttributes(
		attribute.String("comm", event.Comm),
		attribute.Int("dest_port", int(event.DPort)),
	))
}

func (e *EBPFExporter) DecConnections(ctx context.Context, event TCPEvent) {
	if e == nil || e.tcpConnectionsActive == nil {
		return
	}
	e.tcpConnectionsActive.Add(ctx, -1, metric.WithAttributes(
		attribute.String("comm", event.Comm),
		attribute.Int("dest_port", int(event.DPort)),
	))
}
