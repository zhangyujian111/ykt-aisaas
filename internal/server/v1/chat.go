// Package v1 OpenAI 兼容接口（/v1/*）。
package v1

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/llm"
	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/rag"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// Retriever chat 集成的 RAG 检索接口（rag.Retriever 实现，接口隔离避免依赖扩散）。
type Retriever interface {
	RetrieveForChat(ctx context.Context, kbIDs []int64, query string, topK int) []rag.Citation
	BuildContext(cits []rag.Citation) string
}

// ChatHandler chat/completions。
type ChatHandler struct {
	Svc       *llm.ChatService
	Quota     *quota.Guard
	Meter     *metering.Recorder
	Retriever Retriever
	Tools     ToolSource // MCP 工具源（nil 时忽略 x-tools-mcp）
}

// NewChatHandler 构造。
func NewChatHandler(svc *llm.ChatService, q *quota.Guard, m *metering.Recorder, r Retriever, t ToolSource) *ChatHandler {
	return &ChatHandler{Svc: svc, Quota: q, Meter: m, Retriever: r, Tools: t}
}

// platformReq 平台扩展字段（x- 前缀）。
type platformReq struct {
	PersonaID        any    `json:"x-persona-id"`
	KnowledgeBaseIDs any    `json:"x-knowledge-base-ids"`
	ToolsMCP         bool   `json:"x-tools-mcp"`
	ConversationID   any    `json:"x-conversation-id"`
	DeviceID         string `json:"x-device-id"`
	EmotionTarget    string `json:"x-emotion-target"`
	UserID           string `json:"x-user-id"`
}

type chatBody struct {
	platformReq
	Model       string                 `json:"model"`
	Messages    []openaiclient.Message `json:"messages"`
	Stream      bool                   `json:"stream"`
	Tools       []openaiclient.Tool    `json:"tools"`
	Temperature *float64               `json:"temperature"`
	MaxTokens   *int                   `json:"max_tokens"`

	KnowledgeBaseIDs []int64 `json:"x-knowledge-base-ids"`
	ToolsMCP         bool    `json:"x-tools-mcp"`
}

