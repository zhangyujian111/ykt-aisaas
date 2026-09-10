package metering

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// AlertChannelDLQ Redis Pub/Sub 通道，用于 P0 告警。
const AlertChannelDLQ = "aisaas:alert:dlq"

// dlqRow ykt_aisaas_metering_dlq 行。
type dlqRow struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TaskID     string    `gorm:"column:taskId"`
	Payload    string    `gorm:"column:payload"`
	RetryCount int       `gorm:"column:retryCount"`
	LastError  string    `gorm:"column:lastError"`
	CreatedAt  time.Time `gorm:"column:createdAt;autoCreateTime"`
}

func (dlqRow) TableName() string { return "ykt_aisaas_metering_dlq" }

// DLQWorker 死信队列 worker。
// 落库失败后：XADD aisaas:metering:dlq → 重试 3 次 → P0 告警。
type DLQWorker struct {
	db       *gorm.DB
	rdb      *redisx.Client
	maxRetry int
}

// NewDLQWorker 构造。
func NewDLQWorker(db *gorm.DB, rdb *redisx.Client, maxRetry int) *DLQWorker {
	if maxRetry <= 0 {
		maxRetry = 3
	}
	return &DLQWorker{db: db, rdb: rdb, maxRetry: maxRetry}
}

// Push 写入 DLQ 表。
func (w *DLQWorker) Push(ctx context.Context, taskID string, rec Record, lastErr string) error {
	payload, _ := json.Marshal(rec)
	row := dlqRow{
		TaskID:     taskID,
		Payload:    string(payload),
		RetryCount: 0,
		LastError:  lastErr,
	}
	return w.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_metering_dlq").Create(&row).Error
}

// Retry 重试 DLQ 中的记录。
func (w *DLQWorker) Retry(ctx context.Context, process func(Record) error) error {
	var rows []dlqRow
	if err := w.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_metering_dlq").
		Where("retryCount < ?", w.maxRetry).
		Order("id ASC").Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var rec Record
		if err := json.Unmarshal([]byte(row.Payload), &rec); err != nil {
			slog.Warn("dlq unmarshal failed", "taskId", row.TaskID, "err", err)
			continue
		}
		if err := process(rec); err != nil {
			// 更新重试次数
			newCount := row.RetryCount + 1
			w.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
				Table("ykt_aisaas_metering_dlq").
				Where("id = ?", row.ID).
				Updates(map[string]any{"retryCount": newCount, "lastError": err.Error()})
			if newCount >= w.maxRetry {
				slog.Error("dlq max retry exceeded, P0 alert", "taskId", row.TaskID, "retryCount", newCount, "err", err)
				// publish P0 alert to Redis Pub/Sub
				alertPayload, _ := json.Marshal(map[string]any{
					"severity":  "P0",
					"source":    "metering-dlq",
					"message":   "metering event failed after 3 retries",
					"taskId":    row.TaskID,
					"retry":     newCount,
					"lastErr":   err.Error(),
					"ts":        time.Now().UTC().Format(time.RFC3339),
				})
				if err := w.rdb.Publish(ctx, AlertChannelDLQ, alertPayload).Err(); err != nil {
					slog.Error("failed to publish DLQ alert", "err", err)
				}
			}
			continue
		}
		// 成功：删除 DLQ 记录
		w.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
			Table("ykt_aisaas_metering_dlq").
			Where("id = ?", row.ID).Delete(&dlqRow{})
	}
	return nil
}