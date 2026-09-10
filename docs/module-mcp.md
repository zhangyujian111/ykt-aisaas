# MCP 扩展 · 模块文档

> **版本**：V2.0 | **日期**：2026-09-02 | **包**：`internal/mcp/client`
> **状态**：已实现（P1 任务 T4）
> **关联**：[P1-DESIGN-SPEC.md](../docs/P1-DESIGN-SPEC.md) §1.4 · [ARCHITECTURE.md](./ARCHITECTURE.md)

---

## 1. 概述

MCP 扩展为 ykt-aisaas 提供**外部 MCP 工具调用**能力，支持两种传输协议：

| 协议 | 用途 | 连接模式 |
|------|------|---------|
| **stdio** | 本地命令行工具（Python / Node.js / Shell 脚本） | 子进程 stdin/stdout，JSON-RPC 2.0 |
| **sse** | 远程 MCP 服务（HTTP Server-Sent Events） | HTTP POST /message，JSON-RPC 2.0 |

### 与现有 MCP 的关系

| 组件 | 位置 | 职责 |
|------|------|------|
| **P0 MCP Server** | `internal/mcp/mcp.go` | 内置工具 + HTTP 工具执行 + 租户绑定 |
| **V2 MCP Client** | `internal/mcp/client/` | 外部 MCP server 连接 + 工具调用代理 |

V2 扩展不修改 P0 代码，通过 `Client` 接口松散耦合。

---

## 2. 架构

```
┌─────────────────────────────────────────────────────────┐
│                    ykt-aisaas MCP Client                 │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │   Manager    │  │  baseClient  │  │  baseClient  │  │
│  │  (连接池)     │  │  (server A)  │  │  (server B)  │  │
│  │              │  │              │  │              │  │
│  │ Register()   │  │ ListTools()  │  │ ListTools()  │  │
│  │ Get()        │  │ CallTool()   │  │ CallTool()   │  │
│  │ Close()      │  │ Close()      │  │ Close()      │  │
│  └──────────────┘  └──────┬───────┘  └──────┬───────┘  │
│                           │                  │          │
│                    ┌──────┴───────┐  ┌──────┴───────┐  │
│                    │ stdioConn    │  │  sseConn     │  │
│                    │ (子进程)      │  │  (HTTP SSE)  │  │
│                    └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## 3. 核心接口

### 3.1 Client 接口

```go
type Client interface {
    ListTools(ctx context.Context) ([]Tool, error)
    CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)
    Close() error
}
```

### 3.2 Manager 接口

```go
type Manager struct {
    // 内部字段不导出
}

func NewManager() *Manager
func (m *Manager) Register(ctx context.Context, cfg ServerConfig) error
func (m *Manager) Get(name string) (Client, error)
func (m *Manager) ListServers() []string
func (m *Manager) Unregister(name string) error
func (m *Manager) Close() error
```

### 3.3 ServerConfig

```go
type ServerConfig struct {
    Name      string            // 唯一标识
    Transport Transport         // "stdio" | "sse"
    Command   []string          // stdio: 启动命令
    URL       string            // sse: 服务端地址
    Headers   map[string]string // sse: 附加请求头
    Timeout   time.Duration     // 默认 30s
}
```

---

## 4. 传输协议

### 4.1 stdio 传输

**协议**：JSON-RPC 2.0 over stdin/stdout，每行一条 JSON 消息（newline-delimited）。

**连接流程**：
1. `exec.CommandContext` 启动子进程
2. 获取 stdin/stdout/stderr 管道
3. 发送 `initialize` 请求 → 接收 capabilities
4. 发送 `notifications/initialized` 通知
5. 就绪：`tools/list` / `tools/call`

**生命周期**：
- 创建：`Register(cfg)` 时启动子进程
- 活跃：连接期间保持进程存活
- 关闭：`Close()` 时 `Kill()` 子进程

**线程安全**：所有 Call 操作通过 `sync.Mutex` 串行化（stdin/stdout 非并发安全）。

**示例配置**：
```go
cfg := ServerConfig{
    Name:      "weather",
    Transport: TransportStdio,
    Command:   []string{"python3", "/opt/tools/weather.py"},
    Timeout:   10 * time.Second,
}
```

### 4.2 sse 传输

**协议**：JSON-RPC 2.0 over HTTP POST，响应支持 JSON 或 SSE 流。

**连接流程**：
1. 创建 `http.Client`（连接池 1，空闲超时 90s）
2. 发送 `initialize` 请求 → POST /message
3. 发送 `notifications/initialized` 通知
4. 就绪：`tools/list` / `tools/call` → POST /message

**SSE 响应解析**：
- 检测 `Content-Type: text/event-stream`
- 按 `data: ` 前缀提取数据行
- 空行表示事件结束
- 解析 JSON-RPC 响应

**认证**：通过 `Headers` 字段注入 `Authorization` / `X-API-Key` 等。

**示例配置**：
```go
cfg := ServerConfig{
    Name:      "knowledge-base",
    Transport: TransportSSE,
    URL:       "https://kb.example.com",
    Headers:   map[string]string{"Authorization": "Bearer token123"},
    Timeout:   15 * time.Second,
}
```

---

## 5. JSON-RPC 协议

### 5.1 实现的方法

| 方法 | 用途 | 阶段 |
|------|------|------|
| `initialize` | 握手 + 交换 capabilities | 连接建立 |
| `notifications/initialized` | 初始化完成通知 | 连接建立 |
| `tools/list` | 列出可用工具 | 运行时 |
| `tools/call` | 调用工具 | 运行时 |

### 5.2 请求格式

```json
{
    "jsonrpc": "2.0",
    "id": "uuid-v4",
    "method": "tools/call",
    "params": {
        "name": "get_weather",
        "arguments": {"city": "Beijing"}
    }
}
```

### 5.3 响应格式

```json
{
    "jsonrpc": "2.0",
    "id": "uuid-v4",
    "result": {
        "content": [
            {"type": "text", "text": "Beijing: 25°C, sunny"}
        ]
    }
}
```

### 5.4 错误格式

```json
{
    "jsonrpc": "2.0",
    "id": "uuid-v4",
    "error": {
        "code": -32601,
        "message": "Method not found",
        "data": "tools/unknown"
    }
}
```

---

## 6. 使用示例

### 6.1 注册 stdio 工具

```go
mgr := client.NewManager()

