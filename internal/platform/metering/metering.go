// Package metering 计量：调用后记录用量 → Redis 实时计数 + 批量异步落库。
// 对账 cron（日）比对 Redis 累计与 usage_detail 聚合（见 GO 实现方案 §4.7）。
package metering

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// Dimension 计量维度。
const (
	DimLLMTokensIn  = "llm_tokens_in"
	DimLLMTokensOut = "llm_tokens_out"
	DimTTSChars     = "tts_chars"
	DimASRSeconds   = "asr_seconds"
)

// BizType 业务类型。
const (
	BizLLM = "llm"
	BizTTS = "tts"
	BizASR = "asr"
)

// Record 一次调用的用量明细。
type Record struct {
	TenantID  int64
	APIKeyID  int64
	BizType   string
	Dimension string
	Amount    int64
	ModelID   string
	RequestID string
	Status    int8 // 1 成功 0 失败
}

// usageRow ykt_aisaas_usage_detail 行。
type usageRow struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TenantID   int64     `gorm:"column:tenantId"`
	APIKeyID   int64     `gorm:"column:apiKeyId"`
	BizType    string    `gorm:"column:bizType"`
	Dimension  string    `gorm:"column:dimension"`
	Amount     int64     `gorm:"column:amount"`
	ModelID    string    `gorm:"column:modelId"`
	RequestID  string    `gorm:"column:requestId"`
	Status     int8      `gorm:"column:status"`
	CostCents  int64     `gorm:"column:costCents"`
	CreateTime time.Time `gorm:"column:createTime;autoCreateTime"`
}

func (usageRow) TableName() string { return "ykt_aisaas_usage_detail" }

// Pricer 按模型计价（llm.Registry 实现，接口隔离）。
type Pricer interface {
	Price(modelID string) (in, out float64)
}

// Consumer 扣费回调（billing.Service 实现）。
type Consumer interface {
	Consume(ctx context.Context, tenantID int64, bizType string, costCents int64, refID string)
}

// Recorder 计量记录器。
type Recorder struct {
	rdb  *redisx.Client
	db   *gorm.DB
	ch   chan Record
	cfg  config.Metering
	wg   sync.WaitGroup
	prc  Pricer
	cons Consumer
}

// NewRecorder 启动后台批量落库 goroutine。
func NewRecorder(rdb *redisx.Client, db *gorm.DB, cfg config.Metering) (*Recorder, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.FlushSec <= 0 {
		cfg.FlushSec = 3
	}
	r := &Recorder{
		rdb: rdb, db: db, cfg: cfg,
		ch: make(chan Record, cfg.BatchSize*8),
	}
	n := min(2, runtime.NumCPU())
	for i := 0; i < n; i++ {
		r.wg.Add(1)
		go r.worker()
	}
	return r, nil
}

// BindPricer 注入计价器（main 装配）。
func (r *Recorder) BindPricer(p Pricer) { r.prc = p }

// BindConsumer 注入扣费器（main 装配）。
func (r *Recorder) BindConsumer(c Consumer) { r.cons = c }

// costCents 整分计价（usage_detail 落库用）。
func (r *Recorder) costCents(rec Record) int64 {
	return int64(r.costFloat(rec))
}

// costFloat 按维度计价（分，浮点）。LLM 按千 token 单价，其余暂不计价。
func (r *Recorder) costFloat(rec Record) float64 {
	if r.prc == nil {
		return 0
	}
	switch rec.Dimension {
	case DimLLMTokensIn, DimLLMTokensOut:
		in, out := r.prc.Price(rec.ModelID)
		price := in
		if rec.Dimension == DimLLMTokensOut {
			price = out
		}
		return float64(rec.Amount) / 1000.0 * price
	default:
		return 0
	}
}

// Record 记录用量（非阻塞；队列满时丢弃并告警，不阻塞业务）。
func (r *Recorder) Record(ctx context.Context, rec Record) {
	// Redis 实时计数（失败不阻塞）
	ym := time.Now().Format("200601")
	_ = r.rdb.IncrBy(ctx, redisx.KeyQuotaUsed(rec.TenantID, rec.Dimension, ym), rec.Amount).Err()

	select {
	case r.ch <- rec:
	default:
		slog.Warn("metering channel full, record dropped", "tenantId", rec.TenantID, "dim", rec.Dimension)
	}
}

// RecordWithCtx 从业务 ctx 提取租户并记录。
func (r *Recorder) RecordWithCtx(ctx context.Context, rec Record) {
	if tid, ok := tenant.FromSafe(ctx); ok {
		rec.TenantID = tid
	}
	if rec.TenantID == 0 {
		return
	}
	r.Record(ctx, rec)
}

func (r *Recorder) worker() {
	defer r.wg.Done()
	tick := time.NewTicker(time.Duration(r.cfg.FlushSec) * time.Second)
	defer tick.Stop()

	buf := make([]Record, 0, r.cfg.BatchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		rows := make([]usageRow, len(buf))
		for i, rec := range buf {
			rows[i] = usageRow{
				ID: ids.Next(), TenantID: rec.TenantID, APIKeyID: rec.APIKeyID,
				BizType: rec.BizType, Dimension: rec.Dimension, Amount: rec.Amount,
				ModelID: rec.ModelID, RequestID: rec.RequestID, Status: rec.Status,
				CostCents: r.costCents(rec),
			}
		}
		// usage_detail 含 tenantId 但写入时已显式带值；跳过租户插件填充
		if err := r.db.Session(&gorm.Session{SkipHooks: true, Context: context.Background()}).
			Table("ykt_aisaas_usage_detail").Create(&rows).Error; err != nil {
			slog.Error("metering flush failed", "rows", len(rows), "err", err)
			return
		}
		// 扣费：浮点累计后取整（单条 0.1 分级费用不能提前截断）
		if r.cons != nil {
			type aggKey struct {
				tid   int64
				biz   string
				model string
			}
			agg := map[aggKey]float64{}
			for _, rec := range buf {
				if rec.Status == 1 {
					agg[aggKey{rec.TenantID, rec.BizType, rec.ModelID}] += r.costFloat(rec)
				}
			}
			bg := context.Background()
			for k, cents := range agg {
				if cents >= 1 {
					r.cons.Consume(bg, k.tid, k.biz, int64(cents), "batch:"+time.Now().Format("150405"))
				}
			}
		}
		buf = buf[:0]
	}

	for {
		select {
		case rec, ok := <-r.ch:
			if !ok {
				flush()
				return
			}
			buf = append(buf, rec)
			if len(buf) >= r.cfg.BatchSize {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// Close 排空并停止。
func (r *Recorder) Close() {
	close(r.ch)
	r.wg.Wait()
}
