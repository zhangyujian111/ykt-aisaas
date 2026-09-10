package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// globalRecorder 全局审计记录器（由 main 装配）。
var globalRecorder *Recorder

// Recorder 审计日志记录器：同步写 Redis Stream → 后台 consumer 批量落库。
type Recorder struct {
	rdb    *redisx.Client
	db     *gorm.DB
	cfg    Config
	wg     sync.WaitGroup
	stopCh chan struct{}
}

// NewRecorder 构造并启动 consumer。
func NewRecorder(rdb *redisx.Client, db *gorm.DB, cfg Config) *Recorder {
	if cfg.StreamName == "" {
		cfg = DefaultConfig()
	}
	r := &Recorder{
		rdb:    rdb,
		db:     db,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
	r.wg.Add(1)
	go r.consumer()
	globalRecorder = r
	return r
}

// Start 启动 consumer（兼容 NewRecorder 已自动启动的场景）。
func (r *Recorder) Start() {
	// consumer 已在 NewRecorder 中启动
}

// Stop 优雅停止 consumer。
func (r *Recorder) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

// record 内部：同步写 Redis Stream（XADD + MAXLEN 裁剪）。
func (r *Recorder) record(ctx context.Context, entry AuditEntry) {
	if entry.EventTime.IsZero() {
		entry.EventTime = time.Now()
	}
	if entry.EventID == "" {
		entry.EventID = uuidv7()
	}
	// 脱敏 ActionDetail
	if entry.ActionDetail != nil {
		entry.ActionDetail = redactValue(entry.ActionDetail)
	}
	// 脱敏 ErrorMessage
	entry.ErrorMessage = redactString(entry.ErrorMessage)

	payload, err := json.Marshal(entry)
	if err != nil {
		slog.Warn("audit marshal failed", "err", err)
		return
	}

	// XADD to Redis Stream
	err = r.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: r.cfg.StreamName,
		MaxLen: r.cfg.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(payload)},
	}).Err()
	if err != nil {
		slog.Warn("audit xadd failed", "err", err, "action", entry.Action)
	}
}

// consumer 后台消费 Stream → 批量落库 ykt_aisaas_audit_log。
func (r *Recorder) consumer() {
	defer r.wg.Done()

	consumerName := fmt.Sprintf("audit-consumer-%d", os.Getpid())
	groupName := "aisaas-audit-consumers"

	// 创建 consumer group（幂等）
	ctx := context.Background()
	if err := r.rdb.XGroupCreateMkStream(ctx, r.cfg.StreamName, groupName, "0").Err(); err != nil {
		// 忽略 "BUSYGROUP Consumer Group name already exists" 错误
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			slog.Warn("audit xgroup create", "err", err)
		}
	}

	ticker := time.NewTicker(r.cfg.FlushInterval)
	defer ticker.Stop()

	buf := make([]AuditEntry, 0, r.cfg.BatchSize)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := r.flushDB(context.Background(), buf); err != nil {
			slog.Error("audit flush db failed", "rows", len(buf), "err", err)
		}
		buf = buf[:0]
	}

	for {
		select {
		case <-r.stopCh:
			flush()
			return
		case <-ticker.C:
			flush()
		default:
			// XREADGROUP 阻塞读取
			streams, err := r.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{r.cfg.StreamName, ">"},
				Count:    int64(r.cfg.BatchSize),
				Block:    time.Second,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					slog.Error("audit xreadgroup error", "err", err)
				}
				continue
			}
			for _, stream := range streams {
				for _, msg := range stream.Messages {
					payload, ok := msg.Values["payload"].(string)
					if !ok {
						continue
					}
					var entry AuditEntry
					if err := json.Unmarshal([]byte(payload), &entry); err != nil {
						slog.Warn("audit unmarshal failed", "err", err)
						continue
					}
					buf = append(buf, entry)
					// XACK 确认
					r.rdb.XAck(ctx, r.cfg.StreamName, groupName, msg.ID)
				}
			}
			if len(buf) >= r.cfg.BatchSize {
				flush()
			}
		}
	}
}

// flushDB 批量落库。
func (r *Recorder) flushDB(ctx context.Context, entries []AuditEntry) error {
	// 使用 SkipHooks 跳过租户插件（audit_log 是系统表）
	return r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_audit_log").
		CreateInBatches(entries, r.cfg.BatchSize).Error
}

// uuidv7 生成 UUID v7（时间有序，基于 google/uuid）。
func uuidv7() string {
	id, err := uuid.NewV7()
	if err != nil {
		// fallback to uuid v4
		return uuid.NewString()
	}
	return id.String()
}