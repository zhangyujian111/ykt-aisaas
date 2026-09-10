// Package apiv1 SaaS 自有接口（/api/v1/*）— abtest 模块 handler。
package apiv1

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/abtest"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/web"
)

// ABTestHandler /api/v1/abtest/* 接口 handler。
type ABTestHandler struct {
	Svc *abtest.ABTestService
}

// ========== 请求/响应 DTO ==========

// CreateExperimentReq 创建实验请求。
type CreateExperimentReq struct {
	Name              string              `json:"name" binding:"required"`
	Description       string              `json:"description"`
	Hypothesis        string              `json:"hypothesis"`
	PrimaryMetric     string              `json:"primaryMetric" binding:"required"`
	SecondaryMetrics  []string            `json:"secondaryMetrics"`
	MinSampleSize     int                 `json:"minSampleSize"`
	ExpectedEffectSize float64            `json:"expectedEffectSize"`
	StatisticalPower  float64             `json:"statisticalPower"`
	SignificanceLevel float64             `json:"significanceLevel"`
	Variants          []CreateVariantReq  `json:"variants" binding:"required,min=2"`
}

// CreateVariantReq 创建变体请求。
type CreateVariantReq struct {
	Name              string  `json:"name" binding:"required"`
	AllocationPercent float64 `json:"allocationPercent" binding:"required"`
	Config            any     `json:"config"`
	IsControl         bool    `json:"isControl"`
}

