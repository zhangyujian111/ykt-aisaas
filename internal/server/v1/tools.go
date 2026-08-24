package v1

import (
	"context"
	"encoding/json"
	"strings"

	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// ToolSource chat 集成的 MCP 工具源（mcp.Service 实现）。
type ToolSource interface {
	ListTools(ctx context.Context) []openaiclient.Tool
	Execute(ctx context.Context, name string, args json.RawMessage) mcp.ToolCallEvent
}

const maxToolRounds = 5

// runToolLoop 工具调用循环：LLM→tool_calls→执行→role:tool 回填→再调，
// 直到无 tool_calls 或达轮次上限。返回终答响应与全部工具事件。
func (h *ChatHandler) runToolLoop(ctx context.Context, client *openaiclient.Client,
	req *openaiclient.ChatRequest, tools ToolSource) (*openaiclient.ChatResponse, []mcp.ToolCallEvent) {

	var events []mcp.ToolCallEvent
	current := *req
	for round := 0; round < maxToolRounds; round++ {
		creq := current
		creq.Stream = false
		creq.Tools = req.Tools
		resp, err := client.Complete(ctx, &creq)
		if err != nil {
			return nil, events
		}
		if len(resp.Choices) == 0 || resp.FinishReason() != "tool_calls" {
			return resp, events
		}
		assistant := resp.Choices[0].Message
		current.Messages = append(current.Messages, assistant)

		for _, call := range assistant.ToolCalls {
			ev := tools.Execute(ctx, call.Function.Name, json.RawMessage(call.Function.Arguments))
			events = append(events, ev)
			resultJSON, _ := json.Marshal(toolResultOrErr(ev))
			current.Messages = append(current.Messages, openaiclient.Message{
				Role: "tool", ToolCallID: call.ID, Content: string(resultJSON),
			})
		}
	}
	// 轮次耗尽：最后再调一次不带 tools 的请求拿终答
	final := current
	final.Tools = nil
	resp, err := client.Complete(ctx, &final)
	if err != nil {
		return nil, events
	}
	return resp, events
}

func toolResultOrErr(e mcp.ToolCallEvent) any {
	if e.Err != "" {
		return map[string]any{"error": e.Err}
	}
	return e.Result
}

// synthesizeChunks 把终答文本合成流式 chunk（工具路径的 SSE 输出）。
func synthesizeChunks(id, model, content string, usage openaiclient.Usage) []*openaiclient.Chunk {
	runes := []rune(content)
	chunks := make([]*openaiclient.Chunk, 0, len(runes)/16+2)
	push := func(delta openaiclient.Message, finish string) {
		chunks = append(chunks, &openaiclient.Chunk{
			ID: id, Object: "chat.completion.chunk", Model: model,
			Choices: []openaiclient.Choice{{Index: 0, Delta: delta, FinishReason: finish}},
		})
	}
	if len(runes) > 0 {
		var sb strings.Builder
		for _, r := range runes {
			sb.WriteRune(r)
			if sb.Len() >= 16 { // 16 字节一批，打字机节奏
				push(openaiclient.Message{Content: sb.String()}, "")
				sb.Reset()
			}
		}
		if sb.Len() > 0 {
			push(openaiclient.Message{Content: sb.String()}, "")
		}
	}
	push(openaiclient.Message{}, "stop")
	chunks = append(chunks, &openaiclient.Chunk{
		ID: id, Object: "chat.completion.chunk", Model: model,
		Usage: &usage,
	})
	return chunks
}
