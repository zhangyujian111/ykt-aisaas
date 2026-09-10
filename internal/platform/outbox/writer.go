// Package outbox 事务信箱模式：调用方事务内同步写入，Worker 异步消费。
// 用于 memory 写入、抽取、摘要等异步可靠处理场景。
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MessageType 消息类型常量。
const (
	TypeMemoryWrite     = "memory_write"
	TypeMemoryExtract   = "memory_extract"
	TypeMemorySummarize = "memory_summarize"
)

// OutboxMessage 信箱消息。
type OutboxMessage struct {
	TenantID string
	DeviceID string
	Payload  any    // 序列化为 JSON 的载荷
	Type     string // 消息类型，见 Type* 常量
}

// Writer 信箱写入器接口。
type Writer interface {
	// Write 同步写入 outbox 表（调用方事务内执行），返回记录 ID。
	Write(ctx context.Context, msg OutboxMessage) (int64, error)
}

// MemoryOutboxWriter 基于 MySQL 的信箱写入器。
// 使用 *sql.DB 而非 *gorm.DB，因为 outbox 是平台级基础设施，不参与多租户过滤。
type MemoryOutboxWriter struct {
	db *sql.DB
}

// NewWriter 构造信箱写入器。
func NewWriter(db *sql.DB) *MemoryOutboxWriter {
	return &MemoryOutboxWriter{db: db}
}

// Write 同步写入一条 outbox 记录。
func (w *MemoryOutboxWriter) Write(ctx context.Context, msg OutboxMessage) (int64, error) {
	payload, err := json.Marshal(msg.Payload)
	if err != nil {
		return 0, fmt.Errorf("outbox write: marshal payload: %w", err)
	}

	result, err := w.db.ExecContext(ctx,
		`INSERT INTO ykt_aisaas_memory_outbox (tenant_id, device_id, payload, type, status)
		 VALUES (?, ?, ?, ?, 'pending')`,
		msg.TenantID, msg.DeviceID, string(payload), msg.Type,
	)
	if err != nil {
		return 0, fmt.Errorf("outbox write: insert: %w", err)
	}
	return result.LastInsertId()
}

// ---- 数据库行映射 ----

// OutboxRow 数据库行映射。
type OutboxRow struct {
	ID         int64
	TenantID   string
	DeviceID   string
	Type       string
	Payload    string
	RetryCount int
}

// Scan 从 sql.Rows 扫描当前行。
func (r *OutboxRow) Scan(rows *sql.Rows) error {
	return rows.Scan(&r.ID, &r.TenantID, &r.DeviceID, &r.Type, &r.Payload, &r.RetryCount)
}

// ToMessage 转换为 OutboxMessage。
func (r *OutboxRow) ToMessage() OutboxMessage {
	return OutboxMessage{
		TenantID: r.TenantID,
		DeviceID: r.DeviceID,
		Payload:  json.RawMessage(r.Payload),
		Type:     r.Type,
	}
}

// ---- 指数退避 ----

var backoffDurations = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
	60 * time.Second,
}

// Backoff 计算指数退避时间。
// retryCount 从 0 开始；最大退避 60s。
func Backoff(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount >= len(backoffDurations) {
		return backoffDurations[len(backoffDurations)-1]
	}
	return backoffDurations[retryCount]
}

// MaxRetry 最大重试次数。
const MaxRetry = 10