cfg := client.ServerConfig{
    Name:      "weather",
    Transport: client.TransportStdio,
    Command:   []string{"python3", "/opt/tools/weather.py"},
    Timeout:   10 * time.Second,
}
if err := mgr.Register(ctx, cfg); err != nil {
    log.Fatal("register weather server", "err", err)
}
```

### 6.2 注册 sse 工具

```go
cfg := client.ServerConfig{
    Name:      "knowledge-search",
    Transport: client.TransportSSE,
    URL:       "https://kb.internal.example.com",
    Headers:   map[string]string{"X-API-Key": "secret"},
    Timeout:   15 * time.Second,
}
if err := mgr.Register(ctx, cfg); err != nil {
    log.Fatal("register kb server", "err", err)
}
```

### 6.3 调用工具

```go
c, _ := mgr.Get("weather")
tools, _ := c.ListTools(ctx)
for _, t := range tools {
    log.Printf("tool: %s - %s", t.Name, t.Description)
}

result, err := c.CallTool(ctx, "get_weather", map[string]any{
    "city": "Beijing",
})
if err != nil {
    log.Printf("tool error: %v", err)
} else {
    for _, content := range result.Content {
        fmt.Println(content.Text)
    }
}
```

### 6.4 与 xiaozhi-server-go 集成

```
xiaozhi-server-go 不直接调用 MCP 工具。
xiaozhi-server-go 调 aisaas /v1/chat/completions，在请求中指定 x-tools-mcp:true
→ aisaas 内部自动调用 MCP 工具（已有 5 轮循环）
→ 返回最终结果给 xiaozhi-server-go

扩展：xiaozhi-server-go 可以通过 aisaas SDK 注册本地 MCP server：
→ POST /api/v1/mcp/servers { transportType: "stdio", command: "python3 /opt/tools/weather.py" }
→ aisaas 注册后，后续 chat 自动发现并调用
```

---

## 7. 错误处理

| 场景 | 处理 |
|------|------|
| 连接失败 | `Register` 返回 error，不创建 client |
| 进程退出 | `call` 时检测 `ProcessState.Exited()`，返回明确错误 |
| 调用超时 | context 超时 → 返回 `context.DeadlineExceeded` |
| 工具不存在 | 服务端返回 JSON-RPC error `-32602`，客户端透传 |
| JSON 解析失败 | 返回 `parse response: ...` 错误 |
| 网络错误 | sse HTTP 请求失败，返回原始错误 |
| 并发调用 | stdio 通过 mutex 串行化；sse 通过 HTTP 客户端并发安全 |

---

## 8. 审计埋点

每次工具调用自动记录结构化日志：

```json
{
    "msg": "mcp.tool.call",
    "server": "weather",
    "tool": "get_weather",
    "duration_ms": 234,
    "error": ""
}
```

日志级别：`INFO`（成功）/ `INFO`（失败，含 error 字段）。

---

## 9. 文件清单

| 文件 | 行数 | 职责 |
|------|------|------|
| `internal/mcp/client/types.go` | ~70 | 类型定义：Transport / ServerConfig / Tool / ToolResult / Client 接口 |
| `internal/mcp/client/client.go` | ~130 | baseClient 实现：connect + initialize + ListTools + CallTool + 审计 |
| `internal/mcp/client/stdio.go` | ~140 | stdioConn：子进程管理 + 逐行 JSON-RPC + mutex 串行化 |
| `internal/mcp/client/sse.go` | ~150 | sseConn：HTTP POST + SSE 响应解析 + 认证头注入 |
| `internal/mcp/client/manager.go` | ~120 | Manager：Register / Get / ListServers / Unregister / Close |
| `docs/module-mcp.md` | 本文 | 模块文档 |

---

## 10. 与 P0 代码的边界

- ✅ **不修改** `internal/mcp/mcp.go`（P0 MCP Service）
- ✅ **不修改** `internal/mcp/conflict.go`（冲突策略）
- ✅ **不修改** `internal/server/apiv1/mcp.go`（现有 handler）
- ✅ **不修改** `migrations/000004_mcp.up.sql`（现有 schema）
- ✅ **不修改** `main.go`（启动入口）
- 🔮 **未来扩展**：`internal/server/apiv1/mcp.go` 新增 `RegisterExternalServer` handler，调用 `client.Manager.Register`