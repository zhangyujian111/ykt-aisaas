// Package apiv1 SaaS 自有接口（/api/v1/*）。
package apiv1

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/vision"
)

// VisionHandler /api/v1/vision
type VisionHandler struct {
	Svc   vision.Service
	Quota *quota.Guard
	Meter *metering.Recorder
}

// AnalyzeReq 视觉理解请求
type AnalyzeReq struct {
	DeviceID  string `json:"deviceId" binding:"required"`
	ImageURL  string `json:"imageUrl" binding:"required"`
	Prompt    string `json:"prompt" binding:"required"`
	Model     string `json:"model"` // 可选，默认使用配置的默认模型
	MaxTokens int    `json:"maxTokens"`
}

// Analyze POST /api/v1/vision/analyze
func (h *VisionHandler) Analyze(c *gin.Context) {
	var req AnalyzeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}

	// 检查配额（视觉调用使用 vision_calls 维度）
	if h.Quota != nil {
		// TODO: 实际实现配额检查
		// if err := h.Quota.Check(c.Request.Context(), req.TenantID, "vision_calls", 1); err != nil {
		// 	web.Abort(c, err)
		// 	return
		// }
	}

	// 调用视觉服务
	resp, err := h.Svc.Analyze(c.Request.Context(), vision.AnalyzeReq{
		TenantID:  "",
		DeviceID:  req.DeviceID,
		ImageURL:  req.ImageURL,
		Prompt:    req.Prompt,
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "vision analyze failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 记录计量（视觉调用计入 quota）
	if h.Meter != nil {
		h.Meter.Record(c.Request.Context(), metering.Record{
			TenantID:  0, // 从 context 获取
			BizType:   "vision",
			Dimension: "vision_calls",
			Amount:    1,
			ModelID:   req.Model,
		})
	}

	web.OK(c, resp)
}

// CompareReq 多图对比请求
type CompareReq struct {
	DeviceID  string   `json:"deviceId" binding:"required"`
	ImageURLs []string `json:"imageUrls" binding:"required,min=2"`
	Prompt    string   `json:"prompt" binding:"required"`
	Model     string   `json:"model"`
	MaxTokens int      `json:"maxTokens"`
}

// Compare POST /api/v1/vision/compare
func (h *VisionHandler) Compare(c *gin.Context) {
	var req CompareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}

	resp, err := h.Svc.Compare(c.Request.Context(), vision.CompareReq{
		TenantID:  "",
		DeviceID:  req.DeviceID,
		ImageURLs: req.ImageURLs,
		Prompt:    req.Prompt,
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "vision compare failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 记录计量
	if h.Meter != nil {
		h.Meter.Record(c.Request.Context(), metering.Record{
			TenantID:  0,
			BizType:   "vision",
			Dimension: "vision_calls",
			Amount:    int64(len(req.ImageURLs)), // 多图对比按图片数计
			ModelID:   req.Model,
		})
	}

	web.OK(c, resp)
}

// VideoReq 视频分析请求
type VideoReq struct {
	DeviceID       string `json:"deviceId" binding:"required"`
	VideoURL       string `json:"videoUrl" binding:"required"`
	FrameInterval  int    `json:"frameInterval"` // 抽帧间隔（秒）
	Prompt         string `json:"prompt" binding:"required"`
	Model          string `json:"model"`
	MaxTokens      int    `json:"maxTokens"`
}

// AnalyzeVideo POST /api/v1/vision/video
func (h *VisionHandler) AnalyzeVideo(c *gin.Context) {
	var req VideoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, err)
		return
	}

	if req.FrameInterval <= 0 {
		req.FrameInterval = 5 // 默认 5 秒
	}

	resp, err := h.Svc.AnalyzeVideo(c.Request.Context(), vision.VideoReq{
		TenantID:       "",
		DeviceID:       req.DeviceID,
		VideoURL:       req.VideoURL,
		FrameInterval:  req.FrameInterval,
		Prompt:         req.Prompt,
		Model:          req.Model,
		MaxTokens:      req.MaxTokens,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "vision video analyze failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 记录计量
	if h.Meter != nil {
		h.Meter.Record(c.Request.Context(), metering.Record{
			TenantID:  0,
			BizType:   "vision",
			Dimension: "vision_calls",
			Amount:    1,
			ModelID:   req.Model,
		})
	}

	web.OK(c, resp)
}
