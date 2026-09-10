package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// LLMExtractor LLM 实体抽取器。
type LLMExtractor struct {
	Registry *llm.Registry
}

// NewLLMExtractor 构造。
func NewLLMExtractor(registry *llm.Registry) *LLMExtractor {
	return &LLMExtractor{Registry: registry}
}

// ExtractEntities 调用 LLM 做实体抽取。
func (e *LLMExtractor) ExtractEntities(ctx context.Context, messages []MessageItem, extractTypes []string) (*ExtractResult, error) {
	prompt := buildExtractPrompt(messages, extractTypes)

	// 解析默认 chat 模型
	resolved, err := e.Registry.Resolve(ctx, e.Registry.DefaultModelID(ctx))
	if err != nil {
		return nil, fmt.Errorf("resolve model for extract: %w", err)
	}

	req := &openaiclient.ChatRequest{
		Model: resolved.UpstreamModel,
		Messages: []openaiclient.Message{
			{Role: "system", Content: "你是一个实体抽取助手。严格按 JSON 格式输出结果，不要添加任何额外说明。"},
			{Role: "user", Content: prompt},
		},
		Temperature: pfloat64(0.1),
	}

	resp, err := resolved.Client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm extract call: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm extract: empty response")
	}

	content := resp.Choices[0].Message.Content
	content = stripJSONMarkdown(content)

	var result ExtractResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		slog.Warn("llm extract parse failed", "content", content[:min(len(content), 200)], "err", err)
		return nil, fmt.Errorf("parse extract result: %w", err)
	}

	return &result, nil
}

// Summarize 调用 LLM 做摘要。
func (e *LLMExtractor) Summarize(ctx context.Context, messages []MessageItem, summarizeTypes []string, topicFocus string) (*SummarizeResult, error) {
	prompt := buildSummarizePrompt(messages, summarizeTypes, topicFocus)

	resolved, err := e.Registry.Resolve(ctx, e.Registry.DefaultModelID(ctx))
	if err != nil {
		return nil, fmt.Errorf("resolve model for summarize: %w", err)
	}

	req := &openaiclient.ChatRequest{
		Model: resolved.UpstreamModel,
		Messages: []openaiclient.Message{
			{Role: "system", Content: "你是一个对话摘要助手。严格按 JSON 格式输出结果，不要添加任何额外说明。"},
			{Role: "user", Content: prompt},
		},
		Temperature: pfloat64(0.3),
	}

	resp, err := resolved.Client.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm summarize call: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("llm summarize: empty response")
	}

	content := resp.Choices[0].Message.Content
	content = stripJSONMarkdown(content)

	var result SummarizeResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		slog.Warn("llm summarize parse failed", "content", content[:min(len(content), 200)], "err", err)
		return nil, fmt.Errorf("parse summarize result: %w", err)
	}

	return &result, nil
}

// buildExtractPrompt 构建实体抽取 prompt。
func buildExtractPrompt(messages []MessageItem, extractTypes []string) string {
	var sb strings.Builder
	sb.WriteString("从以下对话中提取以下类型的实体和关系：\n")
	sb.WriteString("1. 实体类型：PERSON（人物）, EVENT（事件）, PREFERENCE（偏好）, OBJECT（物品）, LOCATION（地点）, ORGANIZATION（组织）\n")
	sb.WriteString("2. 关系类型：knows, likes, dislikes, participated_in, owns, is_a, located_at, part_of\n")
	if len(extractTypes) > 0 {
		sb.WriteString("3. 重点关注提取维度：")
		sb.WriteString(strings.Join(extractTypes, ", "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("对话：\n")
	for _, m := range messages {
		contentJSON, _ := json.Marshal(m.Content)
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, string(contentJSON)))
	}
	sb.WriteString("\n")
	sb.WriteString("返回 JSON 格式（仅 JSON，不要任何其他文本）：\n")
	sb.WriteString(`{
  "entities": [
    {"type": "PERSON", "name": "实体名", "attributes": {"role": "用户"}, "importance": 0.8}
  ],
  "relations": [
    {"source": "实体A", "target": "实体B", "type": "likes", "weight": 0.7}
  ]
}`)
	return sb.String()
}

// buildSummarizePrompt 构建摘要 prompt。
func buildSummarizePrompt(messages []MessageItem, summarizeTypes []string, topicFocus string) string {
	var sb strings.Builder
	sb.WriteString("请对以下对话进行摘要分析：\n")
	if topicFocus != "" {
		sb.WriteString("重点关注话题：")
		sb.WriteString(topicFocus)
		sb.WriteString("\n")
	}
	if len(summarizeTypes) > 0 {
		sb.WriteString("摘要维度：")
		sb.WriteString(strings.Join(summarizeTypes, ", "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("对话：\n")
	for _, m := range messages {
		contentJSON, _ := json.Marshal(m.Content)
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, string(contentJSON)))
	}
	sb.WriteString("\n")
	sb.WriteString("返回 JSON 格式（仅 JSON，不要任何其他文本）：\n")
	sb.WriteString(`{
  "summary": "对话摘要（简洁，2-3 句话）",
  "topics": ["话题1", "话题2"],
  "keyEvents": ["关键事件1", "关键事件2"],
  "importance": 0.7
}`)
	return sb.String()
}

// stripJSONMarkdown 去除 Markdown 代码块标记。
func stripJSONMarkdown(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func pfloat64(v float64) *float64 { return &v }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}