// Package apiv1 SaaS 自有接口（/api/v1/*）。
package apiv1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tenantm/vision"
)

// VideoHandler /api/v1/vision/video
type VideoHandler struct {
	Svc       vision.VideoService
	FFmpegBin string // ffmpeg 可执行文件路径
	Quota     *quota.Guard
	Meter     *metering.Recorder
}

// VideoStreamReq 流式视频分析请求（GET /api/v1/vision/video/stream）
type VideoStreamReq struct {
	DeviceID   string `json:"deviceId" form:"deviceId" binding:"required"`
	Model      string `json:"model" form:"model"`                           // 模型 ID
	SampleRate int    `json:"sampleRate" form:"sampleRate"`                 // 抽帧间隔（秒）
	MaxFrames  int    `json:"maxFrames" form:"maxFrames"`                   // 最大帧数
	Prompt     string `json:"prompt" form:"prompt" binding:"required"`     // 分析提示词
}

// VideoFileReq 离线视频分析请求（POST /api/v1/vision/video/analyze）
type VideoFileReq struct {
	DeviceID      string `json:"deviceId" form:"deviceId" binding:"required"`
	Model         string `json:"model" form:"model"`                                  // 模型 ID
	SampleRate    int    `json:"sampleRate" form:"sampleRate"`                        // 抽帧间隔（秒）
	MaxFrames     int    `json:"maxFrames" form:"maxFrames"`                          // 最大帧数
	MaxTokens     int    `json:"maxTokens" form:"maxTokens"`                           // 最大 token 数
	Prompt        string `json:"prompt" form:"prompt" binding:"required"`               // 分析提示词
	ExtractMethod string `json:"extractMethod" form:"extractMethod"`                   // 'keyframes' | 'interval'
}

// HandleVideoStream GET /api/v1/vision/video/stream
//
// SSE 流式视频分析端点。设备端通过 SSE 接收实时视频帧分析结果。
//
// 请求参数（Query）:
//   - deviceId: 设备 ID
//   - model: 模型 ID（默认 qwen-vl-max）
//   - sampleRate: 抽帧间隔秒数（默认 1）
//   - maxFrames: 最大帧数（默认 60）
//   - prompt: 分析提示词
//
// 响应: SSE 流，每个事件包含 VideoFrame JSON
func (h *VideoHandler) HandleVideoStream(c *gin.Context) {
	var req VideoStreamReq
	if err := c.ShouldBindQuery(&req); err != nil {
		web.Abort(c, err)
		return
	}

	// 设置默认值
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}

	// 获取租户 ID（从 context 或 header）
	tenantID := getTenantID(c)

	// 启动流式分析
	frameCh, errCh, err := h.Svc.AnalyzeStream(c.Request.Context(), vision.VideoStreamReq{
		TenantID:   tenantID,
		DeviceID:   req.DeviceID,
		Model:      req.Model,
		SampleRate: req.SampleRate,
		MaxFrames:  req.MaxFrames,
		Prompt:     req.Prompt,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "video stream start failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 设置 SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 流式推送循环
	clientGone := c.Request.Context().Done()
	for {
		select {
		case frame, ok := <-frameCh:
			if !ok {
				// 流正常结束
				return
			}
			data, err := json.Marshal(frame)
			if err != nil {
				slog.Default().ErrorContext(c.Request.Context(), "marshal frame failed", "error", err)
				continue
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()

		case err := <-errCh:
			// 错误发生
			errData, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(c.Writer, "data: %s\n\n", errData)
			c.Writer.Flush()
			return

		case <-clientGone:
			// 客户端断开
			return
		}
	}
}

// HandleVideoAnalyze POST /api/v1/vision/video/analyze
//
// 离线视频文件分析端点。接收上传的视频文件，返回完整分析结果。
//
// multipart/form-data 参数:
//   - file: 视频文件
//   - deviceId: 设备 ID
//   - model: 模型 ID
//   - sampleRate: 抽帧间隔（秒，默认 1）
//   - maxFrames: 最大帧数（默认 60）
//   - maxTokens: 最大 token 数
//   - prompt: 分析提示词
//   - extractMethod: 抽帧方式（keyframes/interval，默认 interval）
func (h *VideoHandler) HandleVideoAnalyze(c *gin.Context) {
	// 接收文件上传
	file, err := c.FormFile("file")
	if err != nil {
		web.Abort(c, err)
		return
	}

	// 验证文件类型
	ext := filepath.Ext(file.Filename)
	if ext != ".mp4" && ext != ".avi" && ext != ".mov" && ext != ".mkv" && ext != ".flv" {
		web.Abort(c, fmt.Errorf("unsupported video format: %s", ext))
		return
	}

	// 限制文件大小（最大 100MB）
	if file.Size > 100*1024*1024 {
		web.Abort(c, fmt.Errorf("video file too large: %d bytes (max 100MB)", file.Size))
		return
	}

	// 保存上传文件到临时目录
	tempDir := filepath.Join(os.TempDir(), "aisaas_video")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		web.Abort(c, fmt.Errorf("create temp dir failed: %w", err))
		return
	}
	tempVideoPath := filepath.Join(tempDir, fmt.Sprintf("%d%s", time.Now().UnixNano(), ext))

	if err := c.SaveUploadedFile(file, tempVideoPath); err != nil {
		web.Abort(c, fmt.Errorf("save uploaded file failed: %w", err))
		return
	}
	defer os.Remove(tempVideoPath) // 清理临时文件

	// 解析其他参数
	var req VideoFileReq
	if err := c.ShouldBind(&req); err != nil {
		web.Abort(c, err)
		return
	}

	// 设置默认值
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}
	if req.ExtractMethod == "" {
		req.ExtractMethod = "interval"
	}

	// 获取租户 ID
	tenantID := getTenantID(c)

	// 执行视频分析
	result, err := h.Svc.AnalyzeFile(c.Request.Context(), vision.VideoFileAnalysisReq{
		TenantID:      tenantID,
		DeviceID:      req.DeviceID,
		VideoPath:     tempVideoPath,
		Model:         req.Model,
		SampleRate:    req.SampleRate,
		MaxFrames:     req.MaxFrames,
		MaxTokens:     req.MaxTokens,
		Prompt:        req.Prompt,
		ExtractMethod: req.ExtractMethod,
	})
	if err != nil {
		slog.Default().ErrorContext(c.Request.Context(), "video analyze failed", "error", err)
		web.Abort(c, err)
		return
	}

	// 记录计量
	if h.Meter != nil {
		h.Meter.Record(c.Request.Context(), metering.Record{
			TenantID:  0,
			BizType:   "vision_video",
			Dimension: "video_analyzes",
			Amount:    1,
			ModelID:   req.Model,
		})
	}

	web.OK(c, result)
}

