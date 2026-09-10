package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider OpenAI GPT-4 Vision provider
type OpenAIProvider struct {
	apiKey      string
	apiBase     string
	modelName   string
	maxImageSize int
	maxTokens   int
	httpClient  *http.Client
}

// OpenAIConfig OpenAI provider configuration
type OpenAIConfig struct {
	APIKey       string
	APIBase      string // 默认 https://api.openai.com/v1
	ModelName    string // 默认 gpt-4-vision-preview
	MaxImageSize int    // 默认 20MB
	MaxTokens    int    // 默认 4096
}

// NewOpenAIProvider 创建 OpenAI vision provider
func NewOpenAIProvider(cfg OpenAIConfig) Provider {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.openai.com/v1"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "gpt-4-vision-preview"
	}
	if cfg.MaxImageSize == 0 {
		cfg.MaxImageSize = 20 * 1024 * 1024
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}

	return &OpenAIProvider{
		apiKey:       cfg.APIKey,
		apiBase:      cfg.APIBase,
		modelName:    cfg.ModelName,
		maxImageSize: cfg.MaxImageSize,
		maxTokens:    cfg.MaxTokens,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name 返回 provider 名称
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// openAIRequest OpenAI vision API request
type openAIRequest struct {
	Model    string               `json:"model"`
	Messages []openAIMessage      `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

// openAIMessage OpenAI message
type openAIMessage struct {
	Role    string      `json:"role"`
	Content []openAIContent `json:"content"`
}

// openAIContent OpenAI content item
type openAIContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

// openAIImageURL OpenAI image URL
type openAIImageURL struct {
	URL string `json:"url"`
}

// openAIResponse OpenAI vision API response
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message       openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Analyze 单图理解
func (p *OpenAIProvider) Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error) {
	start := time.Now()

	// 构建图片 URL（支持 URL 和 Base64）
	imageURL := req.ImageURL
	if strings.HasPrefix(req.ImageURL, "data:") {
		// Base64 图片直接使用
		imageURL = req.ImageURL
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}

	// 构建请求
	openaiReq := openAIRequest{
		Model: p.modelName,
		Messages: []openAIMessage{
			{
				Role: "user",
				Content: []openAIContent{
					{
						Type: "text",
						Text: req.Prompt,
					},
					{
						Type: "image_url",
						ImageURL: &openAIImageURL{
							URL: imageURL,
						},
					},
				},
			},
		},
		MaxTokens: maxTokens,
	}

	body, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var openaiResp openAIResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := openaiResp.Choices[0].Message.Content
	textContent := extractTextContent(content)
	tags := extractTags(textContent)
	objects := extractObjects(textContent)
	text := extractOCRText(textContent)

	latencyMs := int(time.Since(start).Milliseconds())

	return &AnalyzeResp{
		Description: textContent,
		Tags:        tags,
		Objects:     objects,
		Text:        text,
		Confidence:  0.9, // OpenAI 不返回置信度，使用默认值
		LatencyMs:   latencyMs,
	}, nil
}

// extractTextContent 从 content 切片中提取文本内容
func extractTextContent(content []openAIContent) string {
	for _, c := range content {
		if c.Type == "text" {
			return c.Text
		}
	}
	return ""
}

// extractTags 从描述中提取标签（简单实现）
func extractTags(content string) []string {
	var tags []string
	// 简单关键词提取
	keywords := []string{"person", "dog", "cat", "car", "building", "tree", "sky", "indoor", "outdoor", "text", "face"}
	contentLower := strings.ToLower(content)
	for _, kw := range keywords {
		if strings.Contains(contentLower, kw) {
			tags = append(tags, kw)
		}
	}
	return tags
}

// extractObjects 从描述中提取物体（简单实现）
func extractObjects(content string) []DetectedObject {
	// 实际生产中应该用更好的解析逻辑
	return nil
}

// extractOCRText 从描述中提取 OCR 文本（简单实现）
func extractOCRText(content string) string {
	// 检查是否包含"图中文字"或"文字"
	if strings.Contains(content, "text") && strings.Contains(content, "reads") {
		// 简单返回
		return content
	}
	return ""
}

// ============================================================================
// QwenVLProvider 阿里云通义千问 VL Provider
// ============================================================================

// QwenVLProvider Qwen-VL provider
type QwenVLProvider struct {
	apiKey      string
	apiBase     string
	modelName   string
	maxImageSize int
	maxTokens   int
	httpClient  *http.Client
}

// QwenVLConfig Qwen-VL provider configuration
type QwenVLConfig struct {
	APIKey       string
	APIBase      string // 默认 https://dashscope.aliyuncs.com/api/v1
	ModelName    string // 默认 qwen-vl-max
	MaxImageSize int
	MaxTokens    int
}

// NewQwenVLProvider 创建 Qwen-VL provider
func NewQwenVLProvider(cfg QwenVLConfig) Provider {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://dashscope.aliyuncs.com/api/v1"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "qwen-vl-max"
	}
	if cfg.MaxImageSize == 0 {
		cfg.MaxImageSize = 20 * 1024 * 1024
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 2048
	}

	return &QwenVLProvider{
		apiKey:       cfg.APIKey,
		apiBase:      cfg.APIBase,
		modelName:    cfg.ModelName,
		maxImageSize: cfg.MaxImageSize,
		maxTokens:    cfg.MaxTokens,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name 返回 provider 名称
func (p *QwenVLProvider) Name() string {
	return "qwen-vl"
}

// qwenVLRequest Qwen-VL API request
type qwenVLRequest struct {
	Model string        `json:"model"`
	Input qwenVLInput   `json:"input"`
	Parameters qwenVLParams `json:"parameters,omitempty"`
}

// qwenVLInput Qwen-VL input
type qwenVLInput struct {
	Messages []qwenVLMessage `json:"messages"`
}

// qwenVLMessage Qwen-VL message
type qwenVLMessage struct {
	Role    string          `json:"role"`
	Content qwenVLContent   `json:"content"`
}

// qwenVLContent Qwen-VL content (supports both text and image)
type qwenVLContent struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

// qwenVLParams Qwen-VL parameters
type qwenVLParams struct {
	MaxTokens int `json:"max_tokens,omitempty"`
}

// qwenVLResponse Qwen-VL API response
type qwenVLResponse struct {
	Output struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}

// Analyze 单图理解
func (p *QwenVLProvider) Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error) {
	start := time.Now()

	// 处理 Base64 图片
	imageURL := req.ImageURL
	if strings.HasPrefix(req.ImageURL, "data:") {
		// Qwen-VL 支持 Base64，直接传递
		imageURL = req.ImageURL
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}

	// 构建请求
	qwenReq := qwenVLRequest{
		Model: p.modelName,
		Input: qwenVLInput{
			Messages: []qwenVLMessage{
				{
					Role: "user",
					Content: qwenVLContent{
						Text:  req.Prompt,
						Image: imageURL,
					},
				},
			},
		},
		Parameters: qwenVLParams{
			MaxTokens: maxTokens,
		},
	}

	body, err := json.Marshal(qwenReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/services/aigc/multimodal-generation/generation", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var qwenResp qwenVLResponse
	if err := json.Unmarshal(respBody, &qwenResp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	if len(qwenResp.Output.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := qwenResp.Output.Choices[0].Message.Content
	tags := extractTags(content)
	objects := extractObjects(content)
	text := extractOCRText(content)

	latencyMs := int(time.Since(start).Milliseconds())

	return &AnalyzeResp{
		Description: content,
		Tags:        tags,
		Objects:     objects,
		Text:        text,
		Confidence:  0.9,
		LatencyMs:   latencyMs,
	}, nil
}

// ============================================================================
// GLM4VProvider 智谱 GLM-4V Provider
// ============================================================================

// GLM4VProvider 智谱 GLM-4V provider
type GLM4VProvider struct {
	apiKey      string
	apiBase     string
	modelName   string
	maxImageSize int
	maxTokens   int
	httpClient  *http.Client
}

// GLM4VConfig GLM-4V provider configuration
type GLM4VConfig struct {
	APIKey       string
	APIBase      string // 默认 https://open.bigmodel.cn/api/paas/v4
	ModelName    string // 默认 glm-4v-plus
	MaxImageSize int
	MaxTokens    int
}

// NewGLM4VProvider 创建 GLM-4V provider
func NewGLM4VProvider(cfg GLM4VConfig) Provider {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://open.bigmodel.cn/api/paas/v4"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "glm-4v-plus"
	}
	if cfg.MaxImageSize == 0 {
		cfg.MaxImageSize = 20 * 1024 * 1024
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 2048
	}

	return &GLM4VProvider{
		apiKey:       cfg.APIKey,
		apiBase:      cfg.APIBase,
		modelName:    cfg.ModelName,
		maxImageSize: cfg.MaxImageSize,
		maxTokens:    cfg.MaxTokens,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name 返回 provider 名称
func (p *GLM4VProvider) Name() string {
	return "glm-4v"
}

// glm4VRequest GLM-4V API request
type glm4VRequest struct {
	Model string          `json:"model"`
	Messages []glm4VMessage `json:"messages"`
}

// glm4VMessage GLM-4V message
type glm4VMessage struct {
	Role    string        `json:"role"`
	Content []glm4VContent `json:"content"`
}

// glm4VContent GLM-4V content
type glm4VContent struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	Text     string `json:"text,omitempty"`
}

// glm4VResponse GLM-4V API response
type glm4VResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Analyze 单图理解
func (p *GLM4VProvider) Analyze(ctx context.Context, req AnalyzeReq) (*AnalyzeResp, error) {
	start := time.Now()

	// 处理 Base64 图片
	imageURL := req.ImageURL
	if strings.HasPrefix(req.ImageURL, "data:") {
		// GLM-4V 需要将 Base64 转为 URL 格式
		imageURL = req.ImageURL
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}

	// 构建请求
	glmReq := glm4VRequest{
		Model: p.modelName,
		Messages: []glm4VMessage{
			{
				Role: "user",
				Content: []glm4VContent{
					{
						Type:     "image_url",
						ImageURL: imageURL,
					},
					{
						Type: "text",
						Text: req.Prompt,
					},
				},
			},
		},
	}

	body, err := json.Marshal(glmReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var glmResp glm4VResponse
	if err := json.Unmarshal(respBody, &glmResp); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	if len(glmResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := glmResp.Choices[0].Message.Content
	tags := extractTags(content)
	objects := extractObjects(content)
	text := extractOCRText(content)

	latencyMs := int(time.Since(start).Milliseconds())

	return &AnalyzeResp{
		Description: content,
		Tags:        tags,
		Objects:     objects,
		Text:        text,
		Confidence:  0.9,
		LatencyMs:   latencyMs,
	}, nil
}

// ============================================================================
// Helper functions
// ============================================================================

// isBase64Image 判断是否是 Base64 编码的图片
func isBase64Image(s string) bool {
	return strings.HasPrefix(s, "data:image/")
}

// decodeBase64Image 解码 Base64 图片并返回 MIME 类型和数据
func decodeBase64Image(s string) (mimeType string, data []byte, err error) {
	if !strings.HasPrefix(s, "data:") {
		return "", nil, fmt.Errorf("not a base64 image")
	}

	// 解析 data:image/jpeg;base64,xxxxx 格式
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid base64 image format")
	}

	header := parts[0]
	dataStr := parts[1]

	// 提取 MIME 类型
	mimeType = strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")

	// 解码 Base64
	data, err = base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		return "", nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	return mimeType, data, nil
}
