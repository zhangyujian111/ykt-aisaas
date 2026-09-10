package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager 管理多个 MCP client 连接池。
//
// 每个 MCP server 对应一个 Client 实例，通过 server name 索引。
// 线程安全：所有操作通过 sync.RWMutex 保护。
type Manager struct {
	clients map[string]Client   // server name → client
	configs map[string]ServerConfig // server name → config
	mu      sync.RWMutex
}

// NewManager 创建空的 Manager。
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]Client),
		configs: make(map[string]ServerConfig),
	}
}

// Register 注册 MCP server 并建立连接。
//
// 流程：
//  1. 校验 cfg.Name 非空且不重复
//  2. NewClient(cfg) 创建客户端
//  3. client.Connect(ctx) 建立连接 + initialize 握手
//  4. 存入 clients map
//
// 返回 error 时保证不创建任何 client（原子性）。
func (m *Manager) Register(ctx context.Context, cfg ServerConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("mcp manager: server name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[cfg.Name]; exists {
		return fmt.Errorf("mcp manager: server %s already registered", cfg.Name)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return fmt.Errorf("mcp manager: register %s: %w", cfg.Name, err)
	}

	// baseClient.Connect 执行传输连接 + MCP initialize 握手
	bc, ok := client.(*baseClient)
	if !ok {
		// 不应发生：NewClient 总是返回 *baseClient
		client.Close()
		return fmt.Errorf("mcp manager: internal error: unexpected client type for %s", cfg.Name)
	}
	if err := bc.Connect(ctx); err != nil {
		client.Close()
		return fmt.Errorf("mcp manager: register %s: %w", cfg.Name, err)
	}

	m.clients[cfg.Name] = client
	m.configs[cfg.Name] = cfg

	slog.Info("mcp server registered",
		"name", cfg.Name,
		"transport", cfg.Transport,
	)
	return nil
}

// Get 按名称获取已注册的 Client。
//
// 返回 error 如果 server 不存在。
func (m *Manager) Get(name string) (Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.clients[name]
	if !ok {
		return nil, fmt.Errorf("mcp manager: server %s not found", name)
	}
	return c, nil
}

// ListServers 返回所有已注册 server 的名称列表。
func (m *Manager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// Unregister 移除 server 并关闭其连接。
//
// 关闭失败仅记录日志，不阻止移除。
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.clients[name]
	if !ok {
		return fmt.Errorf("mcp manager: server %s not found", name)
	}

	if err := c.Close(); err != nil {
		slog.Warn("mcp server close error", "name", name, "err", err)
	}

	delete(m.clients, name)
	delete(m.configs, name)

	slog.Info("mcp server unregistered", "name", name)
	return nil
}

// Close 关闭所有连接并清空注册表。
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for name, c := range m.clients {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))
		}
	}

	m.clients = make(map[string]Client)
	m.configs = make(map[string]ServerConfig)

	if len(errs) > 0 {
		return fmt.Errorf("mcp manager: close errors: %v", errs)
	}
	return nil
}

// ServerCount 返回当前已注册的 server 数量（用于监控）。
func (m *Manager) ServerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// GetConfig 返回指定 server 的配置（用于调试/监控）。
func (m *Manager) GetConfig(name string) (ServerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, ok := m.configs[name]
	if !ok {
		return ServerConfig{}, fmt.Errorf("mcp manager: server %s not found", name)
	}
	return cfg, nil
}