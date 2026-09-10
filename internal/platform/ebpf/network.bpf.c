// eBPF 网络追踪：捕获 TCP 重传 + 连接重置（V8-E 阶段）
//
// 编译：
//
//	clang -O2 -g -Wall -target bpf \
//	    -D__TARGET_ARCH_x86 \
//	    -I/usr/include/x86_64-linux-gnu \
//	    -c network.bpf.c -o network.bpf.o
//
// 部署到 K8s 节点时通过 DaemonSet 挂载 bpf-fs（/sys/fs/bpf），
// 由 Go loader 通过 cilium/ebpf 加载并附加 tracepoint。

#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <net/sock.h>

// tcp_event 与 Go 侧 TCPEvent 字段一一对应。
//
// 注意：内核结构体字段顺序、宽度必须与 Go 侧严格匹配；
// __attribute__((packed)) 保证无 padding 差异。
struct tcp_event {
    __u64 timestamp;
    __u32 pid;
    __u32 saddr;
    __u32 daddr;
    __u16 sport;
    __u16 dport;
    __u32 state;
    __u32 retries;
    char   comm[16];
} __attribute__((packed));

// tcp_events Hash Map：key=pid_tgid, value=struct tcp_event。
//
// BPF_MAP_TYPE_HASH 在小规模事件下便于 userspace 全量迭代；
// 生产规模可换 BPF_MAP_TYPE_PERF_EVENT_ARRAY（per-cpu perf buffer）。
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, __u64);             // pid + timestamp（避免冲突）
    __type(value, struct tcp_event);
} tcp_events SEC(".maps");

// tracepoint: tcp/tcp_retransmit_skb
//
// trace_event_raw_tcp_retransmit_skb 由 vmlinux.h / trace_args 推导：
//
//   struct trace_event_raw_tcp_retransmit_skb {
//       const void *skbaddr;
//       ...
//       __u16 sport;
//       __u16 dport;
//       ...
//   };
//
// 因此可直接通过 ctx->sport / ctx->dport 读取端口。
SEC("tracepoint/tcp/tcp_retransmit_skb")
int trace_tcp_retransmit(struct trace_event_raw_tcp_retransmit_skb *ctx)
{
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u32 pid      = pid_tgid >> 32;

    struct tcp_event event = {};
    event.timestamp = bpf_ktime_get_ns();
    event.pid       = pid;
    event.retries   = 1;

    // 解析 IP + Port（probe_read 安全访问内核态地址）。
    bpf_probe_read(&event.saddr, sizeof(event.saddr), &ctx->saddr);
    bpf_probe_read(&event.daddr, sizeof(event.daddr), &ctx->daddr);
    bpf_probe_read(&event.sport, sizeof(event.sport), &ctx->sport);
    bpf_probe_read(&event.dport, sizeof(event.dport), &ctx->dport);

    // 读取进程名（最长 TASK_COMM_LEN-1 = 15 字节）。
    bpf_get_current_comm(&event.comm, sizeof(event.comm));

    // 写入 hash map，BPF_ANY 表示 key 存在则更新。
    bpf_map_update_elem(&tcp_events, &pid_tgid, &event, BPF_ANY);

    return 0;
}

// 强制 GPL 协议（cilium/ebpf 加载时校验）。
char _license[] SEC("license") = "GPL";
