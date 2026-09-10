// Package ebpf 提供 Linux 内核 eBPF 追踪能力（V8-E 阶段）。
//
// 追踪 TCP 重传、连接重置等网络事件，导出到 OTel metrics，
// 支撑慢请求根因定位与网络异常检测。
//
// 架构：
//
//	Linux Kernel tracepoints
//	  ↓ eBPF programs
//	Userspace Loader (cilium/ebpf)
//	  ↓ Map iteration
//	OTel Exporter → Prometheus
package ebpf

import (
	"net"
	"time"
)

// TCPEvent 表示一次 TCP 重传/重置事件（V8-E 阶段）。
//
// 与 network.bpf.c 中的 struct tcp_event 字段一一对应。
// 用户态读取后转换为该结构体，便于 exporter 上报 OTel。
type TCPEvent struct {
	Timestamp uint64    // bpf_ktime_get_ns()（纳秒）
	PID       uint32    // 进程 ID
	SAddr     uint32    // 源 IP（网络字节序）
	DAddr     uint32    // 目的 IP（网络字节序）
	SPort     uint16    // 源端口
	DPort     uint16    // 目的端口
	State     uint32    // TCP 状态
	Retries   uint32    // 重试次数
	Comm      string    // 进程名（≤15 字节 + NUL）
	ObservedAt time.Time // userspace 读取时间
}

// SourceIP 将网络字节序 SAddr 解析为字符串。
func (e *TCPEvent) SourceIP() string {
	return net.IPv4(byte(e.SAddr>>24), byte(e.SAddr>>16), byte(e.SAddr>>8), byte(e.SAddr)).String()
}

// DestIP 将网络字节序 DAddr 解析为字符串。
func (e *TCPEvent) DestIP() string {
	return net.IPv4(byte(e.DAddr>>24), byte(e.DAddr>>16), byte(e.DAddr>>8), byte(e.DAddr)).String()
}
