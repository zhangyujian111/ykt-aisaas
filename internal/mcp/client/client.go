package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// conn 内部传输连接抽象（不导出，仅供 baseClient 使用）。
type conn interface {
	connect(ctx context.Context) error
	call(ctx context.Context, method string, params any) (json.RawMessage, error)
	close() error
}

// baseClient 实现 Client 接口，将 JSON-RPC 协议逻辑与传输解耦。
type baseClient struct {
	cfg  ServerConfig
	conn conn
}

// NewClient 根据 ServerConfig 创建 MCP 客户端（未连接，需调用 Connect）。
func NewClient(cfg ServerConfig) (Client, error) {
	var c conn
	switch cfg.Transport {
	case TransportStdio:
		c = newStdioConn(cfg)
	case TransportSSE:
		c = newSSEConn(cfg)
	default:
		return nil, fmt.Errorf("mcp client %s: unsupported transport %s", cfg.Name, cfg.Transport)
	}
	return &baseClient{cfg: cfg, conn: c}, nil
}

// Connect 建立传输连接 + MCP initialize 握手。
// 通常在 Manager.Register 内部调用，也可独立调用。
func (c *baseClient) Connect(ctx context.Context) error {
	if err := c.conn.connect(ctx); err != nil {
		return fmt.Errorf("mcp client %s: connect: %w", c.cfg.Name, err)
	}

	// MCP initialize 握手（protocolVersion 2024-11-05）
	if err := c.initialize(ctx); err != nil {
		c.conn.close()
		return fmt.Errorf("mcp client %s: initialize: %w", c.cfg.Name, err)
	}
	return nil
}

// initialize 执行 MCP initialize 握手协议。
func (c *baseClient) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "ykt-aisaas",
			"version": "2.0.0",
		},
	}
	result, err := c.conn.call(ctx, "initialize", params)
	if err != nil {
		return err
	}

	// 解析服务端 capabilities（best-effort，仅用于日志）
	var initResp struct {
		Capabilities    map[string]any `json:"capabilities"`
		ServerInfo      map[string]any `json:"serverInfo"`
		ProtocolVersion string         `json:"protocolVersion"`
	}
	_ = json.Unmarshal(result, &initResp)
	slog.Debug("mcp initialize done",
		"server", c.cfg.Name,
		"protocol", initResp.ProtocolVersion,
		"capabilities", initResp.Capabilities,
	)

	// 发送 initialized 通知（one-way，不关心返回值）
	_, _ = c.conn.call(ctx, "notifications/initialized", nil)
	return nil
}

// ListTools 列出服务端可用工具。
func (c *baseClient) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := c.conn.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp client %s: list tools: %w", c.cfg.Name, err)
	}
	var resp struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp client %s: parse tools/list: %w", c.cfg.Name, err)
	}
	return resp.Tools, nil
}

// CallTool 调用指定工具。
func (c *baseClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	start := time.Now()
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	result, err := c.conn.call(ctx, "tools/call", params)
	elapsed := time.Since(start)

	// 审计埋点：mcp.tool.call
	slog.Info("mcp.tool.call",
		"server", c.cfg.Name,
		"tool", name,
		"duration_ms", elapsed.Milliseconds(),
		"error", errToString(err),
	)

	if err != nil {
		return nil, fmt.Errorf("mcp client %s: call tool %s: %w", c.cfg.Name, name, err)
	}

	// 解析 ToolResult；兼容两种格式：直接 {content,isError} 与嵌套 {content:[...]}
	var tr ToolResult
	if err := json.Unmarshal(result, &tr); err != nil {
		// 尝试嵌套解析
		var wrapped struct {
			Content []ToolContent `json:"content"`
			IsError bool          `json:"isError"`
		}
		if err2 := json.Unmarshal(result, &wrapped); err2 != nil {
			return nil, fmt.Errorf("mcp client %s: parse tools/call for %s: %w", c.cfg.Name, name, err)
		}
		tr = ToolResult{Content: wrapped.Content, IsError: wrapped.IsError}
	}
	return &tr, nil
}

// Close 关闭连接并释放资源。
func (c *baseClient) Close() error {
	return c.conn.close()
}

// errToString 将 error 转为空字符串或错误消息。
func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// newRequestID 生成 UUID v4 作为 JSON-RPC 请求 ID。
func newRequestID() string {
	return uuid.New().String()
}