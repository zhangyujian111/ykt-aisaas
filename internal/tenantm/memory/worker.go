package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// Worker Redis Stream 消费者：处理 extract + summarize 任务。
type Worker struct {
	rdb            *redisx.Client
	extractW       *ExtractWorker
	summarizeW     *SummarizeWorker
	cfg            Config
	stopCh         chan struct{}
	wg             sync.WaitGroup
	dlqMaxRetries  int
	quotaGuard     *quota.Guard
	meterRecorder  *metering.Recorder
}

// NewWorker 构造 Worker。
func NewWorker(rdb *redisx.Client, extractW *ExtractWorker, summarizeW *SummarizeWorker, cfg Config, quotaGuard *quota.Guard, meterRecorder *metering.Recorder) *Worker {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 2
	}
	if cfg.DLQMaxRetries <= 0 {
		cfg.DLQMaxRetries = 3
	}
	w := &Worker{
		rdb:           rdb,
		extractW:       extractW,
		summarizeW:     summarizeW,
		cfg:            cfg,
		stopCh:         make(chan struct{}),
		dlqMaxRetries: cfg.DLQMaxRetries,
		quotaGuard:     quotaGuard,
		meterRecorder:  meterRecorder,
	}

	// 将依赖注入到 worker
	if extractW != nil {
		extractW.quotaGuard = quotaGuard
		extractW.meterRecorder = meterRecorder
	}
	if summarizeW != nil {
		summarizeW.quotaGuard = quotaGuard
		summarizeW.meterRecorder = meterRecorder
	}

	return w
}

// Start 启动 worker 协程。
func (w *Worker) Start() {
	ctx := context.Background()

	// 创建 consumer groups（幂等）
	for _, stream := range []string{StreamExtract, StreamSummarize} {
		if err := w.rdb.XGroupCreateMkStream(ctx, stream, w.cfg.ConsumerGroup, "0").Err(); err != nil {
			if err.Error() != "BUSYGROUP Consumer Group name already exists" {
				slog.Warn("memory xgroup create", "stream", stream, "err", err)
			}
		}
	}

	// 启动独立消费者分别处理 extract 和 summarize（避免饥饿）
	for i := 0; i < w.cfg.WorkerCount; i++ {
		w.wg.Add(1)
		go w.consumeExtractLoop(ctx, i)
		w.wg.Add(1)
		go w.consumeSummarizeLoop(ctx, i)
	}
	slog.Info("memory worker started", "workers", w.cfg.WorkerCount)
}

// Stop 优雅停止（10s 超时）。
func (w *Worker) Stop() {
	close(w.stopCh)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		slog.Warn("memory worker stop timeout, forcing exit")
	}
	slog.Info("memory worker stopped")
}

// PublishExtractTask 发布抽取任务到 Stream。
func (w *Worker) PublishExtractTask(ctx context.Context, task *ExtractTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal extract task: %w", err)
	}
	return w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamExtract,
		MaxLen: w.cfg.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(payload)},
	}).Err()
}

// PublishSummarizeTask 发布摘要任务到 Stream。
func (w *Worker) PublishSummarizeTask(ctx context.Context, task *SummarizeTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal summarize task: %w", err)
	}
	return w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamSummarize,
		MaxLen: w.cfg.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"payload": string(payload)},
	}).Err()
}

// consumeExtractLoop 独立消费 extract stream（避免饥饿）。
func (w *Worker) consumeExtractLoop(ctx context.Context, workerID int) {
	defer w.wg.Done()
	consumerName := fmt.Sprintf("memory-extract-%d-%d", os.Getpid(), workerID)

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}
		w.consumeStreamWithDLQ(ctx, StreamExtract, StreamExtractDLQ, consumerName, true)
	}
}