// ExperimentResp 实验响应。
type ExperimentResp struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Status            string         `json:"status"`
	Hypothesis        string         `json:"hypothesis"`
	PrimaryMetric     string         `json:"primaryMetric"`
	SecondaryMetrics  []string       `json:"secondaryMetrics"`
	MinSampleSize     int            `json:"minSampleSize"`
	ExpectedEffectSize float64       `json:"expectedEffectSize"`
	StatisticalPower  float64        `json:"statisticalPower"`
	SignificanceLevel float64        `json:"significanceLevel"`
	CreatedBy         int64          `json:"createdBy"`
	StartedAt         *string        `json:"startedAt"`
	EndedAt           *string        `json:"endedAt"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
	Variants          []VariantResp  `json:"variants,omitempty"`
}

// VariantResp 变体响应。
type VariantResp struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	AllocationPercent float64 `json:"allocationPercent"`
	Config            any     `json:"config"`
	IsControl         bool    `json:"isControl"`
}

// RecordEventReq 事件记录请求。
type RecordEventReq struct {
	ExperimentName string `json:"experimentName" binding:"required"`
	VariantName    string `json:"variantName" binding:"required"`
	UserID         string `json:"userId" binding:"required"`
	MetricName     string `json:"metricName" binding:"required"`
	MetricValue    float64 `json:"metricValue"`
	Properties     any    `json:"properties"`
}

// AnalysisResp 分析结果响应。
type AnalysisResp struct {
	ExperimentID      int64              `json:"experimentId"`
	ExperimentName    string             `json:"experimentName"`
	PrimaryMetric     string             `json:"primaryMetric"`
	TotalSampleSize   int                `json:"totalSampleSize"`
	SignificanceLevel float64            `json:"significanceLevel"`
	Results           []*abtest.StatisticalResult `json:"results"`
	Recommendation    string             `json:"recommendation"`
}

// ========== Handlers ==========

// CreateExperiment POST /api/v1/abtest/experiments
func (h *ABTestHandler) CreateExperiment(c *gin.Context) {
	var req CreateExperimentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	// 缺省值
	if req.MinSampleSize <= 0 {
		req.MinSampleSize = 10000
	}
	if req.StatisticalPower <= 0 {
		req.StatisticalPower = 0.80
	}
	if req.SignificanceLevel <= 0 {
		req.SignificanceLevel = 0.05
	}
	if req.SecondaryMetrics == nil {
		req.SecondaryMetrics = []string{}
	}

	exp := &abtest.Experiment{
		Name:              req.Name,
		Description:       req.Description,
		Hypothesis:        req.Hypothesis,
		PrimaryMetric:     req.PrimaryMetric,
		SecondaryMetrics:  req.SecondaryMetrics,
		MinSampleSize:     req.MinSampleSize,
		ExpectedEffectSize: req.ExpectedEffectSize,
		StatisticalPower:  req.StatisticalPower,
		SignificanceLevel: req.SignificanceLevel,
		Variants:          make([]*abtest.Variant, 0, len(req.Variants)),
	}

	for _, v := range req.Variants {
		cfgJSON := marshalConfig(v.Config)
		exp.Variants = append(exp.Variants, &abtest.Variant{
			Name:              v.Name,
			AllocationPercent: v.AllocationPercent,
			ConfigJSON:        cfgJSON,
			IsControl:         v.IsControl,
		})
	}

	result, err := h.Svc.CreateExperiment(c.Request.Context(), exp)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	c.JSON(201, web.Result{
		Code:      errs.OK,
		Message:   "success",
		RequestID: web.RequestID(c),
		Data:      experimentToResp(result),
	})
}

// StartExperiment POST /api/v1/abtest/experiments/:id/start
func (h *ABTestHandler) StartExperiment(c *gin.Context) {
	id, err := parseInt64(c.Param("id"))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid experiment id"))
		return
	}

	if err := h.Svc.StartExperiment(c.Request.Context(), id); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, gin.H{"status": "running", "id": id})
}

// PauseExperiment POST /api/v1/abtest/experiments/:id/pause
func (h *ABTestHandler) PauseExperiment(c *gin.Context) {
	id, err := parseInt64(c.Param("id"))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid experiment id"))
		return
	}

	if err := h.Svc.PauseExperiment(c.Request.Context(), id); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, gin.H{"status": "paused", "id": id})
}

// StopExperiment POST /api/v1/abtest/experiments/:id/stop
func (h *ABTestHandler) StopExperiment(c *gin.Context) {
	id, err := parseInt64(c.Param("id"))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid experiment id"))
		return
	}

	if err := h.Svc.StopExperiment(c.Request.Context(), id); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, gin.H{"status": "completed", "id": id})
}

// AssignVariant GET /api/v1/abtest/assign?experiment=name&user_id=xxx
func (h *ABTestHandler) AssignVariant(c *gin.Context) {
	experimentName := c.Query("experiment")
	userID := c.Query("user_id")
	tenantID := intQuery64(c, "tenant_id", 0)

	if experimentName == "" || userID == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "experiment and user_id are required"))
		return
	}

	variant, err := h.Svc.AssignVariant(c.Request.Context(), experimentName, userID, tenantID)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.ResourceNotFound, err))
		return
	}

	web.OK(c, VariantResp{
		ID:                variant.ID,
		Name:              variant.Name,
		AllocationPercent: variant.AllocationPercent,
		Config:            parseConfigJSON(variant.ConfigJSON),
		IsControl:         variant.IsControl,
	})
}

// RecordEvent POST /api/v1/abtest/events
func (h *ABTestHandler) RecordEvent(c *gin.Context) {
	var req RecordEventReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}

	// 从实验名解析 experiment_id 和 variant_id
	exp, err := h.Svc.GetExperimentByName(c.Request.Context(), req.ExperimentName)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.ResourceNotFound, err))
		return
	}

	// 查找变体
	var variantID int64
	for _, v := range exp.Variants {
		if v.Name == req.VariantName {
			variantID = v.ID
			break
		}
	}
	if variantID == 0 {
		web.Abort(c, errs.New(errs.InvalidParam, "variant not found: "+req.VariantName))
		return
	}

	propsJSON := marshalConfig(req.Properties)
	event := &abtest.Event{
		ExperimentID:   exp.ID,
		VariantID:      variantID,
		UserID:         req.UserID,
		MetricName:     req.MetricName,
		MetricValue:    req.MetricValue,
		PropertiesJSON: propsJSON,
	}

	if err := h.Svc.RecordEvent(c.Request.Context(), event); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	c.JSON(204, nil)
}

// GetAnalysis GET /api/v1/abtest/experiments/:id/analysis
func (h *ABTestHandler) GetAnalysis(c *gin.Context) {
	id, err := parseInt64(c.Param("id"))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid experiment id"))
		return
	}

	analysis, err := h.Svc.AnalyzeExperiment(c.Request.Context(), id)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	web.OK(c, analysis)
}

// ListExperiments GET /api/v1/abtest/experiments
func (h *ABTestHandler) ListExperiments(c *gin.Context) {
	status := c.Query("status")
	limit := intQuery(c, "limit", 20)

	experiments, err := h.Svc.ListExperiments(c.Request.Context(), status, limit)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}

	resp := make([]ExperimentResp, 0, len(experiments))
	for _, exp := range experiments {
		resp = append(resp, experimentToResp(exp))
	}
	web.OK(c, resp)
}

// GetExperiment GET /api/v1/abtest/experiments/:id
func (h *ABTestHandler) GetExperiment(c *gin.Context) {
	id, err := parseInt64(c.Param("id"))
	if err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, "invalid experiment id"))
		return
	}

	exp, err := h.Svc.GetExperiment(c.Request.Context(), id)
	if err != nil {
		web.Abort(c, errs.Wrap(errs.ResourceNotFound, err))
		return
	}

	web.OK(c, experimentToResp(exp))
}

// ========== 辅助函数 ==========

func experimentToResp(exp *abtest.Experiment) ExperimentResp {
	resp := ExperimentResp{
		ID:                exp.ID,
		Name:              exp.Name,
		Description:       exp.Description,
		Status:            string(exp.Status),
		Hypothesis:        exp.Hypothesis,
		PrimaryMetric:     exp.PrimaryMetric,
		SecondaryMetrics:  exp.SecondaryMetrics,
		MinSampleSize:     exp.MinSampleSize,
		ExpectedEffectSize: exp.ExpectedEffectSize,
		StatisticalPower:  exp.StatisticalPower,
		SignificanceLevel: exp.SignificanceLevel,
		CreatedBy:         exp.CreatedBy,
		CreatedAt:         exp.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         exp.UpdatedAt.Format(time.RFC3339),
		Variants:          make([]VariantResp, 0, len(exp.Variants)),
	}

	if exp.StartedAt != nil {
		s := exp.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &s
	}
	if exp.EndedAt != nil {
		s := exp.EndedAt.Format(time.RFC3339)
		resp.EndedAt = &s
	}

	for _, v := range exp.Variants {
		resp.Variants = append(resp.Variants, VariantResp{
			ID:                v.ID,
			Name:              v.Name,
			AllocationPercent: v.AllocationPercent,
			Config:            parseConfigJSON(v.ConfigJSON),
			IsControl:         v.IsControl,
		})
	}

	return resp
}

func intQuery(c *gin.Context, key string, defaultVal int) int {
	val := c.Query(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func intQuery64(c *gin.Context, key string, defaultVal int64) int64 {
	val := c.Query(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// marshalConfig 序列化配置为 JSON 字符串。
func marshalConfig(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseConfigJSON 解析 JSON 配置字符串。
func parseConfigJSON(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil
	}
	return v
}