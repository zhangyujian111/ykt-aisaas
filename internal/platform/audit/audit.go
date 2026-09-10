// Package audit 审计日志：异步写 Redis Stream，后台 consumer 批量落库。
// 写入失败不阻塞业务路径，仅 warn 日志 + 单独重试队列。
package audit

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// AuditEntry 审计日志条目（与 ykt_aisaas_audit_log 表对齐）。
// EventID 由 Record 函数自动生成 UUID v7。
type AuditEntry struct {
	EventID       string         // UUID v7（自动生成）
	TenantID      int64          // 租户 ID（0 = 系统级）
	ActorType     string         // apikey/device/user/internal/system
	ActorID       string         // 操作者 ID
	ActorIP       string         // 来源 IP
	Action        string         // auth.success | auth.fail | quota.check | apikey.create | apikey.rotate | apikey.revoke
	ActionDetail  map[string]any // 扩展字段（自动脱敏）
	ResourceType  string         // apikey | quota | tenant | session
	ResourceID    string         // 资源 ID
	Result        string         // success | failure | partial
	ErrorCode     string         // 错误码
	ErrorMessage  string         // 错误消息（敏感字段 [REDACTED]）
	EventTime     time.Time      // 事件时间（自动填充）
	TraceID       string         // OpenTelemetry trace ID
	SpanID        string         // OpenTelemetry span ID
	ParentSpanID  string         // OpenTelemetry parent span ID
	Region        string         // Region 标识
}

// Record 异步记录审计日志（同步写 Redis Stream，失败不阻塞）。
// 接口签名：Record(ctx, entry) —— ctx 用于提取 trace 信息。
var nilWarnLogged atomic.Bool

func Record(ctx context.Context, entry AuditEntry) {
	if globalRecorder == nil {
		if nilWarnLogged.CompareAndSwap(false, true) {
			slog.Warn("audit recorder not initialized; audit events are being dropped",
				"fix", "call audit.NewRecorder() in main.go before starting server",
				"first_dropped_event", entry.Action,
			)
		}
		return
	}
	globalRecorder.record(ctx, entry)
}