// Completions POST /v1/chat/completions。
func (h *ChatHandler) Completions(c *gin.Context) {
	ctx := c.Request.Context()

	var body chatBody
	if err := c.ShouldBindJSON(&body); err != nil {
		web.AbortOpenAI(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}
	if len(body.Messages) == 0 {
		web.AbortOpenAI(c, errs.New(errs.InvalidParam, "messages 不能为空"))
		return
	}
	if body.Model == "" {
		body.Model = h.Svc.Registry.DefaultModelID(ctx)
		if body.Model == "" {
			web.AbortOpenAI(c, errs.New(errs.ModelNotFound, "未指定 model 且无默认模型"))
			return
		}
	}

	resolved, err := h.Svc.Registry.Resolve(ctx, body.Model)
	if err != nil {
		web.AbortOpenAI(c, err)
		return
	}

	// RAG：检索 → 引用 → prompt 注入
	var citations []rag.Citation
	if len(body.KnowledgeBaseIDs) > 0 && h.Retriever != nil {
		query := lastUserQuery(body.Messages)
		citations = h.Retriever.RetrieveForChat(ctx, body.KnowledgeBaseIDs, query, 5)
		if ctxBlock := h.Retriever.BuildContext(citations); ctxBlock != "" {
			body.Messages = append([]openaiclient.Message{{
				Role:    "system",
				Content: ctxBlock,
			}}, body.Messages...)
		}
	}

	// 配额预扣（估算：输入 token ≈ 字符数/2，上限 2048 预扣）
	estimated := quota.EstimateChat(body.Messages)
	if !h.Quota.Precheck(ctx, estimated) {
		web.AbortOpenAI(c, errs.New(errs.QuotaExceeded))
		return
	}

	upReq := h.Svc.UpstreamRequest(resolved, body.Messages, body.Temperature, body.MaxTokens, body.Tools)

	// MCP 工具：装载租户工具并入请求
	if body.ToolsMCP && h.Tools != nil {
		if regTools := h.Tools.ListTools(ctx); len(regTools) > 0 {
			upReq.Tools = append(upReq.Tools, regTools...)
		}
	}
	if len(upReq.Tools) > 0 {
		h.withTools(c, resolved, upReq, &body)
		return
	}

	if body.Stream {
		h.stream(c, resolved, upReq, citations, estimated)
		return
	}

	resp, err := resolved.Client.Complete(ctx, upReq)
	if err != nil {
		h.handleChatRefund(ctx, c, resolved, estimated, 0)
		h.Meter.RecordWithCtx(ctx, metering.Record{
			BizType: metering.BizLLM, Dimension: metering.DimLLMTokensIn,
			ModelID: resolved.ModelID, Status: 0, RequestID: web.RequestID(c),
		})
		web.AbortOpenAI(c, errs.Wrap(errs.ProviderError, err))
		return
	}

	h.recordUsage(c, resolved, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	// 成功后按实际用量退款
	h.handleChatRefund(ctx, c, resolved, estimated, int64(resp.Usage.PromptTokens))
	if len(citations) > 0 { // 非流式：x-rag-citations 顶层扩展字段
		c.JSON(200, gin.H{
			"id": resp.ID, "object": resp.Object, "model": resp.Model,
			"choices": resp.Choices, "usage": resp.Usage,
			"x-rag-citations": citations,
		})
		return
	}
	c.JSON(200, resp)
}

// withTools 工具调用路径：内部循环（非流式上游调用），对外按原请求形态输出。
func (h *ChatHandler) withTools(c *gin.Context, resolved *llm.Resolved, req *openaiclient.ChatRequest, body *chatBody) {
	ctx := c.Request.Context()

	resp, events := h.runToolLoop(ctx, resolved.Client, req, h.Tools)

	if !body.Stream {
		if resp == nil {
			web.AbortOpenAI(c, errs.New(errs.ProviderError, errLast(events)))
			return
		}
		h.recordUsage(c, resolved, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		out := gin.H{
			"id": resp.ID, "object": resp.Object, "model": resp.Model,
			"choices": resp.Choices, "usage": resp.Usage,
		}
		if len(events) > 0 {
			out["x-tool-calls"] = events
		}
		c.JSON(200, out)
		return
	}

	sse := web.NewSSE(c)
	defer sse.Close()
	for _, ev := range events {
		sse.WriteEvent("x-tool-call", ev)
	}
	var content string
	var usage openaiclient.Usage
	if resp != nil {
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}
		usage = resp.Usage
	} else {
		content = "抱歉，工具调用失败：" + errLast(events)
	}
	for _, ch := range synthesizeChunks("chatcmpl-tools", resolved.ModelID, content, usage) {
		sse.WriteData(ch)
	}
	h.recordUsage(c, resolved, usage.PromptTokens, usage.CompletionTokens)
	sse.WriteEvent("x-metering", gin.H{
		"inputTokens": usage.PromptTokens, "outputTokens": usage.CompletionTokens,
	})
	sse.WriteDone()
}

func errLast(events []mcp.ToolCallEvent) string {
	for _, ev := range events {
		if ev.Err != "" {
			return ev.Name + ": " + ev.Err
		}
	}
	return "上游无响应"
}

// lastUserQuery 取最后一条 user 消息作检索 query。
func lastUserQuery(msgs []openaiclient.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// stream SSE 流式（OpenAI chunk 透传 + x-metering/x-quota 扩展事件）。
func (h *ChatHandler) stream(c *gin.Context, resolved *llm.Resolved, req *openaiclient.ChatRequest, citations []rag.Citation, estimated int64) {
	ctx := c.Request.Context()
	sse := web.NewSSE(c)
	defer sse.Close()

	if len(citations) > 0 { // 流式：内容前先发引用事件
		sse.WriteEvent("x-rag-citations", citations)
	}

	var usage openaiclient.Usage
	var apiErr *openaiclient.APIError
	for ev := range resolved.Client.Stream(ctx, req) {
		// 心跳（无事件间隙时每 15s 一条注释行）
		select {
		case <-sse.Heartbeat():
			sse.Raw(": heartbeat\n\n")
		default:
		}
		if ev.Err != nil {
			if errors, ok := ev.Err.(*openaiclient.APIError); ok {
				apiErr = errors
			}
			sse.WriteEvent("x-error", gin.H{
				"code": errs.ProviderError, "message": ev.Err.Error(), "fatal": false,
			})
			continue
		}
		if ev.Chunk.Usage != nil {
			usage = *ev.Chunk.Usage
		}
		sse.WriteData(ev.Chunk)
	}
	_ = apiErr

	if usage.TotalTokens == 0 { // 上游未回 usage：按聚合文本估算
		usage.PromptTokens = 0
	}
	// 流式完成后按实际用量退款
	h.handleChatRefund(ctx, c, resolved, estimated, int64(usage.PromptTokens))
	h.recordUsage(c, resolved, usage.PromptTokens, usage.CompletionTokens)

	sse.WriteEvent("x-metering", gin.H{
		"inputTokens": usage.PromptTokens, "outputTokens": usage.CompletionTokens,
	})
	sse.WriteDone()
}

func (h *ChatHandler) recordUsage(c *gin.Context, resolved *llm.Resolved, in, out int) {
	ctx := c.Request.Context()
	if in > 0 {
		h.Meter.RecordWithCtx(ctx, metering.Record{
			BizType: metering.BizLLM, Dimension: metering.DimLLMTokensIn,
			Amount: int64(in), ModelID: resolved.ModelID, Status: 1,
			RequestID: web.RequestID(c),
		})
	}
	if out > 0 {
		h.Meter.RecordWithCtx(ctx, metering.Record{
			BizType: metering.BizLLM, Dimension: metering.DimLLMTokensOut,
			Amount: int64(out), ModelID: resolved.ModelID, Status: 1,
			RequestID: web.RequestID(c),
		})
	}
}

// ModelsHandler GET /v1/models。
type ModelsHandler struct{ Registry *llm.Registry }

// Models 列出租户可用模型（OpenAI list 格式 + x- 扩展字段）。
func (h *ModelsHandler) Models(c *gin.Context) {
	rows, err := h.Registry.ListChatModels(c.Request.Context())
	if err != nil {
		web.AbortOpenAI(c, err)
		return
	}
	data := make([]gin.H, 0, len(rows))
	for _, m := range rows {
		data = append(data, gin.H{
			"id": m.ModelID, "object": "model", "owned_by": m.Provider,
			"x-type": m.Type, "x-modality": parseModality(m.Modality),
			"x-context-length": m.ContextLength, "x-default": m.IsDefault == 1,
		})
	}
	c.JSON(200, gin.H{"object": "list", "data": data})
}

func parseModality(s string) []string {
	s = strings.Trim(s, "[]\" ")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// handleChatRefund chat 完成后按估算差额退款（异步非阻塞）。
// estimated 预扣量，actual 实际用量（失败时为 0）。
func (h *ChatHandler) handleChatRefund(ctx context.Context, c *gin.Context, resolved *llm.Resolved, estimated, actual int64) {
	if estimated <= actual {
		return
	}
	refund := estimated - actual
	tid, _ := tenant.FromSafe(ctx)
	if tid == 0 {
		return
	}
	// 异步退款，不阻塞响应
	go func() {
		bg := context.Background()
		if err := h.Quota.Refund(bg, tid, redisx.DimLLMTokensIn, refund); err != nil {
			slog.Warn("handleChatRefund failed",
				"err", err, "tenantID", tid, "dimension", redisx.DimLLMTokensIn, "refund", refund)
		}
	}()
}