// consumeSummarizeLoop 独立消费 summarize stream（避免饥饿）。
func (w *Worker) consumeSummarizeLoop(ctx context.Context, workerID int) {
	defer w.wg.Done()
	consumerName := fmt.Sprintf("memory-summarize-%d-%d", os.Getpid(), workerID)

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}
		w.consumeStreamWithDLQ(ctx, StreamSummarize, StreamSummarizeDLQ, consumerName, false)
	}
}

// consumeStreamWithDLQ 消费单个 Stream，支持重试和 DLQ。
func (w *Worker) consumeStreamWithDLQ(ctx context.Context, streamName, dlqStreamName, consumerName string, isExtract bool) {
	streams, err := w.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    w.cfg.ConsumerGroup,
		Consumer: consumerName,
		Streams:  []string{streamName, ">"},
		Count:    1,
		Block:    2 * time.Second,
	}).Result()
	if err != nil {
		if err != redis.Nil {
			slog.Error("memory xreadgroup error", "stream", streamName, "err", err)
		}
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			payload, ok := msg.Values["payload"].(string)
			retryCount := 0
			if rc, ok := msg.Values["retryCount"].(string); ok {
				fmt.Sscanf(rc, "%d", &retryCount)
			}
			if !ok {
				w.ack(ctx, streamName, msg.ID)
				continue
			}

			var processErr error
			if isExtract {
				processErr = w.processExtract(ctx, payload)
			} else {
				processErr = w.processSummarize(ctx, payload)
			}

			if processErr != nil {
				slog.Error("memory task processing failed",
					"stream", streamName,
					"msgId", msg.ID,
					"retryCount", retryCount,
					"err", processErr,
				)

				// 失败后 ACK
				w.ack(ctx, streamName, msg.ID)

				// 超过重试次数则写入 DLQ，否则重新入队
				if retryCount >= w.dlqMaxRetries {
					w.pushToDLQ(ctx, dlqStreamName, payload, retryCount, processErr.Error())
				} else {
					w.requeueTask(ctx, streamName, payload, retryCount+1)
				}
				continue
			}
			w.ack(ctx, streamName, msg.ID)
		}
	}
}

// pushToDLQ 将失败任务写入 DLQ stream。
func (w *Worker) pushToDLQ(ctx context.Context, dlqStreamName, payload string, retryCount int, errMsg string) {
	err := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: dlqStreamName,
		MaxLen: w.cfg.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"payload":      payload,
			"retryCount":   retryCount,
			"lastError":    errMsg,
			"failedAt":     time.Now().Format(time.RFC3339),
		},
	}).Err()
	if err != nil {
		slog.Error("push to dlq failed", "stream", dlqStreamName, "err", err)
	}
}

// requeueTask 重新入队任务（带重试计数）。
func (w *Worker) requeueTask(ctx context.Context, streamName, payload string, retryCount int) {
	err := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		MaxLen: w.cfg.StreamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"payload":     payload,
			"retryCount":  retryCount,
		},
	}).Err()
	if err != nil {
		slog.Error("requeue task failed", "stream", streamName, "err", err)
	}
}

// processExtract 处理抽取消息。
func (w *Worker) processExtract(ctx context.Context, payload string) error {
	var task ExtractTask
	if err := json.Unmarshal([]byte(payload), &task); err != nil {
		return fmt.Errorf("unmarshal extract task: %w", err)
	}
	return w.extractW.ProcessExtractTask(ctx, &task)
}

// processSummarize 处理摘要消息。
func (w *Worker) processSummarize(ctx context.Context, payload string) error {
	var task SummarizeTask
	if err := json.Unmarshal([]byte(payload), &task); err != nil {
		return fmt.Errorf("unmarshal summarize task: %w", err)
	}
	return w.summarizeW.ProcessSummarizeTask(ctx, &task)
}

// ack 确认消息。
func (w *Worker) ack(ctx context.Context, stream, msgID string) {
	if err := w.rdb.XAck(ctx, stream, w.cfg.ConsumerGroup, msgID).Err(); err != nil {
		slog.Warn("memory xack failed", "stream", stream, "msgId", msgID, "err", err)
	}
}