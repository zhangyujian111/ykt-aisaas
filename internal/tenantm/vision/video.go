// Package vision provides multimodal image and video understanding capabilities.
//
// Supports multiple vision providers:
//   - OpenAI GPT-4 Vision
//   - Qwen-VL (Alibaba Cloud)
//   - GLM-4V (Zhipu AI)
package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

// VideoService 视频理解服务接口
type VideoService interface {
	// AnalyzeStream 流式视频分析：传入视频帧流（每 1s 一帧），返回 SSE 流式描述
	AnalyzeStream(ctx context.Context, req VideoStreamReq) (<-chan *VideoFrame, <-chan error, error)
	// AnalyzeFile 离线视频分析（上传视频文件）
	AnalyzeFile(ctx context.Context, req VideoFileAnalysisReq) (*VideoAnalysis, error)
}

// VideoStreamReq 流式视频分析请求
type VideoStreamReq struct {
	TenantID   string
	DeviceID   string
	VideoPath  string  // 视频文件路径（离线模式）
	Model      string  // 'qwen-vl-max' / 'gpt-4-vision-preview' / 'glm-4v-plus'
	SampleRate int     // 每多少秒取一帧（默认 1s）
	MaxFrames  int     // 最大帧数（默认 60 = 1分钟）
	Prompt     string   // 分析提示词
}

// VideoFileAnalysisReq 离线视频文件分析请求
type VideoFileAnalysisReq struct {
	TenantID      string
	DeviceID      string
	VideoPath     string   // 视频文件路径
	Model         string   // 模型 ID
	SampleRate    int      // 抽帧间隔（秒）
	MaxFrames     int      // 最大帧数
	MaxTokens     int      // 最大生成 token 数
	Prompt        string   // 分析提示词
	ExtractMethod string   // 'keyframes' | 'interval' 抽帧方式
}

// VideoFrame 视频帧分析结果
type VideoFrame struct {
	Timestamp   time.Duration `json:"timestamp"`    // 视频时间戳
	FrameNumber int           `json:"frameNumber"`  // 帧序号
	Description string        `json:"description"`  // 帧描述
	Tags        []string      `json:"tags"`         // 标签列表
	IsKeyframe  bool         `json:"isKeyframe"`   // 关键帧（场景变化）
	LatencyMs   int          `json:"latencyMs"`    // 处理延迟（毫秒）
}

// VideoAnalysis 视频分析结果
type VideoAnalysis struct {
	Frames      []*VideoFrame `json:"frames"`       // 所有帧分析结果
	Summary     string        `json:"summary"`      // 视频摘要
	TotalFrames int           `json:"totalFrames"`  // 处理帧数
	Duration    time.Duration `json:"duration"`     // 视频时长
	LatencyMs   int           `json:"latencyMs"`    // 总处理延迟
}

// videoService 视频服务实现
type videoService struct {
	providers map[string]Provider
	extractor FrameExtractor
	defaultModel string
}

// NewVideoService 创建视频服务
func NewVideoService(providers map[string]Provider, extractor FrameExtractor, defaultModel string) VideoService {
	return &videoService{
		providers:    providers,
		extractor:    extractor,
		defaultModel: defaultModel,
	}
}

// AnalyzeStream 流式视频分析
func (s *videoService) AnalyzeStream(ctx context.Context, req VideoStreamReq) (<-chan *VideoFrame, <-chan error, error) {
	if req.Model == "" {
		req.Model = s.defaultModel
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}

	provider, ok := s.providers[req.Model]
	if !ok {
		return nil, nil, fmt.Errorf("vision provider %q not found", req.Model)
	}

	frameCh := make(chan *VideoFrame, req.MaxFrames)
	errCh := make(chan error, 1)

	go func() {
		defer close(frameCh)
		defer close(errCh)

		// 提取视频帧
		framePaths, err := s.extractor.ExtractByInterval(ctx, req.VideoPath, req.SampleRate)
		if err != nil {
			errCh <- fmt.Errorf("extract frames failed: %w", err)
			return
		}

		// 限制最大帧数
		if len(framePaths) > req.MaxFrames {
			framePaths = framePaths[:req.MaxFrames]
		}

		// 逐帧分析
		for frameNumber, framePath := range framePaths {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// 读取帧文件并转为 base64
			imgData, err := ioutil.ReadFile(framePath)
			if err != nil {
				errCh <- fmt.Errorf("read frame %d failed: %w", frameNumber, err)
				continue
			}
			base64Img := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imgData)

			// 调用 Provider 分析
			start := time.Now()
			resp, err := provider.Analyze(ctx, AnalyzeReq{
				TenantID:  req.TenantID,
				DeviceID:  req.DeviceID,
				ImageURL:  base64Img,
				Prompt:    req.Prompt,
				Model:     req.Model,
				MaxTokens: 1024,
			})
			if err != nil {
				errCh <- fmt.Errorf("analyze frame %d failed: %w", frameNumber, err)
				continue
			}

			// 推送结果
			frameCh <- &VideoFrame{
				Timestamp:   time.Duration(frameNumber*req.SampleRate) * time.Second,
				FrameNumber: frameNumber + 1,
				Description: resp.Description,
				Tags:        resp.Tags,
				IsKeyframe:  true,
				LatencyMs:   int(time.Since(start).Milliseconds()),
			}

			// 清理临时帧文件
			os.Remove(framePath)
		}
	}()

	return frameCh, errCh, nil
}

// AnalyzeFile 离线视频文件分析
func (s *videoService) AnalyzeFile(ctx context.Context, req VideoFileAnalysisReq) (*VideoAnalysis, error) {
	if req.Model == "" {
		req.Model = s.defaultModel
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

	provider, ok := s.providers[req.Model]
	if !ok {
		return nil, fmt.Errorf("vision provider %q not found", req.Model)
	}

	var framePaths []string
	var err error

	// 提取视频帧
	if req.ExtractMethod == "keyframes" {
		framePaths, err = s.extractor.ExtractKeyframes(ctx, req.VideoPath, "")
	} else {
		framePaths, err = s.extractor.ExtractByInterval(ctx, req.VideoPath, req.SampleRate)
	}
	if err != nil {
		return nil, fmt.Errorf("extract frames failed: %w", err)
	}

	// 限制最大帧数
	if len(framePaths) > req.MaxFrames {
		framePaths = framePaths[:req.MaxFrames]
	}

	// 分析每一帧
	var frames []*VideoFrame
	start := time.Now()
	for i, framePath := range framePaths {
		// 构建图片 URL
		imageURL := framePath
		if !strings.HasPrefix(framePath, "http://") && !strings.HasPrefix(framePath, "https://") && !strings.HasPrefix(framePath, "data:") {
			imageURL = "file://" + framePath
		}

		resp, err := provider.Analyze(ctx, AnalyzeReq{
			TenantID:  req.TenantID,
			DeviceID:  req.DeviceID,
			ImageURL:  imageURL,
			Prompt:    req.Prompt,
			MaxTokens: req.MaxTokens,
		})
		if err != nil {
			// 单帧失败不影响整流，记录错误继续
			continue
		}

		frames = append(frames, &VideoFrame{
			Timestamp:   time.Duration(i*req.SampleRate) * time.Second,
			FrameNumber: i + 1,
			Description: resp.Description,
			Tags:        resp.Tags,
			IsKeyframe:  false,
			LatencyMs:   resp.LatencyMs,
		})
	}

	return &VideoAnalysis{
		Frames:      frames,
		TotalFrames: len(frames),
		LatencyMs:   int(time.Since(start).Milliseconds()),
	}, nil
}
