// Package client MCP 客户端：抽象传输协议（stdio/sse），管理多 server 连接池。
//
// 职责：
//   - Client 接口：ListTools / CallTool / Close
//   - 传输层：stdio（子进程 stdin/stdout）+ sse（HTTP Server-Sent Events）
//   - 协议层：JSON-RPC 2.0（MCP 2024-11-05）
//   - Manager：多 server 连接池管理
//   - 审计埋点：mcp.tool.call（slog 结构化日志）
//
// 边界：
//   - 不修改 P0 现有 MCP server 实现（internal/mcp/mcp.go）
//   - 不引入新的外部依赖
//   - 不写测试代码
package client

import (
	"context"
	"encoding/json"
	"time"
)

// Transport 传输协议类型。
type Transport string

const (
	// TransportStdio 本地子进程标准输入输出。
	TransportStdio Transport = "stdio"
	// TransportSSE HTTP Server-Sent Events 远程服务。
	TransportSSE Transport = "sse"
)

// ServerConfig MCP server 注册配置。
type ServerConfig struct {
	// Name 唯一标识（用于 Manager 索引）。
	Name string
	// Transport 传输协议（stdio / sse）。
	Transport Transport
	// Command stdio 启动命令（含 args），如 ["python3", "/opt/tools/weather.py"]。
	Command []string
	// URL sse 服务端地址，如 "https://mcp.example.com"。
	URL string
	// Headers sse 请求附加头（Authorization / X-API-Key 等）。
	Headers map[string]string
	// Timeout 单次调用超时，默认 30s。
	Timeout time.Duration
}

// Tool MCP 工具定义（对应 tools/list 响应）。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolResult 工具调用结果（对应 tools/call 响应）。
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent 工具返回内容块。
type ToolContent struct {
	Type string `json:"type"`       // "text" | "image" | "resource"
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"` // base64 for image
}

// JSONRPCRequest JSON-RPC 2.0 请求。
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 响应。
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 2.0 错误对象。
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Client MCP 客户端抽象接口。
// 每个 MCP server 对应一个 Client 实例，内部维护一个长连接。
type Client interface {
	// ListTools 列出服务端可用工具。
	ListTools(ctx context.Context) ([]Tool, error)

	// CallTool 调用指定工具并返回结果。
	CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)

	// Close 关闭连接并释放资源。
	Close() error
}