// HandleVideoStreamUpload POST /api/v1/vision/video/stream/upload
//
// 流式视频分析文件上传模式。接收视频文件，转换为帧流进行 SSE 分析。
//
// multipart/form-data 参数:
//   - file: 视频文件
//   - deviceId: 设备 ID
//   - model: 模型 ID
//   - sampleRate: 抽帧间隔（秒，默认 1）
//   - maxFrames: 最大帧数（默认 60）
//   - prompt: 分析提示词
func (h *VideoHandler) HandleVideoStreamUpload(c *gin.Context) {
	// 接收文件上传
	file, err := c.FormFile("file")
	if err != nil {
		web.Abort(c, err)
		return
	}

	// 验证文件类型
	ext := filepath.Ext(file.Filename)
	if ext != ".mp4" && ext != ".avi" && ext != ".mov" && ext != ".mkv" && ext != ".flv" {
		web.Abort(c, fmt.Errorf("unsupported video format: %s", ext))
		return
	}

	// 限制文件大小
	if file.Size > 100*1024*1024 {
		web.Abort(c, fmt.Errorf("video file too large: %d bytes (max 100MB)", file.Size))
		return
	}

	// 保存上传文件
	tempDir := filepath.Join(os.TempDir(), "aisaas_video")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		web.Abort(c, fmt.Errorf("create temp dir failed: %w", err))
		return
	}
	tempVideoPath := filepath.Join(tempDir, fmt.Sprintf("%d%s", time.Now().UnixNano(), ext))

	if err := c.SaveUploadedFile(file, tempVideoPath); err != nil {
		web.Abort(c, fmt.Errorf("save uploaded file failed: %w", err))
		return
	}
	defer os.Remove(tempVideoPath)

	// 解析参数
	var req VideoStreamReq
	if err := c.ShouldBind(&req); err != nil {
		web.Abort(c, err)
		return
	}

	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}

	tenantID := getTenantID(c)

	// 创建临时 context 用于整个视频分析过程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frameCh, errCh, err := h.Svc.AnalyzeStream(ctx, vision.VideoStreamReq{
		TenantID:   tenantID,
		DeviceID:   req.DeviceID,
		Model:      req.Model,
		SampleRate: req.SampleRate,
		MaxFrames:  req.MaxFrames,
		Prompt:     req.Prompt,
	})
	if err != nil {
		web.Abort(c, err)
		return
	}

	// 设置 SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 流式推送
	clientGone := c.Request.Context().Done()
	for {
		select {
		case frame, ok := <-frameCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(frame)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()

		case err := <-errCh:
			errData, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintf(c.Writer, "data: %s\n\n", errData)
			c.Writer.Flush()
			return

		case <-clientGone:
			return
		}
	}
}

// getTenantID 从 context 或 header 获取租户 ID
func getTenantID(c *gin.Context) string {
	// 优先从 context 获取
	if tenantID, exists := c.Get("tenantID"); exists {
		if id, ok := tenantID.(string); ok {
			return id
		}
	}
	// fallback 到 header
	return c.GetHeader("X-Tenant-ID")
}

// writeSSEEvent 写入 SSE 事件
func writeSSEEvent(c *gin.Context, eventType string, data interface{}) {
	c.Writer.Write([]byte(fmt.Sprintf("event: %s\n", eventType)))
	c.Writer.Write([]byte("data: "))
	encoder := json.NewEncoder(c.Writer)
	encoder.Encode(data)
	c.Writer.Write([]byte("\n"))
	c.Writer.Flush()
}
