// Package outbox 事务信箱模式：Worker 异步消费。
package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// MessageHandler 消息处理器接口。
// 由调用方实现具体业务逻辑（如 memory 写入、抽取、摘要）。
type MessageHandler interface {
	// Handle 处理 outbox 消息。返回 error 表示处理失败，Worker 将重试。
	Handle(ctx context.Context, msg OutboxMessage) error
}

// Worker 信箱消费者接口。
type Worker interface {
	// Start 启动 worker 轮询循环（非阻塞，内部启动 goroutine）。
	Start(ctx context.Context) error
	// Stop 优雅停止 worker。
	Stop() error
}

// MemoryOutboxWorker 基于 MySQL 的信箱消费者。
// 单 goroutine 轮询 + CAS 乐观锁防止多实例重复消费。
type MemoryOutboxWorker struct {
	db        *sql.DB
	handler   MessageHandler
	interval  time.Duration
	batchSize int
	workerID  string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// WorkerConfig Worker 配置。
type WorkerConfig struct {
	Interval  time.Duration // 轮询间隔，默认 1s
	BatchSize int           // 每批处理数量，默认 10
}

// DefaultWorkerConfig 默认 Worker 配置。
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Interval:  1 * time.Second,
		BatchSize: 10,
	}
}

// NewWorker 构造信箱 Worker。
func NewWorker(db *sql.DB, handler MessageHandler, cfg WorkerConfig) *MemoryOutboxWorker {
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	return &MemoryOutboxWorker{
		db:        db,
		handler:   handler,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
		workerID:  fmt.Sprintf("outbox-%d-%d", os.Getpid(), time.Now().UnixNano()%10000),
		stopCh:    make(chan struct{}),
	}
}

// Start 启动 worker（非阻塞，内部启动 goroutine）。
func (w *MemoryOutboxWorker) Start(ctx context.Context) error {
	w.wg.Add(1)
	go w.loop(ctx)
	slog.Info("outbox worker started",
		"workerId", w.workerID,
		"interval", w.interval,
		"batchSize", w.batchSize,
	)
	return nil
}

// Stop 优雅停止 worker（最多等待 10s）。
func (w *MemoryOutboxWorker) Stop() error {
	close(w.stopCh)
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("outbox worker stopped")
	case <-time.After(10 * time.Second):
		slog.Warn("outbox worker stop timeout, forcing exit")
	}
	return nil
}

// loop 主轮询循环。
func (w *MemoryOutboxWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// 启动时立即处理一批
	w.processBatch(ctx)

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

// processBatch 处理一批 pending 消息（CAS 乐观锁防止重复消费）。
func (w *MemoryOutboxWorker) processBatch(ctx context.Context) {
	// 1. SELECT pending 消息（status='pending' 且 next_retry_at 已到期）
	rows, err := w.db.QueryContext(ctx,
		`SELECT id, tenant_id, device_id, type, payload, retry_count
		 FROM ykt_aisaas_memory_outbox
		 WHERE status = 'pending'
		   AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		 ORDER BY id
		 LIMIT ?`, w.batchSize,
	)
	if err != nil {
		slog.Error("outbox select pending failed", "err", err)
		return
	}
	defer rows.Close()

	var outboxRows []*OutboxRow
	for rows.Next() {
		r := &OutboxRow{}
		if err := r.Scan(rows); err != nil {
			slog.Error("outbox scan row failed", "err", err)
			continue
		}
		outboxRows = append(outboxRows, r)
	}
	if err := rows.Err(); err != nil {
		slog.Error("outbox rows iteration error", "err", err)
		return
	}

	if len(outboxRows) == 0 {
		return
	}

	// 2. CAS 乐观锁：UPDATE status='processing' WHERE status='pending' AND id=?
	//    只有 CAS 成功的记录才处理（防止多 worker 实例重复消费）
	var claimed []*OutboxRow
	for _, r := range outboxRows {
		result, err := w.db.ExecContext(ctx,
			`UPDATE ykt_aisaas_memory_outbox
			 SET status = 'processing', worker_id = ?, updated_at = NOW()
			 WHERE id = ? AND status = 'pending'`,
			w.workerID, r.ID,
		)
		if err != nil {
			slog.Error("outbox claim failed", "id", r.ID, "err", err)
			continue
		}
		affected, _ := result.RowsAffected()
		if affected > 0 {
			claimed = append(claimed, r)
		}
	}

	// 3. 处理 claimed 消息
	for _, r := range claimed {
		w.processOne(ctx, r)
	}
}

// processOne 处理单条 outbox 消息。
func (w *MemoryOutboxWorker) processOne(ctx context.Context, r *OutboxRow) {
	msg := r.ToMessage()

	// 带超时的 context
	procCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := w.handler.Handle(procCtx, msg); err != nil {
		slog.Error("outbox handler failed",
			"id", r.ID,
			"type", r.Type,
			"retryCount", r.RetryCount,
			"err", err,
		)
		w.markFailed(ctx, r, err)
		return
	}

	// 成功：标记 done
	_, err := w.db.ExecContext(ctx,
		`UPDATE ykt_aisaas_memory_outbox
		 SET status = 'done', updated_at = NOW()
		 WHERE id = ?`,
		r.ID,
	)
	if err != nil {
		slog.Error("outbox mark done failed", "id", r.ID, "err", err)
	}
}

// markFailed 标记失败并计算重试策略。
func (w *MemoryOutboxWorker) markFailed(ctx context.Context, r *OutboxRow, err error) {
	newRetry := r.RetryCount + 1

	if newRetry > MaxRetry {
		// 超过最大重试次数 → failed + DLQ
		_, dbErr := w.db.ExecContext(ctx,
			`UPDATE ykt_aisaas_memory_outbox
			 SET status = 'failed', retry_count = ?, error_message = ?, updated_at = NOW()
			 WHERE id = ?`,
			newRetry, err.Error(), r.ID,
		)
		if dbErr != nil {
			slog.Error("outbox mark failed(final) error", "id", r.ID, "err", dbErr)
		}
		slog.Error("outbox max retry exceeded, moved to failed",
			"id", r.ID,
			"type", r.Type,
			"retryCount", newRetry,
			"tenantId", r.TenantID,
			"deviceId", r.DeviceID,
		)
		return
	}

	// 计算退避时间（基于当前 retryCount，即已失败次数）
	// retryCount=0 → 1s, retryCount=1 → 2s, retryCount=2 → 4s, ...
	nextRetry := time.Now().Add(Backoff(r.RetryCount))

	_, dbErr := w.db.ExecContext(ctx,
		`UPDATE ykt_aisaas_memory_outbox
		 SET status = 'pending', retry_count = ?, next_retry_at = ?, error_message = ?, updated_at = NOW()
		 WHERE id = ?`,
		newRetry, nextRetry, err.Error(), r.ID,
	)
	if dbErr != nil {
		slog.Error("outbox mark failed(retry) error", "id", r.ID, "err", dbErr)
	}
}