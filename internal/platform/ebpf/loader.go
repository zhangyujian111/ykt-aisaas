package ebpf

import (
	"context"
	"errors"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// EBPFLoader 加载并管理 eBPF 对象生命周期（V8-E 阶段）。
//
// 职责：
//  1. 加载编译后的 ELF 对象（network.bpf.o）
//  2. 附加 tracepoint 到内核
//  3. 提供阻塞式事件读取通道
//
// 关闭时必须调用 Close() 释放 link + collection，否则内核态资源泄漏。
type EBPFLoader struct {
	collection *ebpf.Collection
	links      []link.Link
}

// EBPFObjectPath 默认编译产物的相对路径，可由调用方注入。
const DefaultEBPFObjectPath = "internal/platform/ebpf/network.bpf.o"

// NewEBPFLoader 加载编译后的 eBPF 对象。
//
// path 推荐传入 embed.FS 中的 .o 字节（避免运行时依赖外部文件）；
// 此处保留 path 参数以便 dev/test 模式下加载磁盘文件。
func NewEBPFLoader(path string) (*EBPFLoader, error) {
	if path == "" {
		path = DefaultEBPFObjectPath
	}

	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, fmt.Errorf("load collection spec %q: %w", path, err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("new collection: %w", err)
	}

	return &EBPFLoader{collection: coll}, nil
}

// AttachTracepoint 附加 tcp_retransmit_skb tracepoint。
//
// 要求运行节点：kernel >= 4.7 + BPF + SYS_ADMIN capability。
// K8s 侧由 DaemonSet 的 securityContext.privileged 提供。
func (l *EBPFLoader) AttachTracepoint() error {
	prog := l.collection.Programs["trace_tcp_retransmit"]
	if prog == nil {
		return errors.New("ebpf program trace_tcp_retransmit not found in collection")
	}

	lnk, err := link.Tracepoint("tcp", "tcp_retransmit_skb", prog, nil)
	if err != nil {
		return fmt.Errorf("attach tracepoint tcp/tcp_retransmit_skb: %w", err)
	}

	l.links = append(l.links, lnk)
	return nil
}

// ReadEvents 持续读取事件并通过 channel 推送给调用方。
//
// 每次迭代后立即 Delete key，避免重复消费；
// 调用方负责在 ctx 取消时关闭 channel。
func (l *EBPFLoader) ReadEvents(ctx context.Context, ch chan<- TCPEvent) error {
	events := l.collection.Maps["tcp_events"]
	if events == nil {
		return errors.New("ebpf map tcp_events not found in collection")
	}

	iter := events.Iterate()
	var (
		key   uint64
		event struct {
			Timestamp uint64
			PID       uint32
			SAddr     uint32
			DAddr     uint32
			SPort     uint16
			DPort     uint16
			State     uint32
			Retries   uint32
			Comm      [16]byte
		}
	)
	for iter.Next(&key, &event) {
		tcpEvent := TCPEvent{
			Timestamp: event.Timestamp,
			PID:       event.PID,
			SAddr:     event.SAddr,
			DAddr:     event.DAddr,
			SPort:     event.SPort,
			DPort:     event.DPort,
			State:     event.State,
			Retries:   event.Retries,
			Comm:      string(event.Comm[:]),
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- tcpEvent:
		}

		if err := events.Delete(&key); err != nil {
			// 单条删除失败不影响整体流程，仅记录。
			_ = err
		}
	}
	return iter.Err()
}

// Close 释放所有附加的 link 与 collection。
//
// 顺序：先 link（detache 回调）再 collection（卸载 map + program）。
func (l *EBPFLoader) Close() error {
	for _, lnk := range l.links {
		_ = lnk.Close()
	}
	l.links = nil

	if l.collection != nil {
		return l.collection.Close()
	}
	return nil
}
