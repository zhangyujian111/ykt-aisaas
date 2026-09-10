package face

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Service 人脸检测服务（v1：detect + 5 点 keypoints，不做识别）
type Service interface {
	// Detect 实时人脸检测（无状态）
	Detect(ctx context.Context, req DetectReq) (*DetectResp, error)
}

// DetectReq 检测请求
type DetectReq struct {
	DeviceID  string
	ImageData []byte // JPEG raw
	FrameCRC  uint32 // 来自协议 §5.2 camera_frame 前 4 字节，用于审计关联
	TsMs      int64
}

// DetectResp 检测响应
type DetectResp struct {
	Detections []Detection `json:"detections"`
	LatencyMs  int         `json:"latencyMs"`
	ModelUsed  string      `json:"modelUsed"`
	Hit        bool        `json:"hit"` // 是否有检测到（detections 非空）
}

type faceService struct {
	detector Detector
	logger   *slog.Logger
}

// NewService 创建 face service（v1：仅检测）
func NewService(detector Detector, logger *slog.Logger) Service {
	return &faceService{
		detector: detector,
		logger:   logger,
	}
}

func (s *faceService) Detect(ctx context.Context, req DetectReq) (*DetectResp, error) {
	start := time.Now()

	detections, err := s.detector.Detect(ctx, req.ImageData)
	if err != nil {
		return nil, fmt.Errorf("face detection: %w", err)
	}

	resp := &DetectResp{
		Detections: detections,
		LatencyMs:  int(time.Since(start).Milliseconds()),
		ModelUsed:  s.detector.Name(),
		Hit:        len(detections) > 0,
	}

	s.logger.InfoContext(ctx, "face.detect",
		"deviceId", req.DeviceID,
		"hit", resp.Hit,
		"faces", len(detections),
		"latencyMs", resp.LatencyMs,
		"model", resp.ModelUsed,
	)

	return resp, nil
}
