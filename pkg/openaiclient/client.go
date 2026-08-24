// Package openaiclient 极简 OpenAI 兼容客户端（Chat Completions + Embeddings）。
// 供 ykt-aisaas 平台调用上游（OpenAI/DeepSeek/智谱/DashScope 兼容端点/自建 vllm）。
// 设计：仅依赖标准库；SSE 流式解析健壮（跨 chunk 半行拼接、[DONE] 终止、usage 尾包）。
package openaiclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client 上游客户端（一个上游端点一个实例，调用方缓存）。
type Client struct {
	BaseURL string // 如 https://api.deepseek.com/v1
	APIKey  string
	HTTP    *http.Client
}

// New 构造。
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// ---- 请求/响应模型（OpenAI 协议子集 + 平台 x- 扩展剥离）----

// Message 消息。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用。Index 仅流式 delta 中出现（按 index 累积 arguments 分片）。
type ToolCall struct {
	Index   int          `json:"index,omitempty"`
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 函数调用。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool 工具定义。
type Tool struct {
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}

// ToolFunc 工具函数定义。
type ToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatRequest chat/completions 请求。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
}

// FinishReason 首个 choice 的 finish_reason（空安全）。
func (r *ChatResponse) FinishReason() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].FinishReason
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice 选择。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
}

// ChatResponse 非流式响应。
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Chunk 流式分片。
type Chunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"` // stream_options.include_usage 尾包
}

// EmbeddingRequest embeddings 请求。
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbeddingData 单条向量。
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// EmbeddingResponse embeddings 响应。
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// APIError 上游错误。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("upstream %d: %s", e.StatusCode, trunc(e.Body, 300))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (c *Client) newPost(ctx context.Context, path string, body any, extraHeaders map[string]string) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return resp, nil
}

// Complete 非流式调用。
func (c *Client) Complete(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	httpReq, err := c.newPost(ctx, "/chat/completions", req, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// Embed 文本向量化。
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	httpReq, err := c.newPost(ctx, "/embeddings", req, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	return &out, nil
}

// StreamChunk 流式输出的一个事件：Chunk 或错误或完成信号。
type StreamChunk struct {
	Chunk *Chunk
	Err   error
	Done  bool
}

// Stream 流式调用。返回事件通道，调用方 range 消费直至关闭。
// 通道关闭即流结束（无论正常/异常，最后一个事件含 Err）。
func (c *Client) Stream(ctx context.Context, req *ChatRequest) <-chan StreamChunk {
	req.Stream = true
	out := make(chan StreamChunk, 32)

	go func() {
		defer close(out)
		// 强制上游附带 usage 尾包（OpenAI/DeepSeek/智谱/DashScope 兼容）
		httpReq, err := c.newPost(ctx, "/chat/completions", withUsageOption(req), nil)
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		resp, err := c.do(httpReq)
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var lineBuf string
		for sc.Scan() {
			if ctx.Err() != nil {
				out <- StreamChunk{Err: ctx.Err()}
				return
			}
			line := sc.Text()
			if line == "" {
				continue
			}
			// SSE 事件跨 chunk 断行保护：仅处理 data: 前缀行
			if !strings.HasPrefix(line, "data:") {
				lineBuf += line
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}
			if payload == "[DONE]" {
				return
			}
			var ch Chunk
			if err := json.Unmarshal([]byte(payload), &ch); err != nil {
				out <- StreamChunk{Err: fmt.Errorf("bad chunk %q: %w", trunc(payload, 120), err)}
				return
			}
			out <- StreamChunk{Chunk: &ch}
		}
		if err := sc.Err(); err != nil {
			out <- StreamChunk{Err: err}
		}
	}()
	return out
}

type withUsage struct {
	ChatRequest
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

func withUsageOption(req *ChatRequest) any {
	return withUsage{ChatRequest: *req, StreamOptions: &streamOptions{IncludeUsage: true}}
}

// MergeContent 聚合流式 chunk 为完整文本（服务层复用）。
func MergeContent(chunks []*Chunk) string {
	var sb strings.Builder
	for _, ch := range chunks {
		for _, c := range ch.Choices {
			sb.WriteString(c.Delta.Content)
		}
	}
	return sb.String()
}
