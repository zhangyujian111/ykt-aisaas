package abtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/observability"
)

// =============================================================================
// 领域模型
// =============================================================================

// Experiment 实验定义。
type Experiment struct {
	ID                int64             `json:"id,omitempty"`
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	Status            ExperimentStatus  `json:"status"`
	Hypothesis        string            `json:"hypothesis,omitempty"`
	PrimaryMetric     string            `json:"primaryMetric"`
	SecondaryMetrics  []string          `json:"secondaryMetrics,omitempty"`
	MinSampleSize     int               `json:"minSampleSize"`
	ExpectedEffectSize float64          `json:"expectedEffectSize,omitempty"`
	StatisticalPower  float64           `json:"statisticalPower"`
	SignificanceLevel float64           `json:"significanceLevel"`
	CreatedBy         int64             `json:"createdBy"`
	StartedAt         *time.Time        `json:"startedAt,omitempty"`
	EndedAt           *time.Time        `json:"endedAt,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
	Variants          []*Variant        `json:"variants,omitempty"`
}

// Variant 实验变体。
type Variant struct {
	ID                int64             `json:"id,omitempty"`
	ExperimentID      int64             `json:"experimentId"`
	Name              string            `json:"name"`
	AllocationPercent float64           `json:"allocationPercent"`
	ConfigJSON        string            `json:"config,omitempty"`
	IsControl         bool              `json:"isControl"`
}

// Event 实验事件。
type Event struct {
	ID              int64             `json:"id,omitempty"`
	ExperimentID    int64             `json:"experimentId"`
	VariantID       int64             `json:"variantId"`
	UserID          string            `json:"userId"`
	MetricName      string            `json:"metricName"`
	MetricValue     float64           `json:"metricValue"`
	PropertiesJSON  string            `json:"properties,omitempty"`
	RecordedAt      time.Time         `json:"recordedAt"`
}

// =============================================================================
// GORM 数据行映射
// =============================================================================

type experimentDO struct {
	ID                 int64           `gorm:"column:id;primaryKey;autoIncrement"`
	Name               string          `gorm:"column:name"`
	Description        sql.NullString  `gorm:"column:description"`
	Status             string          `gorm:"column:status"`
	Hypothesis         sql.NullString  `gorm:"column:hypothesis"`
	PrimaryMetric      sql.NullString  `gorm:"column:primary_metric"`
	SecondaryMetrics   sql.NullString  `gorm:"column:secondary_metrics"`
	MinSampleSize      int             `gorm:"column:min_sample_size"`
	ExpectedEffectSize sql.NullFloat64 `gorm:"column:expected_effect_size"`
	StatisticalPower   float64         `gorm:"column:statistical_power"`
	SignificanceLevel  float64         `gorm:"column:significance_level"`
	CreatedBy          sql.NullInt64   `gorm:"column:created_by"`
	StartedAt          sql.NullTime    `gorm:"column:started_at"`
	EndedAt            sql.NullTime    `gorm:"column:ended_at"`
	CreatedAt          time.Time       `gorm:"column:created_at"`
	UpdatedAt          time.Time       `gorm:"column:updated_at"`
}

func (experimentDO) TableName() string { return "ykt_aisaas_ab_experiments" }

type variantDO struct {
	ID                int64          `gorm:"column:id;primaryKey;autoIncrement"`
	ExperimentID      int64          `gorm:"column:experiment_id"`
	Name              string         `gorm:"column:name"`
	AllocationPercent float64        `gorm:"column:allocation_percent"`
	Config            sql.NullString `gorm:"column:config"`
	IsControl         bool           `gorm:"column:is_control"`
}

func (variantDO) TableName() string { return "ykt_aisaas_ab_variants" }

type eventDO struct {
	ID           int64          `gorm:"column:id;primaryKey;autoIncrement"`
	ExperimentID int64          `gorm:"column:experiment_id"`
	VariantID    int64          `gorm:"column:variant_id"`
	UserID       string         `gorm:"column:user_id"`
	MetricName   sql.NullString `gorm:"column:metric_name"`
	MetricValue  sql.NullFloat64 `gorm:"column:metric_value"`
	Properties   sql.NullString `gorm:"column:properties"`
	RecordedAt   time.Time      `gorm:"column:recorded_at;autoCreateTime"`
}

func (eventDO) TableName() string { return "ykt_aisaas_ab_events" }

// =============================================================================
// MetricsCollector 指标收集器接口
// =============================================================================

// MetricsCollector 用于将 A/B 事件实时推送到 OTel / Prometheus。
type MetricsCollector interface {
	RecordABEvent(ctx context.Context, event *Event)
}

// otelMetricsCollector 基于 OTel 的指标收集器实现。
type otelMetricsCollector struct{}

// NewOTelMetricsCollector 创建 OTel 指标收集器。
func NewOTelMetricsCollector() MetricsCollector {
	return &otelMetricsCollector{}
}

// RecordABEvent 将 A/B 事件记录为 OTel 指标。
func (c *otelMetricsCollector) RecordABEvent(ctx context.Context, event *Event) {
	// 使用 OTel 已注册的指标（A/B 事件可复用现有 metering 管道）
	// 这里作为扩展点：后续可接入专门的 ab_test_events counter
	_ = ctx
	_ = event
	slog.Debug("ab event recorded to otel",
		"experimentId", event.ExperimentID,
		"variantId", event.VariantID,
		"metric", event.MetricName,
		"value", event.MetricValue,
	)
}

// =============================================================================
// ABTestService 实验服务
// =============================================================================

// ABTestService A/B 测试实验服务。
// 提供实验生命周期管理、变体分配（粘性哈希）、事件记录和统计分析。
type ABTestService struct {
	db      *gorm.DB
	redis   *redis.Client
	metrics MetricsCollector

	mu       sync.RWMutex
	experiments map[string]*Experiment // 按 name 缓存
}

// NewABTestService 创建实验服务。
func NewABTestService(db *gorm.DB, redis *redis.Client) *ABTestService {
	return &ABTestService{
		db:          db,
		redis:       redis,
		metrics:     NewOTelMetricsCollector(),
		experiments: make(map[string]*Experiment),
	}
}

// SetMetricsCollector 设置自定义指标收集器（测试用）。
func (s *ABTestService) SetMetricsCollector(mc MetricsCollector) {
	s.metrics = mc
}

// =============================================================================
// 实验生命周期
// =============================================================================

// CreateExperiment 创建实验及变体（事务）。
func (s *ABTestService) CreateExperiment(ctx context.Context, exp *Experiment) (*Experiment, error) {
	// 校验
	if exp.Name == "" {
		return nil, fmt.Errorf("experiment name is required")
	}
	if len(exp.Variants) == 0 {
		return nil, fmt.Errorf("at least one variant is required")
	}
	if err := s.validateAllocation(exp.Variants); err != nil {
		return nil, err
	}

	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. 插入实验
	secondaryJSON, _ := json.Marshal(exp.SecondaryMetrics)
	expDO := experimentDO{
		Name:               exp.Name,
		Description:        sql.NullString{String: exp.Description, Valid: exp.Description != ""},
		Status:             string(StatusDraft),
		Hypothesis:         sql.NullString{String: exp.Hypothesis, Valid: exp.Hypothesis != ""},
		PrimaryMetric:      sql.NullString{String: exp.PrimaryMetric, Valid: exp.PrimaryMetric != ""},
		SecondaryMetrics:   sql.NullString{String: string(secondaryJSON), Valid: len(exp.SecondaryMetrics) > 0},
		MinSampleSize:      exp.MinSampleSize,
		ExpectedEffectSize: sql.NullFloat64{Float64: exp.ExpectedEffectSize, Valid: exp.ExpectedEffectSize > 0},
		StatisticalPower:   exp.StatisticalPower,
		SignificanceLevel:  exp.SignificanceLevel,
		CreatedBy:          sql.NullInt64{Int64: exp.CreatedBy, Valid: exp.CreatedBy > 0},
	}

	if err := tx.Create(&expDO).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("create experiment: %w", err)
	}
	exp.ID = expDO.ID
	exp.Status = StatusDraft
	exp.CreatedAt = expDO.CreatedAt
	exp.UpdatedAt = expDO.UpdatedAt

	// 2. 插入变体
	for _, v := range exp.Variants {
		v.ExperimentID = exp.ID
		vDO := variantDO{
			ExperimentID:      v.ExperimentID,
			Name:              v.Name,
			AllocationPercent: v.AllocationPercent,
			Config:            sql.NullString{String: v.ConfigJSON, Valid: v.ConfigJSON != ""},
			IsControl:         v.IsControl,
		}
		if err := tx.Create(&vDO).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("create variant %s: %w", v.Name, err)
		}
		v.ID = vDO.ID
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("commit experiment: %w", err)
	}

	slog.Info("ab experiment created",
		"id", exp.ID,
		"name", exp.Name,
		"variants", len(exp.Variants),
	)

	return exp, nil
}

// StartExperiment 启动实验（draft → running）。
func (s *ABTestService) StartExperiment(ctx context.Context, experimentID int64) error {
	exp, err := s.loadExperiment(ctx, experimentID)
	if err != nil {
		return err
	}

	if !CanTransition(exp.Status, StatusRunning) {
		return fmt.Errorf("cannot start experiment in status %s", exp.Status)
	}

	now := time.Now()
	result := s.db.WithContext(ctx).Model(&experimentDO{}).
		Where("id = ?", experimentID).
		Updates(map[string]any{
			"status":     string(StatusRunning),
			"started_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("start experiment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrExperimentNotFound
	}

	slog.Info("ab experiment started", "id", experimentID, "startedAt", now)
	return nil
}

// PauseExperiment 暂停实验（running → paused）。
func (s *ABTestService) PauseExperiment(ctx context.Context, experimentID int64) error {
	return s.transitionStatus(ctx, experimentID, StatusPaused)
}

// StopExperiment 完成实验（running/paused → completed）。
func (s *ABTestService) StopExperiment(ctx context.Context, experimentID int64) error {
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&experimentDO{}).
		Where("id = ? AND status IN ?", experimentID, []string{string(StatusRunning), string(StatusPaused)}).
		Updates(map[string]any{
			"status":   string(StatusCompleted),
			"ended_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("stop experiment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrExperimentNotFound
	}

	slog.Info("ab experiment stopped", "id", experimentID, "endedAt", now)
	return nil
}

// transitionStatus 通用状态转移。
func (s *ABTestService) transitionStatus(ctx context.Context, experimentID int64, target ExperimentStatus) error {
	exp, err := s.loadExperiment(ctx, experimentID)
	if err != nil {
		return err
	}

	if !CanTransition(exp.Status, target) {
		return fmt.Errorf("cannot transition experiment from %s to %s", exp.Status, target)
	}

	result := s.db.WithContext(ctx).Model(&experimentDO{}).
		Where("id = ?", experimentID).
		Update("status", string(target))
	if result.Error != nil {
		return fmt.Errorf("transition status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrExperimentNotFound
	}

	slog.Info("ab experiment status transition", "id", experimentID, "from", exp.Status, "to", target)
	return nil
}

// =============================================================================
// 变体分配（粘性哈希）
// =============================================================================

// AssignVariant 为指定用户分配变体（粘性）。
//
// 算法：
//  1. 检查 Redis 缓存（24h TTL），命中直接返回
//  2. 使用 SHA-256 哈希计算确定性桶
//  3. 根据 allocation_percent 选择变体
//  4. 持久化到 DB + 写入 Redis 缓存
func (s *ABTestService) AssignVariant(ctx context.Context, experimentName, userID string, tenantID int64) (*Variant, error) {
	// 1. 加载实验（含变体）
	exp, err := s.loadRunningExperimentWithVariants(ctx, experimentName)
	if err != nil {
		return nil, err
	}

	// 2. 检查 Redis 缓存
	cacheKey := fmt.Sprintf("ab:assign:%s:%s", experimentName, userID)
	cached, redisErr := s.redis.Get(ctx, cacheKey).Result()
	if redisErr == nil && cached != "" {
		// 命中缓存：从已加载的变体中匹配
		for _, v := range exp.Variants {
			if v.Name == cached {
				return v, nil
			}
		}
		// 缓存的变体名已失效（变体被删除），继续走分配流程
		slog.Warn("ab assignment cached variant not found, re-assigning",
			"experiment", experimentName, "user", userID, "cached", cached)
	}

	// 3. 哈希分配
	bucket := HashBucket(experimentName, userID)
	selected := SelectVariant(bucket, exp.Variants)
	if selected == nil {
		return nil, ErrVariantNotFound
	}

	// 4. 持久化分配（INSERT IGNORE 保证幂等）
	result := s.db.WithContext(ctx).Table("ykt_aisaas_ab_assignments").
		Session(&gorm.Session{SkipHooks: true}).
		Exec(`
			INSERT IGNORE INTO ykt_aisaas_ab_assignments (experiment_id, variant_id, user_id, tenant_id)
			VALUES (?, ?, ?, ?)
		`, exp.ID, selected.ID, userID, tenantID)
	if result.Error != nil {
		slog.Warn("ab assignment persist failed", "err", result.Error)
		// 非阻塞：分配失败不阻断用户请求，仍返回变体
	}

	// 5. 缓存分配（24h TTL）
	if err := s.redis.Set(ctx, cacheKey, selected.Name, 24*time.Hour).Err(); err != nil {
		slog.Warn("ab assignment cache failed", "err", err)
	}

	return selected, nil
}

// =============================================================================
// 事件记录
// =============================================================================

// RecordEvent 记录实验事件。
//
// 双写策略：
//  1. 异步写 DB（goroutine，不阻塞请求）
//  2. 同步推送到 OTel Metrics（实时分析）
func (s *ABTestService) RecordEvent(ctx context.Context, event *Event) error {
	// 1. 异步写 DB
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		evtDO := eventDO{
			ExperimentID: event.ExperimentID,
			VariantID:    event.VariantID,
			UserID:       event.UserID,
			MetricName:   sql.NullString{String: event.MetricName, Valid: event.MetricName != ""},
			MetricValue:  sql.NullFloat64{Float64: event.MetricValue, Valid: true},
			Properties:   sql.NullString{String: event.PropertiesJSON, Valid: event.PropertiesJSON != ""},
		}
		if err := s.db.WithContext(bgCtx).Table("ykt_aisaas_ab_events").
			Session(&gorm.Session{SkipHooks: true}).Create(&evtDO).Error; err != nil {
			slog.Error("ab event persist failed",
				"experimentId", event.ExperimentID,
				"metric", event.MetricName,
				"err", err,
			)
		}
	}()

	// 2. 实时推送 OTel 指标
	if s.metrics != nil {
		s.metrics.RecordABEvent(ctx, event)
	}

	// 记录 observability 标准指标
	observability.DBQueriesTotal.Add(ctx, 1)

	return nil
}

// =============================================================================
// 查询
// =============================================================================

// GetExperiment 获取实验详情（含变体）。
func (s *ABTestService) GetExperiment(ctx context.Context, experimentID int64) (*Experiment, error) {
	return s.loadExperimentWithVariants(ctx, experimentID)
}

// GetExperimentByName 按名称获取实验详情（含变体）。
func (s *ABTestService) GetExperimentByName(ctx context.Context, name string) (*Experiment, error) {
	var row experimentDO
	if err := s.db.WithContext(ctx).Table("ykt_aisaas_ab_experiments").
		Where("name = ?", name).First(&row).Error; err != nil {
		return nil, ErrExperimentNotFound
	}

	exp := s.rowToExperiment(row)
	variants, err := s.loadVariants(ctx, exp.ID)
	if err != nil {
		return nil, err
	}
	exp.Variants = variants
	return exp, nil
}

// ListExperiments 列出实验（按状态过滤）。
func (s *ABTestService) ListExperiments(ctx context.Context, status string, limit int) ([]*Experiment, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []experimentDO
	query := s.db.WithContext(ctx).Table("ykt_aisaas_ab_experiments")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list experiments: %w", err)
	}

	experiments := make([]*Experiment, 0, len(rows))
	for _, row := range rows {
		experiments = append(experiments, s.rowToExperiment(row))
	}
	return experiments, nil
}

// =============================================================================
// 内部辅助
// =============================================================================

// loadExperiment 加载实验（不含变体）。
func (s *ABTestService) loadExperiment(ctx context.Context, experimentID int64) (*Experiment, error) {
	var row experimentDO
	if err := s.db.WithContext(ctx).Table("ykt_aisaas_ab_experiments").
		Where("id = ?", experimentID).First(&row).Error; err != nil {
		return nil, ErrExperimentNotFound
	}
	return s.rowToExperiment(row), nil
}

// loadExperimentWithVariants 加载实验（含变体）。
func (s *ABTestService) loadExperimentWithVariants(ctx context.Context, experimentID int64) (*Experiment, error) {
	exp, err := s.loadExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}

	variants, err := s.loadVariants(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	exp.Variants = variants
	return exp, nil
}

// loadRunningExperimentWithVariants 加载运行中的实验（含变体）。
func (s *ABTestService) loadRunningExperimentWithVariants(ctx context.Context, name string) (*Experiment, error) {
	var row experimentDO
	if err := s.db.WithContext(ctx).Table("ykt_aisaas_ab_experiments").
		Where("name = ? AND status = ?", name, StatusRunning).First(&row).Error; err != nil {
		return nil, ErrExperimentNotRunning
	}

	exp := s.rowToExperiment(row)
	variants, err := s.loadVariants(ctx, exp.ID)
	if err != nil {
		return nil, err
	}
	exp.Variants = variants
	return exp, nil
}

// loadVariants 加载实验变体。
func (s *ABTestService) loadVariants(ctx context.Context, experimentID int64) ([]*Variant, error) {
	var rows []variantDO
	if err := s.db.WithContext(ctx).Table("ykt_aisaas_ab_variants").
		Where("experiment_id = ?", experimentID).
		Order("allocation_percent ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load variants: %w", err)
	}

	variants := make([]*Variant, 0, len(rows))
	for _, row := range rows {
		cfg := ""
		if row.Config.Valid {
			cfg = row.Config.String
		}
		variants = append(variants, &Variant{
			ID:                row.ID,
			ExperimentID:      row.ExperimentID,
			Name:              row.Name,
			AllocationPercent: row.AllocationPercent,
			ConfigJSON:        cfg,
			IsControl:         row.IsControl,
		})
	}
	return variants, nil
}

// fetchEvents 获取实验事件（指定变体）。
func (s *ABTestService) fetchEvents(ctx context.Context, experimentID, variantID int64) ([]*Event, error) {
	var rows []eventDO
	if err := s.db.WithContext(ctx).Table("ykt_aisaas_ab_events").
		Where("experiment_id = ? AND variant_id = ?", experimentID, variantID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}

	events := make([]*Event, 0, len(rows))
	for _, row := range rows {
		evt := &Event{
			ID:           row.ID,
			ExperimentID: row.ExperimentID,
			VariantID:    row.VariantID,
			UserID:       row.UserID,
			RecordedAt:   row.RecordedAt,
		}
		if row.MetricName.Valid {
			evt.MetricName = row.MetricName.String
		}
		if row.MetricValue.Valid {
			evt.MetricValue = row.MetricValue.Float64
		}
		if row.Properties.Valid {
			evt.PropertiesJSON = row.Properties.String
		}
		events = append(events, evt)
	}
	return events, nil
}

// rowToExperiment 将 DB 行转为 Experiment 结构体。
func (s *ABTestService) rowToExperiment(row experimentDO) *Experiment {
	exp := &Experiment{
		ID:               row.ID,
		Name:             row.Name,
		Status:           ExperimentStatus(row.Status),
		MinSampleSize:    row.MinSampleSize,
		StatisticalPower: row.StatisticalPower,
		SignificanceLevel: row.SignificanceLevel,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}

	if row.Description.Valid {
		exp.Description = row.Description.String
	}
	if row.Hypothesis.Valid {
		exp.Hypothesis = row.Hypothesis.String
	}
	if row.PrimaryMetric.Valid {
		exp.PrimaryMetric = row.PrimaryMetric.String
	}
	if row.SecondaryMetrics.Valid {
		var metrics []string
		if err := json.Unmarshal([]byte(row.SecondaryMetrics.String), &metrics); err == nil {
			exp.SecondaryMetrics = metrics
		}
	}
	if row.ExpectedEffectSize.Valid {
		exp.ExpectedEffectSize = row.ExpectedEffectSize.Float64
	}
	if row.CreatedBy.Valid {
		exp.CreatedBy = row.CreatedBy.Int64
	}
	if row.StartedAt.Valid {
		exp.StartedAt = &row.StartedAt.Time
	}
	if row.EndedAt.Valid {
		exp.EndedAt = &row.EndedAt.Time
	}

	return exp
}

// validateAllocation 校验变体分配比例总和为 100。
func (s *ABTestService) validateAllocation(variants []*Variant) error {
	var total float64
	for _, v := range variants {
		total += v.AllocationPercent
	}
	if total < 99.99 || total > 100.01 {
		return fmt.Errorf("%w: got %.2f", ErrInvalidAllocation, total)
	}

	hasControl := false
	for _, v := range variants {
		if v.IsControl {
			hasControl = true
			break
		}
	}
	if !hasControl {
		return ErrNoControlVariant
	}

	return nil
}