// Package vision provides multimodal image understanding capabilities.
//
// Supports multiple vision providers:
//   - OpenAI GPT-4 Vision
//   - Qwen-VL (Alibaba Cloud)
//   - GLM-4V (Zhipu AI)
package vision

import (
	"context"
	"fmt"
)

// MaxImageSize 默认最大图片大小（20MB）
const MaxImageSize = 20 * 1024 * 1024

// Service 视觉理解服务接口
type Service interface {
	// Analyze 单图理解
	Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error)
	// Compare 多图对比
	Compare(ctx context.Context, req CompareReq) (*CompareResp, error)
	// AnalyzeVideo 视频帧分析
	AnalyzeVideo(ctx context.Context, req VideoReq) (*AnalyzeResp, error)
}

// AnalyzeReq 单图理解请求
type AnalyzeReq struct {
	TenantID  string
	DeviceID  string
	ImageURL  string  // 图片 URL 或 Base64 (data:image/jpeg;base64,xxx)
	Prompt    string  // 用户提示词
	Model     string  // 模型 ID（如 "openai-gpt4v"）
	MaxTokens int     // 最大生成 token 数
}

// AnalyzeResp 单图理解响应
type AnalyzeResp struct {
	Description string          `json:"description"` // 图片描述
	Tags        []string        `json:"tags"`        // 标签列表
	Objects     []DetectedObject `json:"objects"`     // 检测到的物体
	Text        string          `json:"text"`        // OCR 结果（图片中的文字）
	Confidence  float64         `json:"confidence"`  // 置信度
	LatencyMs   int             `json:"latencyMs"`   // 处理延迟（毫秒）
}

// DetectedObject 检测到的物体
type DetectedObject struct {
	Label      string  `json:"label"`       // 物体标签
	Confidence float64 `json:"confidence"`  // 置信度
	BoundingBox [4]int `json:"boundingBox"` // 边界框 [x, y, w, h]
}

// CompareReq 多图对比请求
type CompareReq struct {
	TenantID  string
	DeviceID  string
	ImageURLs []string // 多个图片 URL 或 Base64
	Prompt    string   // 对比提示词
	Model     string   // 模型 ID
	MaxTokens int
}

// CompareResp 多图对比响应
type CompareResp struct {
	Analysis  string          `json:"analysis"`  // 对比分析结果
	Similarity float64        `json:"similarity"` // 相似度（0-1）
	Differences []string      `json:"differences"` // 差异列表
	LatencyMs   int           `json:"latencyMs"`
}

// VideoReq 视频帧分析请求
type VideoReq struct {
	TenantID   string
	DeviceID   string
	VideoURL   string   // 视频 URL
	FrameInterval int   // 抽帧间隔（秒），默认 5s
	Prompt     string   // 分析提示词
	Model      string   // 模型 ID
	MaxTokens  int
}

// Provider 视觉 provider 接口
type Provider interface {
	Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error)
	Name() string
}

// visionService 视觉服务实现
type visionService struct {
	providers map[string]Provider
	defaultModel string
}

// NewService 创建视觉服务
func NewService(providers map[string]Provider, defaultModel string) Service {
	return &visionService{
		providers:    providers,
		defaultModel: defaultModel,
	}
}

// Analyze 单图理解
func (s *visionService) Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error) {
	if req.Model == "" {
		req.Model = s.defaultModel
	}

	provider, ok := s.providers[req.Model]
	if !ok {
		return nil, fmt.Errorf("vision provider %q not found", req.Model)
	}

	return provider.Analyze(ctx, req)
}

// Compare 多图对比
func (s *visionService) Compare(ctx context.Context, req CompareReq) (*CompareResp, error) {
	if len(req.ImageURLs) < 2 {
		return nil, fmt.Errorf("compare requires at least 2 images, got %d", len(req.ImageURLs))
	}

	if req.Model == "" {
		req.Model = s.defaultModel
	}

	provider, ok := s.providers[req.Model]
	if !ok {
		return nil, fmt.Errorf("vision provider %q not found", req.Model)
	}

	// 多图对比通过多次 Analyze 实现
	// 构建一个包含所有图片的提示词
	var results []*AnalyzeResp
	for _, imgURL := range req.ImageURLs {
		resp, err := provider.Analyze(ctx, AnalyzeReq{
			TenantID:  req.TenantID,
			DeviceID:  req.DeviceID,
			ImageURL:  imgURL,
			Prompt:    req.Prompt,
			MaxTokens: req.MaxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("analyze image failed: %w", err)
		}
		results = append(results, resp)
	}

	// 计算相似度和差异
	similarity := s.calculateSimilarity(results)
	differences := s.extractDifferences(results)

	return &CompareResp{
		Analysis:   results[0].Description,
		Similarity: similarity,
		Differences: differences,
		LatencyMs:  results[0].LatencyMs,
	}, nil
}

// AnalyzeVideo 视频帧分析
func (s *visionService) AnalyzeVideo(ctx context.Context, req VideoReq) (*AnalyzeResp, error) {
	if req.Model == "" {
		req.Model = s.defaultModel
	}

	provider, ok := s.providers[req.Model]
	if !ok {
		return nil, fmt.Errorf("vision provider %q not found", req.Model)
	}

	// 视频帧分析：这里只是简单实现，实际应该抽帧
	// P3 阶段会实现真正的视频处理
	resp, err := provider.Analyze(ctx, AnalyzeReq{
		TenantID:  req.TenantID,
		DeviceID:  req.DeviceID,
		ImageURL:  req.VideoURL,
		Prompt:    req.Prompt,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return nil, err
	}

	// 添加视频相关标签
	resp.Tags = append(resp.Tags, "video_analysis")
	return resp, nil
}

// calculateSimilarity 计算图片相似度（简单实现）
func (s *visionService) calculateSimilarity(results []*AnalyzeResp) float64 {
	if len(results) < 2 {
		return 1.0
	}

	// 通过标签重叠度计算相似度
	tagSets := make([]map[string]bool, len(results))
	for i, r := range results {
		tagSets[i] = make(map[string]bool)
		for _, tag := range r.Tags {
			tagSets[i][tag] = true
		}
	}

	// 计算交集
	intersection := 0
	union := 0
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			for tag := range tagSets[i] {
				if tagSets[j][tag] {
					intersection++
				}
				union++
			}
			for tag := range tagSets[j] {
				if !tagSets[i][tag] {
					union++
				}
			}
		}
	}

	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// extractDifferences 提取图片差异
func (s *visionService) extractDifferences(results []*AnalyzeResp) []string {
	if len(results) < 2 {
		return nil
	}

	var differences []string
	tagSets := make([]map[string]bool, len(results))
	for i, r := range results {
		tagSets[i] = make(map[string]bool)
		for _, tag := range r.Tags {
			tagSets[i][tag] = true
		}
	}

	// 找出只在部分图片中存在的标签
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			for tag := range tagSets[i] {
				if !tagSets[j][tag] {
					differences = append(differences, fmt.Sprintf("Image %d has '%s' but Image %d does not", i+1, tag, j+1))
				}
			}
		}
	}

	return differences
}
