// Package apiv1 SaaS 自有接口（/api/v1/*）— anomaly 模块 handler。
package apiv1

import (
	"time"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/anomaly"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// AnomalyHandler /api/v1/anomaly/* 接口 handler。
type AnomalyHandler struct {
	Engine anomaly.Engine
}

// ========== 请求/响应 DTO ==========

// CreateRuleReq 创建规则请求。
type CreateRuleReq struct {
	ID          string   `json:"id" binding:"required"`
	TenantID    string   `json:"tenantId"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	RuleType    string   `json:"ruleType" binding:"required"`
	Metric      string   `json:"metric" binding:"required"`
	Threshold   float64  `json:"threshold" binding:"required"`
	WindowSizeMs int     `json:"windowSizeMs" binding:"required"`
	Severity    string   `json:"severity"`
	Channels    []string `json:"channels"`
	Enabled     *bool    `json:"enabled"`
}

// UpdateRuleReq 更新规则请求。
type UpdateRuleReq struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RuleType    string   `json:"ruleType"`
	Metric      string   `json:"metric"`
	Threshold   *float64 `json:"threshold"`
	WindowSizeMs *int    `json:"windowSizeMs"`
	Severity    string   `json:"severity"`
	Channels    []string `json:"channels"`
	Enabled     *bool    `json:"enabled"`
}

// RuleResp 规则响应。
type RuleResp struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenantId"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RuleType    string   `json:"ruleType"`
	Metric      string   `json:"metric"`
	Threshold   float64  `json:"threshold"`
	WindowSizeMs int     `json:"windowSizeMs"`
	Severity    string   `json:"severity"`
	Enabled     bool     `json:"enabled"`
	Channels    []string `json:"channels"`
}

// EventResp 事件响应。
type EventResp struct {
	ID          int64   `json:"id"`
	RuleID      string  `json:"ruleId"`
	TenantID    string  `json:"tenantId"`
	TriggeredAt string  `json:"triggeredAt"`
	MetricValue float64 `json:"metricValue"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	Notified    bool    `json:"notified"`
}

// ========== Handlers ==========

// ListRules GET /api/v1/anomaly/rules
func (h *AnomalyHandler) ListRules(c *gin.Context) {
	tenantID := c.Query("tenantId")
	rules, err := h.Engine.ListRules(c.Request.Context(), tenantID)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	resp := make([]RuleResp, 0, len(rules))
	for _, r := range rules {
		resp = append(resp, ruleToResp(r))
	}
	web.OK(c, resp)
}

// GetRule GET /api/v1/anomaly/rules/:id
func (h *AnomalyHandler) GetRule(c *gin.Context) {
	ruleID := c.Param("id")
	rule, err := h.Engine.GetRule(c.Request.Context(), ruleID)
	if err != nil {
		web.Abort(c, errs.New(errs.ResourceNotFound, "rule not found: "+ruleID))
		return
	}
	web.OK(c, ruleToResp(*rule))
}

// CreateRule POST /api/v1/anomaly/rules
func (h *AnomalyHandler) CreateRule(c *gin.Context) {
	var req CreateRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	severity := "warn"
	if req.Severity != "" {
		severity = req.Severity
	}

	channels := req.Channels
	if channels == nil {
		channels = []string{}
	}

	rule := anomaly.Rule{
		ID:          req.ID,
		TenantID:    req.TenantID,
		Name:        req.Name,
		Description: req.Description,
		RuleType:    anomaly.RuleType(req.RuleType),
		Metric:      req.Metric,
		Threshold:   req.Threshold,
		WindowSize:  time.Duration(req.WindowSizeMs) * time.Millisecond,
		Severity:    anomaly.Severity(severity),
		Channels:    channels,
		Enabled:     enabled,
	}

	if err := h.Engine.AddRule(c.Request.Context(), rule); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, ruleToResp(rule))
}

// UpdateRule PUT /api/v1/anomaly/rules/:id
func (h *AnomalyHandler) UpdateRule(c *gin.Context) {
	ruleID := c.Param("id")

	var req UpdateRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	// 先获取现有规则
	existing, err := h.Engine.GetRule(c.Request.Context(), ruleID)
	if err != nil {
		web.Abort(c, errs.New(errs.ResourceNotFound, "rule not found: "+ruleID))
		return
	}

	// 部分更新
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.RuleType != "" {
		existing.RuleType = anomaly.RuleType(req.RuleType)
	}
	if req.Metric != "" {
		existing.Metric = req.Metric
	}
	if req.Threshold != nil {
		existing.Threshold = *req.Threshold
	}
	if req.WindowSizeMs != nil {
		existing.WindowSize = time.Duration(*req.WindowSizeMs) * time.Millisecond
	}
	if req.Severity != "" {
		existing.Severity = anomaly.Severity(req.Severity)
	}
	if req.Channels != nil {
		existing.Channels = req.Channels
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.Engine.UpdateRule(c.Request.Context(), *existing); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, ruleToResp(*existing))
}

// DeleteRule DELETE /api/v1/anomaly/rules/:id
func (h *AnomalyHandler) DeleteRule(c *gin.Context) {
	ruleID := c.Param("id")
	if err := h.Engine.RemoveRule(c.Request.Context(), ruleID); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}
	web.OK(c, gin.H{"deleted": ruleID})
}

// ListEvents GET /api/v1/anomaly/events
func (h *AnomalyHandler) ListEvents(c *gin.Context) {
	tenantID := c.Query("tenantId")
	ruleID := c.Query("ruleId")
	limit := intQuery(c, "limit", 50)

	var events []anomaly.Event
	var err error

	if ruleID != "" {
		events, err = h.Engine.GetEvents(c.Request.Context(), ruleID, limit)
	} else {
		events, err = h.Engine.ListEvents(c.Request.Context(), tenantID, limit)
	}

	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	resp := make([]EventResp, 0, len(events))
	for _, evt := range events {
		resp = append(resp, EventResp{
			ID:          evt.ID,
			RuleID:      evt.RuleID,
			TenantID:    evt.TenantID,
			TriggeredAt: evt.TriggeredAt.Format(time.RFC3339),
			MetricValue: evt.MetricValue,
			Severity:    string(evt.Severity),
			Message:     evt.Message,
			Notified:    evt.Notified,
		})
	}
	web.OK(c, resp)
}

// ========== 辅助函数 ==========

func ruleToResp(r anomaly.Rule) RuleResp {
	channels := r.Channels
	if channels == nil {
		channels = []string{}
	}
	return RuleResp{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Name:        r.Name,
		Description: r.Description,
		RuleType:    string(r.RuleType),
		Metric:      r.Metric,
		Threshold:   r.Threshold,
		WindowSizeMs: int(r.WindowSize.Milliseconds()),
		Severity:    string(r.Severity),
		Enabled:     r.Enabled,
		Channels:    channels,
	}
}

