package anomaly

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Rule 异常检测规则。
type Rule struct {
	ID          string        `json:"id"`
	TenantID    string        `json:"tenantId"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	RuleType    RuleType      `json:"ruleType"`
	Metric      string        `json:"metric"`
	Threshold   float64       `json:"threshold"`
	WindowSize  time.Duration `json:"-"`
	Severity    Severity      `json:"severity"`
	Enabled     bool          `json:"enabled"`
	Channels    []string      `json:"channels"`
}

// anomalyRuleDO GORM 数据行映射。
type anomalyRuleDO struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	TenantID             string         `gorm:"column:tenantId"`
	Name                 string         `gorm:"column:name"`
	Description          sql.NullString `gorm:"column:description"`
	RuleType             string         `gorm:"column:ruleType"`
	Metric               string         `gorm:"column:metric"`
	ThresholdValue       float64        `gorm:"column:thresholdValue"`
	WindowSizeMs         int            `gorm:"column:windowSizeMs"`
	Severity             string         `gorm:"column:severity"`
	Enabled              bool           `gorm:"column:enabled"`
	NotificationChannels string         `gorm:"column:notificationChannels"`
	CreatedAt            time.Time      `gorm:"column:createdAt"`
	UpdatedAt            time.Time      `gorm:"column:updatedAt"`
}

func (anomalyRuleDO) TableName() string { return "ykt_aisaas_anomaly_rules" }

// Event 异常事件。
type Event struct {
	ID          int64     `json:"id,omitempty"`
	RuleID      string    `json:"ruleId"`
	TenantID    string    `json:"tenantId"`
	TriggeredAt time.Time `json:"triggeredAt"`
	MetricValue float64   `json:"metricValue"`
	Severity    Severity  `json:"severity"`
	Message     string    `json:"message"`
	Notified    bool      `json:"notified"`
}

// anomalyEventDO GORM 数据行映射。
type anomalyEventDO struct {
	ID          int64          `gorm:"column:id;primaryKey;autoIncrement"`
	RuleID      string         `gorm:"column:ruleId"`
	TenantID    string         `gorm:"column:tenantId"`
	TriggeredAt time.Time      `gorm:"column:triggeredAt;autoCreateTime"`
	MetricValue sql.NullFloat64 `gorm:"column:metricValue"`
	Severity    string         `gorm:"column:severity"`
	Message     sql.NullString `gorm:"column:message"`
	Notified    bool           `gorm:"column:notified"`
}

func (anomalyEventDO) TableName() string { return "ykt_aisaas_anomaly_events" }

// Engine 规则引擎接口。
type Engine interface {
	Start(ctx context.Context) error
	Stop() error
	AddRule(ctx context.Context, rule Rule) error
	UpdateRule(ctx context.Context, rule Rule) error
	RemoveRule(ctx context.Context, ruleID string) error
	ListRules(ctx context.Context, tenantID string) ([]Rule, error)
	GetRule(ctx context.Context, ruleID string) (*Rule, error)
	ListEvents(ctx context.Context, tenantID string, limit int) ([]Event, error)
	GetEvents(ctx context.Context, ruleID string, limit int) ([]Event, error)
}

// AnomalyEngine 异常检测引擎实现。
type AnomalyEngine struct {
	db        *gorm.DB
	metrics   MetricsCollector
	notifier  Notifier
	interval  time.Duration

	mu       sync.RWMutex
	rules    map[string]Rule // in-memory rule cache
	stopCh   chan struct{}
	running  bool
}

// NewEngine 创建异常检测引擎。
func NewEngine(db *gorm.DB, metrics MetricsCollector, notifier Notifier) *AnomalyEngine {
	return &AnomalyEngine{
		db:       db,
		metrics:  metrics,
		notifier: notifier,
		interval: 30 * time.Second,
		rules:    make(map[string]Rule),
		stopCh:   make(chan struct{}),
	}
}

// Start 启动评估循环。
func (e *AnomalyEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.stopCh = make(chan struct{})
	e.mu.Unlock()

	// 初始加载规则
	if err := e.loadRules(ctx); err != nil {
		slog.Warn("anomaly engine initial rule load failed", "err", err)
	}

	ticker := time.NewTicker(e.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.evaluateAll(ctx)
			}
		}
	}()

	slog.Info("anomaly engine started", "interval", e.interval)
	return nil
}

// Stop 停止评估循环。
func (e *AnomalyEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return nil
	}
	e.running = false
	close(e.stopCh)
	slog.Info("anomaly engine stopped")
	return nil
}

// loadRules 从 DB 加载规则到内存缓存。
func (e *AnomalyEngine) loadRules(ctx context.Context) error {
	var rows []anomalyRuleDO
	if err := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules").
		Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return fmt.Errorf("load anomaly rules: %w", err)
	}

	rules := make(map[string]Rule, len(rows))
	for _, row := range rows {
		rule := e.rowToRule(row)
		rules[row.ID] = rule
	}

	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()

	slog.Debug("anomaly rules loaded", "count", len(rules))
	return nil
}

// rowToRule 将 DB 行转为 Rule 结构体。
func (e *AnomalyEngine) rowToRule(row anomalyRuleDO) Rule {
	var channels []string
	if row.NotificationChannels != "" {
		_ = json.Unmarshal([]byte(row.NotificationChannels), &channels)
	}
	if channels == nil {
		channels = []string{}
	}

	desc := ""
	if row.Description.Valid {
		desc = row.Description.String
	}

	return Rule{
		ID:          row.ID,
		TenantID:    row.TenantID,
		Name:        row.Name,
		Description: desc,
		RuleType:    RuleType(row.RuleType),
		Metric:      row.Metric,
		Threshold:   row.ThresholdValue,
		WindowSize:  time.Duration(row.WindowSizeMs) * time.Millisecond,
		Severity:    Severity(row.Severity),
		Enabled:     row.Enabled,
		Channels:    channels,
	}
}

// evaluateAll 评估所有规则。
func (e *AnomalyEngine) evaluateAll(ctx context.Context) {
	e.mu.RLock()
	rules := make([]Rule, 0, len(e.rules))
	for _, r := range e.rules {
		rules = append(rules, r)
	}
	e.mu.RUnlock()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		value, err := e.metrics.Collect(ctx, rule.Metric, rule.WindowSize)
		if err != nil {
			slog.Warn("anomaly metric collect failed",
				"ruleId", rule.ID, "metric", rule.Metric, "err", err)
			continue
		}

		if value > rule.Threshold {
			event := Event{
				RuleID:      rule.ID,
				TenantID:    rule.TenantID,
				TriggeredAt: time.Now(),
				MetricValue: value,
				Severity:    rule.Severity,
				Message:     fmt.Sprintf("%s exceeded threshold: %.2f > %.2f", rule.Name, value, rule.Threshold),
			}

			// 持久化事件
			if err := e.saveEvent(ctx, event); err != nil {
				slog.Error("anomaly save event failed", "ruleId", rule.ID, "err", err)
			}

			// 发送通知
			if err := e.notifier.Notify(ctx, event, rule.Channels); err != nil {
				slog.Warn("anomaly notify failed", "ruleId", rule.ID, "err", err)
			}
		}
	}
}

// saveEvent 持久化异常事件。
func (e *AnomalyEngine) saveEvent(ctx context.Context, event Event) error {
	row := anomalyEventDO{
		RuleID:      event.RuleID,
		TenantID:    event.TenantID,
		TriggeredAt: event.TriggeredAt,
		MetricValue: sql.NullFloat64{Float64: event.MetricValue, Valid: true},
		Severity:    string(event.Severity),
		Message:     sql.NullString{String: event.Message, Valid: true},
		Notified:    event.Notified,
	}
	return e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_events").
		Session(&gorm.Session{SkipHooks: true}).Create(&row).Error
}

// AddRule 添加规则。
func (e *AnomalyEngine) AddRule(ctx context.Context, rule Rule) error {
	if rule.ID == "" {
		return ErrRuleIDRequired
	}
	if !IsValidRuleType(rule.RuleType) {
		return ErrInvalidRuleType
	}
	if !IsValidSeverity(rule.Severity) {
		return ErrInvalidSeverity
	}

	channelsJSON, _ := json.Marshal(rule.Channels)
	row := anomalyRuleDO{
		ID:                   rule.ID,
		TenantID:             rule.TenantID,
		Name:                 rule.Name,
		Description:          sql.NullString{String: rule.Description, Valid: rule.Description != ""},
		RuleType:             string(rule.RuleType),
		Metric:               rule.Metric,
		ThresholdValue:       rule.Threshold,
		WindowSizeMs:         int(rule.WindowSize.Milliseconds()),
		Severity:             string(rule.Severity),
		Enabled:              rule.Enabled,
		NotificationChannels: string(channelsJSON),
	}

	if err := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules").
		Session(&gorm.Session{SkipHooks: true}).Create(&row).Error; err != nil {
		return fmt.Errorf("add anomaly rule: %w", err)
	}

	// 刷新内存缓存
	e.mu.Lock()
	e.rules[rule.ID] = rule
	e.mu.Unlock()

	return nil
}

// UpdateRule 更新规则。
func (e *AnomalyEngine) UpdateRule(ctx context.Context, rule Rule) error {
	if rule.ID == "" {
		return ErrRuleIDRequired
	}

	channelsJSON, _ := json.Marshal(rule.Channels)
	updates := map[string]any{
		"name":                 rule.Name,
		"description":          rule.Description,
		"ruleType":             string(rule.RuleType),
		"metric":               rule.Metric,
		"thresholdValue":       rule.Threshold,
		"windowSizeMs":         int(rule.WindowSize.Milliseconds()),
		"severity":             string(rule.Severity),
		"enabled":              rule.Enabled,
		"notificationChannels": string(channelsJSON),
	}

	result := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules").
		Session(&gorm.Session{SkipHooks: true}).
		Where("id = ?", rule.ID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update anomaly rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}

	// 刷新内存缓存
	e.mu.Lock()
	if rule.Enabled {
		e.rules[rule.ID] = rule
	} else {
		delete(e.rules, rule.ID)
	}
	e.mu.Unlock()

	return nil
}

// RemoveRule 删除规则。
func (e *AnomalyEngine) RemoveRule(ctx context.Context, ruleID string) error {
	result := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules").
		Session(&gorm.Session{SkipHooks: true}).
		Where("id = ?", ruleID).Delete(&anomalyRuleDO{})
	if result.Error != nil {
		return fmt.Errorf("remove anomaly rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRuleNotFound
	}

	e.mu.Lock()
	delete(e.rules, ruleID)
	e.mu.Unlock()

	return nil
}

// ListRules 列出规则。
func (e *AnomalyEngine) ListRules(ctx context.Context, tenantID string) ([]Rule, error) {
	var rows []anomalyRuleDO
	query := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules")
	if tenantID != "" {
		query = query.Where("tenantId = ?", tenantID)
	}
	if err := query.Order("createdAt DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list anomaly rules: %w", err)
	}

	rules := make([]Rule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, e.rowToRule(row))
	}
	return rules, nil
}

// GetRule 获取单个规则。
func (e *AnomalyEngine) GetRule(ctx context.Context, ruleID string) (*Rule, error) {
	var row anomalyRuleDO
	if err := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_rules").
		Where("id = ?", ruleID).First(&row).Error; err != nil {
		return nil, ErrRuleNotFound
	}
	rule := e.rowToRule(row)
	return &rule, nil
}

// ListEvents 列出所有事件（按租户过滤）。
func (e *AnomalyEngine) ListEvents(ctx context.Context, tenantID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []anomalyEventDO
	query := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_events")
	if tenantID != "" {
		query = query.Where("tenantId = ?", tenantID)
	}
	if err := query.Order("triggeredAt DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list anomaly events: %w", err)
	}

	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		evt := Event{
			ID:          row.ID,
			RuleID:      row.RuleID,
			TenantID:    row.TenantID,
			TriggeredAt: row.TriggeredAt,
			Severity:    Severity(row.Severity),
			Notified:    row.Notified,
		}
		if row.MetricValue.Valid {
			evt.MetricValue = row.MetricValue.Float64
		}
		if row.Message.Valid {
			evt.Message = row.Message.String
		}
		events = append(events, evt)
	}
	return events, nil
}

// GetEvents 获取指定规则的事件。
func (e *AnomalyEngine) GetEvents(ctx context.Context, ruleID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []anomalyEventDO
	if err := e.db.WithContext(ctx).Table("ykt_aisaas_anomaly_events").
		Where("ruleId = ?", ruleID).
		Order("triggeredAt DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get anomaly events: %w", err)
	}

	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		evt := Event{
			ID:          row.ID,
			RuleID:      row.RuleID,
			TenantID:    row.TenantID,
			TriggeredAt: row.TriggeredAt,
			Severity:    Severity(row.Severity),
			Notified:    row.Notified,
		}
		if row.MetricValue.Valid {
			evt.MetricValue = row.MetricValue.Float64
		}
		if row.Message.Valid {
			evt.Message = row.Message.String
		}
		events = append(events, evt)
	}
	return events, nil
}