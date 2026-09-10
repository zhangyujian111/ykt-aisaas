package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sseConn 通过 HTTP Server-Sent Events 实现 MCP 传输。
//
// 协议：JSON-RPC 2.0 over HTTP POST /message，响应支持 JSON 或 SSE 流。
// 线程安全：HTTP 客户端并发安全，通过 mutex 保护连接状态。
type sseConn struct {
	cfg        ServerConfig
	httpClient *http.Client
	baseURL    string
	mu         sync.Mutex
}

func newSSEConn(cfg ServerConfig) *sseConn {
	return &sseConn{
		cfg:     cfg,
		baseURL: strings.TrimRight(cfg.URL, "/"),
	}
}

// connect 建立 HTTP 客户端并验证连通性。
func (s *sseConn) connect(ctx context.Context) error {
	if s.baseURL == "" {
		return fmt.Errorf("sse transport requires non-empty URL")
	}

	s.httpClient = &http.Client{
		Timeout: 0, // 无全局超时，每次请求通过 context 控制
		Transport: &http.Transport{
			MaxIdleConns:        1,
			MaxIdleConnsPerHost: 1,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}

	slog.Debug("sse client ready", "server", s.cfg.Name, "url", s.baseURL)
	return nil
}

// call 发送 JSON-RPC 请求到 HTTP 端点并解析响应。
func (s *sseConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpClient == nil {
		return nil, fmt.Errorf("sse connection not established")
	}

	// 序列化 params
	paramsBytes := []byte("null")
	if params != nil {
		var err error
		paramsBytes, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	// 构造 JSON-RPC 请求
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      newRequestID(),
		Method:  method,
		Params:  paramsBytes,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 构造 HTTP 请求
	endpoint := s.baseURL + "/message"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// 注入自定义 Headers（认证等）
	for k, v := range s.cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	// 超时控制
	timeout := s.cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq = httpReq.WithContext(reqCtx)

	// 发送请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to %s: %w", s.cfg.Name, err)
	}
	defer resp.Body.Close()

	// 非 2xx 响应
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sse server %s returned %d: %s",
			s.cfg.Name, resp.StatusCode, truncBytes(body, 200))
	}

	// 根据 Content-Type 选择解析方式
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return s.parseSSEEvent(resp.Body)
	}

	return s.parseJSONResponse(resp.Body)
}

// parseJSONResponse 解析 JSON 格式的 JSON-RPC 响应。
func (s *sseConn) parseJSONResponse(body io.Reader) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20)) // 1MB 上限
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse json-rpc response: %w (raw=%s)", err, truncBytes(raw, 200))
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("json-rpc error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// parseSSEEvent 解析 SSE 流中的 JSON-RPC 响应。
//
// SSE 格式：
//
//	data: {"jsonrpc":"2.0","id":"xxx","result":{...}}
//
//	（空行表示事件结束）
func (s *sseConn) parseSSEEvent(body io.Reader) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB

	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// 空行 = 事件结束
		if line == "" {
			if len(dataLines) > 0 {
				break
			}
			continue
		}

		// data: 前缀行
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
			continue
		}

		// 忽略其他字段（event:, id:, retry:）
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read sse stream: %w", err)
	}

	if len(dataLines) == 0 {
		return nil, fmt.Errorf("no data in sse event")
	}

	// 合并多行 data（某些实现会拆分多行）
	data := strings.Join(dataLines, "\n")

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
		return nil, fmt.Errorf("parse sse data as json-rpc: %w (data=%s)", err, truncStr(data, 200))
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("json-rpc error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// close 关闭 HTTP 客户端连接池。
func (s *sseConn) close() error {
	if s.httpClient != nil {
		s.httpClient.CloseIdleConnections()
	}
	return nil
}

// truncBytes 截断字节切片到指定长度（用于错误消息）。
func truncBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// truncStr 截断字符串到指定长度。
func truncStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}