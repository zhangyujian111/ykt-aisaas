# YKT AI SaaS · Go 实现方案

> 版本：**v2.0-GO（2026-08-12）** — 实现语言从 Java 切换为 Go
> 状态：待评审
> 关联：[ARCHITECTURE.md](./ARCHITECTURE.md) · [DATABASE.md](./DATABASE.md) · [MODULES.md](./MODULES.md) · [API.md](./API.md)
>
> **本方案取代范围**：ARCHITECTURE §3 技术栈 / §7.3 代码示例 / MODULES §2 父pom / §5 依赖清单 / §9 时间线。
> **本方案保留范围**：业务架构、多租户隔离策略、OpenAI 兼容协议、WebSocket 通路 B、数据库设计（100%）、API 规范（100%）、xiaozhi-server 改造方案（100%，协议集成语言无关）、部署拓扑（同物理机）。

---

## 0. 为什么转 Go 反而成立（决策复盘）

v1.2 选 Java 的核心理由是**复用**：Spring AI 生态 + 拷贝 xiaozhi-ai 的 20+ Provider。重新评估后，这个理由被三个事实削弱：

| v1.2 假设 | 实际情况 |
|---|---|
| 拷贝 xiaozhi-ai provider 省大量工时 | MVP 只用 **4 个 provider**（OpenAI 兼容 LLM / Aliyun TTS / Aliyun ASR / Edge-TTS），且已统一 OpenAI 协议——拷贝收益缩小到 2 周以内 |
| Spring AI 的 RAG/MCP/ToolCall 不可替代 | MVP 的 RAG 是 PgVector CRUD + 余弦检索（pgx 直写即可）；MCP 有 `mark3labs/mcp-go`；ToolCall 是 OpenAI JSON 协议循环——均不依赖 Spring AI |
| Java 团队无 Go 经验 | Go 语法面小，4-6 周即可产出（配 1 周入门 + 结对） |

同时 Go 带来**结构性收益**（对本项目恰好命中）：

1. **多租户上下文：`context.Context` 天然传播** — Java 方案的头号风险（ThreadLocal + Reactor Context 在流式/异步链路丢失 → 串租户）在 Go 中**结构性消失**。context 显式传参，编译器强制经过每一层。
2. **goroutine 原生适配流式** — SSE/WebSocket/音频管道就是 channel + goroutine，无虚拟线程配置、无 Reactor 心智负担。
3. **同物理机部署更从容** — 与 xiaozhi-server 共享一台 16C32G：Go 单二进制 ~30MB、常驻内存 ~200MB（Java 约 1-2GB JVM），给 xiaozhi 和 MySQL/PG/Redis 留出更多余量。
4. **中间件减两个** — asynq（Redis）替代 RocketMQ + PowerJob，少运维 2 个组件。
5. **无 Spring Data Redis 版本锁定 bug 类问题**（v1.2 曾被迫锁 3.4.x）。

**代价（诚实列出）**：
- 团队 Go 学习曲线（首 2 周效率 ~60%，第 3 周恢复）
- 音频栈 CGo（libopus）+ sherpa-onnx-go 绑定，构建链略复杂
- 工期从 8-9 周 → **10-12 周**（含学习曲线）

---

## 1. 技术栈映射总表（Java → Go）

| 层 | Java（v1.2） | Go（v2.0） | 备注 |
|---|---|---|---|
| 语言 | Java 17 | **Go 1.23+** | 泛型可用于 Provider 抽象 |
| HTTP 框架 | SpringBoot 3.5.8 | **Gin** | 生态最大；SSE/WS 共存成熟 |
| WebSocket | spring-websocket | **gorilla/websocket** | 二进制帧久经考验 |
| LLM 框架 | Spring AI 1.1.x | **自研薄客户端**（~500 行） | OpenAI 协议简单；备选 Eino/langchaingo，接口留缝 |
| MCP Client | Spring AI MCP | **mark3labs/mcp-go** | 社区事实标准 |
| ORM | MyBatis-Plus 3.5.16 | **GORM** + 自研租户插件 | 插件机制做 WHERE tenantId 注入 |
| 向量库访问 | Spring AI PgVector | **pgx/v5 + pgvector-go** | 动态表名用参数化整数拼接 |
| 分页 | PageHelper | GORM `Pagination` 封装（~50 行） | - |
| 鉴权 | Sa-Token 1.39 | **golang-jwt/v5** + 自研中间件 | access+refresh 双 token |
| RBAC | Sa-Token 注解 | 权限串中间件 `RequirePerm("apikey:create")` | 简单直接；Casbin 备选 |
| 分布式锁 | Redisson | **redsync/v4** | - |
| 限流 | Redisson RPermit | **redis_rate**（GCRA）+ Lua 预扣 | 配额 Lua 复用 v1.2 脚本 |
| 消息队列 | RocketMQ 5.x | ~~删除~~ → asynq 内置 | 见 §6 |
| 任务调度 | PowerJob | **asynq Scheduler** | cron 表达式，月账单/对账 |
| 对象存储 | minio-java 8.5 | **minio-go/v7** | 官方，API 同构 |
| DB 迁移 | Flyway | **golang-migrate** | SQL 内容直接复用 DATABASE.md |
| API 文档 | Knife4j/SpringDoc | **swaggo/swag** 注解 | /swagger/index.html |
| 对象映射 | MapStruct | 手写 mapper 函数 | 编译期安全，不用反射 copier |
| 校验 | spring-validation | **go-playground/validator** | struct tag |
| 日志 | logback | **slog**（标准库） | 结构化 JSON |
| 配置 | application.yml | **viper**（yaml + env 覆盖） | - |
| 雪花 ID | 自研 | **bwmarrin/snowflake** | - |
| 加密 | AES/SHA-256 | 标准库 crypto | 同 v1.2 策略 |
| Opus 编解码 | Concentus | **hraban/opus**（CGo） | 静态链接 libopus |
| VAD | ONNX Runtime + Silero | **sherpa-onnx-go** | 模型文件复用 xiaozhi 的 silero.onnx |
| 本地 ASR 兜底 | SherpaOnnx | sherpa-onnx-go | 同库，P2 启用 |
| 监控 | Micrometer | **prometheus/client_golang** + pprof | - |
| 链路追踪 | （未定） | otel-go（可选，P1） | - |
| 测试 | JUnit5 + Mockito | **testify + testcontainers-go + httpexpect** | 租户隔离专项套件 |
| 前端 | Vue 3 + Element Plus | **不变** | 全部复用 |

**被删除的中间件**（运维简化）：
- ❌ RocketMQ（NameServer + Broker）→ asynq 跑在 Redis 上
- ❌ PowerJob Server → asynq Scheduler + asynq.Web UI

**保留的中间件**：MySQL 8.0、PostgreSQL 16 + PgVector、Redis 7、MinIO、Nginx。

---

## 2. 框架选型对比（为什么是 Gin 而不是 go-zero/Kratos）

| 候选 | 优势 | 劣势 | 结论 |
|---|---|---|---|
| **Gin** ✅ | 中间件生态最大；SSE 直接写 ResponseWriter；与 gorilla/ws 共存无冲突；学习成本最低 | 无代码生成、无内置服务发现 | **选定**：模块化单体 + 自有模块边界已定，不需要重框架 |
| go-zero | 内置代码生成/熔断/K8s | 面向微服务，单体被其约束；代码生成与我们的包结构冲突 | ❌ |
| Kratos | DDD 工程规范 | 规范太重，迁移我们的 DDD-lite 成本高 | ❌ |
| Echo/Fiber | 类似 Gin | 生态略小 / fasthttp 与 WS/SSE 兼容坑 | ❌ |

**原则**：框架只做 HTTP 路由 + 中间件。业务架构（模块边界/分层/多租户/计量）由我们自己的 `internal/platform` 承载——与 v1.2 的 framework 模块职责一一对应。

---

## 3. 项目结构（对应 MODULES.md 的 16 模块）

```
ykt-aisaas-go/
├── cmd/
│   └── aisaas/
│       └── main.go                  # 唯一入口：装配 config→db→redis→router→server
├── internal/
│   ├── platform/                    # ⭐ = v1.2 的 ykt-aisaas-framework
│   │   ├── config/                  # viper 加载
│   │   ├── logger/                  # slog 封装
│   │   ├── database/                # gorm 初始化 + 租户插件注册 + pgx pool
│   │   │   └── tenant_plugin.go     # ⭐ GORM 多租户插件（见 §5.2）
│   │   ├── redisx/                  # go-redis + redsync + Lua 脚本加载
│   │   ├── tenant/                  # ⭐ tenantctx（context key）+ 忽略表配置
│   │   ├── auth/                    # ⭐ ApiKey 中间件 + JWT + RequirePerm
│   │   ├── quota/                   # ⭐ 配额预扣（Lua）/ 限流（redis_rate）
│   │   ├── metering/                # ⭐ 计量：Record() → Redis INCR + asynq 异步落库
│   │   ├── asynqx/                  # asynq client/server/scheduler 封装
│   │   ├── miniox/                  # MinIO 客户端 + 路径规范
│   │   ├── web/                     # gin engine 装配/CORS/Recovery/RequestID/Swagger
│   │   └── errs/                    # = v1.2 的 ErrorCode 枚举（复用编号）
│   │
│   ├── tenantm/                     # = v1.2 的 ykt-aisaas-tenant（避免与 platform/tenant 重名）
│   │   ├── tenant/  member/  apikey/
│   │   └── billing/                 # plan/subscription/balance/quota/bill
│   │
│   ├── llm/                         # = ykt-aisaas-ai-llm
│   │   ├── service.go               # ChatService：persona 注入/RAG 注入/toolcall 循环
│   │   ├── registry.go              # 模型注册表（model_registry 表 + 缓存）
│   │   └── model.go                 # ChatModel 接口定义（本地定义，不 import pkg）
│   ├── tts/                         # = ykt-aisaas-ai-tts
│   │   ├── provider.go              # TtsProvider 接口
│   │   ├── aliyun_cosyvoice.go      # DashScope WS 流式
│   │   ├── edge.go                  # Edge-TTS 协议移植
│   │   └── factory.go               # tenantId 维度缓存
│   ├── asr/                         # = ykt-aisaas-ai-asr
│   │   ├── provider.go              # AsrProvider 接口（含 emotion 返回）
│   │   ├── aliyun_paraformer.go     # DashScope 流式识别
│   │   └── factory.go
│   ├── rag/                         # = ykt-aisaas-ai-rag
│   │   ├── kb.go  doc.go            # 知识库/文档元数据（MySQL）
│   │   ├── store.go                 # ⭐ PgVector 动态表（pgx）：ensure/query/insert
│   │   ├── ingest.go                # 切片+向量化（asynq 任务）
│   │   └── retrieve.go              # 召回 + 可选 rerank
│   ├── mcp/                         # = ykt-aisaas-ai-mcp
│   │   ├── client.go                # mark3labs/mcp-go 封装 + 探活
│   │   └── registry.go              # server/tool/binding（表 + 缓存）
│   ├── emotion/                     # = ykt-aisaas-ai-emotion
│   │   └── persona.go               # Persona CRUD + 解析缓存 + prompt 构造
│   ├── stream/                      # = ykt-aisaas-ai-stream（SSE）
│   │   └── sse.go                   # SSEWriter：event/data/心跳/背压超时
│   ├── realtime/                    # ⭐ = ykt-aisaas-ai-realtime（仅通路 B）
│   │   ├── handler.go               # /internal/xiaozhi/v1/realtime WS handler
│   │   ├── protocol/                # xiaozhi 协议移植（hello/listen/iot/abort/goodbye/mcp）
│   │   ├── session/                 # BridgeSession + SessionRegistry(Redis) + 超时清理
│   │   ├── audio/                   # opus codec(hraban) + vad(sherpa-onnx-go) + 帧队列
│   │   └── pipeline/                # ⭐ VAD→ASR→LLM→TTS + Barge-in（§5.6）
│   ├── job/                         # 长任务（asynq worker）
│   │   ├── voiceclone.go  finetune.go  ingest_doc.go  monthly_bill.go  reconcile.go
│   ├── admin/                       # = ykt-aisaas-admin（运营后台业务）
│   └── server/                      # = ykt-aisaas-server（路由装配）
│       ├── router.go                # v1 / api/v1 / internal/api/v1 / admin 四组
│       ├── v1/                      # OpenAI 兼容 handlers
│       ├── apiv1/                   # SaaS 自有 handlers
│       ├── internalapi/             # 内部超级租户 handlers
│       └── adminapi/
├── pkg/
│   └── openaiclient/                # ⭐ 自研 OpenAI 兼容客户端（Complete/Stream/Embedding）
├── migrations/                      # golang-migrate（SQL 复用 DATABASE.md）
│   ├── 000001_init.up.sql / .down.sql
│   └── 000002_xiaozhi_migration.up.sql
├── web/                             # Vue 3 前端（原样复用）
├── deploy/
│   ├── docker-compose-deps.yml      # MySQL/PG/Redis/MinIO（不变）
│   ├── Dockerfile                   # 多阶段构建 → ~30MB 镜像
│   └── aisaas.service               # systemd（同物理机直跑，无需容器化应用层）
├── go.mod
└── Makefile                         # make run/test/lint/migrate/swagger/build
```

**模块边界规则**（继承 v1.2 §0.2）：
1. `platform` 不 import 任何业务包
2. `llm/tts/asr/rag/mcp/emotion` 互不 import；跨模块协作只出现在 `realtime`（编排方）和 `server`（装配方）
3. 业务包对外只暴露接口 + DTO，不暴露 GORM 模型

---

## 4. 核心代码骨架

### 4.1 多租户上下文（⭐ 对比 Java 的最大简化）

```go
// internal/platform/tenant/ctx.go
package tenant

import "context"

type ctxKey struct{}

// With 注入租户 ID（在 ApiKey 中间件 / WS 握手 / asynq 任务入口调用）
func With(ctx context.Context, tenantID int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, tenantID)
}

// From 提取；丢失即 fail-fast（严禁返回 0 静默放行）
func From(ctx context.Context) int64 {
	if v, ok := ctx.Value(ctxKey{}).(int64); ok {
		return v
	}
	panic(errs.New(errs.TenantContextLost)) // 50001，触发告警
}

func FromSafe(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(ctxKey{}).(int64)
	return v, ok
}
```

**对比 Java**：v1.2 需要 `TransmittableThreadLocal` + `Hooks.enableAutomaticContextPropagation()` + Reactor `contextWrite` 三套机制协同，且有 5 条陷阱清单。Go 版只有一种机制：**context 显式传参**，编译器保证每一层都经过。原来 §5.2 的"陷阱清单"整节作废。

### 4.2 GORM 多租户插件（= MyBatis-Plus TenantLineInnerInterceptor）

```go
// internal/platform/database/tenant_plugin.go
package database

type TenantPlugin struct{ SkipTables map[string]bool }

func (p *TenantPlugin) Name() string { return "ykt_tenant" }

func (p *TenantPlugin) Initialize(db *gorm.DB) error {
	db.Callback().Query().Before("gorm:query").Register("tenant:q", p.addWhere)
	db.Callback().Update().Before("gorm:update").Register("tenant:u", p.addWhere)
	db.Callback().Delete().Before("gorm:delete").Register("tenant:d", p.addWhere)
	db.Callback().Create().Before("gorm:create").Register("tenant:c", p.fillCreate)
	return nil
}

func (p *TenantPlugin) addWhere(tx *gorm.DB) {
	if p.skip(tx) || tx.Statement.SkipHooks {
		return
	}
	tid, ok := tenant.FromSafe(tx.Statement.Context)
	if !ok {
		tx.AddError(errs.New(errs.TenantContextLost))
		return
	}
	tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "tenantId"}, Value: tid},
	}})
}

func (p *TenantPlugin) fillCreate(tx *gorm.DB) {
	if p.skip(tx) {
		return
	}
	tid, ok := tenant.FromSafe(tx.Statement.Context)
	if !ok {
		tx.AddError(errs.New(errs.TenantContextLost))
		return
	}
	// 反射/字段设置 tenantId（模型统一内嵌 TenantModel）
	tx.Statement.SetColumn("tenantId", tid)
}

func (p *TenantPlugin) skip(tx *gorm.DB) bool {
	return p.SkipTables[tx.Statement.Table]
}
```

忽略表清单 = DATABASE.md §0.3（ykt_aisaas_tenant / plan / dict_* / admin_* …），从 config 加载。

**Raw SQL 红线**：租户表禁止裸 SQL；确需手写 SQL 时必须 `WHERE tenantId = ?` 且参数来自 `tenant.From(ctx)`，用 linter（自定义 vet 规则）+ CR 兜底。

### 4.3 API Key 鉴权中间件

```go
// internal/platform/auth/apikey.go
func APIKey(svc *ApiKeyService, scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := extractBearer(c.GetHeader("Authorization")) // Bearer sk-aisaas-...
		if raw == "" {
			raw = c.Query("api_key") // WS 场景
		}
		bo, err := svc.Validate(c.Request.Context(), raw) // Redis 缓存 → MySQL(sha256)
		if err != nil { abort(c, err); return }
		if !bo.HasScope(scope) { abort(c, errs.New(errs.Forbidden)); return }
		if !bo.IPAllowed(c.ClientIp()) { abort(c, errs.New(errs.IPNotAllowed)); return }

		ctx := tenant.With(c.Request.Context(), bo.TenantID)
		ctx = authctx.WithActor(ctx, authctx.Actor{Type: "apikey", ID: bo.ID})
		ctx = requestid.With(ctx, ids.New()) // X-Request-Id 回写
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
```

内部超级租户（xiaozhi-server）走独立中间件 `InternalToken()`：校验 `X-Internal-Token` + 来源 IP = 127.0.0.1（同物理机），注入 `tenantId=1` + `isUnlimited`。

### 4.4 OpenAI 兼容客户端（替代 Spring AI，~500 行）

```go
// pkg/openaiclient/client.go
type Client struct {
	http *http.Client
	// baseUrl/apiKey 来自 model_registry 行（AES 解密后内存持有，不落日志）
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	// ⭐ 平台扩展（透传给上游前剥离，平台自身消费）
	XPersonaID        int64   `json:"x-persona-id,omitempty"`
	XKnowledgeBaseIDs []int64 `json:"x-knowledge-base-ids,omitempty"`
	XToolsMCP         bool    `json:"x-tools-mcp,omitempty"`
	XConversationID   int64   `json:"x-conversation-id,omitempty"`
	XDeviceID         string  `json:"x-device-id,omitempty"`
	XEmotionTarget    string  `json:"x-emotion-target,omitempty"`
	XUserID           string  `json:"x-user-id,omitempty"`
}

// Stream 返回 token 通道；usage 在最后一个 chunk（stream_options.include_usage）
func (c *Client) Stream(ctx context.Context, req *ChatRequest) (<-chan Chunk, <-chan error) {
	chunks := make(chan Chunk, 32)
	errCh := make(chan error, 1)
	go c.consumeSSE(ctx, req, chunks, errCh) // http req → bufio scan "data:" 行 → [DONE]
	return chunks, errCh
}

func (c *Client) Complete(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
func (c *Client) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)
```

**ToolCall 循环**（服务层，不进客户端）：LLM 返回 `tool_calls` → 执行（MCP/内置工具）→ 把结果以 `role:"tool"` 追加 → 再调，直到 `finish_reason != "tool_calls"`，最多 5 轮。

### 4.5 SSE 流式 Handler（OpenAI 兼容）

```go
// internal/server/v1/chat.go
func (h *ChatHandler) Completions(c *gin.Context) {
	req, err := h.parseAndResolve(c) // 模型路由：tenant_model_config → registry → client 缓存
	if err != nil { abortOpenAI(c, err); return }

	h.sse(c, func(sse *stream.SSEWriter) {
		defer sse.Close()

		chunks, errCh := h.svc.StreamChat(c.Request.Context(), req) // 内含 persona/RAG 注入
		var usage Usage
		for {
			select {
			case ch, ok := <-chunks:
				if !ok {
					sse.WriteEvent("x-metering", usage) // 扩展事件
					sse.WriteDone()                    // data: [DONE]
					metering.Record(c.Request.Context(), usage) // ⭐ 异步计量
					return
				}
				usage.Accumulate(ch)
				sse.WriteChunk(ch)
			case err := <-errCh:
				sse.WriteError(err) // x-error 事件，流不中断
				if errs.IsFatal(err) { return }
			case <-sse.Heartbeat(): // 15s 心跳注释行
			}
		}
	})
}
```

### 4.6 实时流水线（通路 B，⭐ 核心）

```go
// internal/realtime/pipeline/pipeline.go
type Pipeline struct {
	vad  audio.VAD
	asr  asr.Provider
	llm  llm.ChatModel        // 接口，本地定义
	tts  tts.Provider
	rag  rag.Retriever        // 可为 nil
	mcp  mcp.ToolSource       // 可为 nil
}

// Run 启动 5 个 goroutine 串联的流水线；ctx 取消 = Barge-in 级联熔断
func (p *Pipeline) Run(parent context.Context, sess *session.Bridge) {
	ctx, cancel := context.WithCancel(parent)
	sess.OnAbort(func() { // 设备发 abort（用户开口打断）
		cancel()          // ① 熔断 LLM 流/ TTS 合成
		sess.ClearTTSQ()  // ② 丢弃待播音频
	})

	pcmCh := make(chan []byte, 64)   // Opus→PCM
	speechCh := make(chan audio.Speech, 4) // VAD 切出的语音段
	asrCh := make(chan asr.Result, 4)
	llmCh := make(chan llm.Chunk, 64)
	ttsCh := make(chan []byte, 64)   // Opus 帧 → 设备

	go p.decodeLoop(ctx, sess.AudioIn(), pcmCh)          // hraban/opus
	go p.vadLoop(ctx, pcmCh, speechCh)                   // sherpa-onnx-go silero
	go p.asrLoop(ctx, speechCh, asrCh)                   // Paraformer 流式（含情绪）
	go p.llmLoop(ctx, asrCh, llmCh)                      // persona+RAG+MCP+toolcall
	go p.ttsLoop(ctx, llmCh, ttsCh)                      // CosyVoice 流式，逐句喂
	go sess.SendAudioLoop(ctx, ttsCh)                    // 20ms/帧下发

	<-ctx.Done() // 会话结束/打断/超时
}

func (p *Pipeline) llmLoop(ctx context.Context, asrCh <-chan asr.Result, out chan<- llm.Chunk) {
	for r := range asrCh {
		msgs := p.buildMessages(ctx, r) // persona system prompt + RAG 片段 + 历史(最近N轮,Redis)
		chunks, errCh := p.llm.Stream(ctx, &llm.ChatRequest{Messages: msgs, Tools: p.mcp.Tools(ctx)})
		if err := p.pumpLLM(ctx, chunks, errCh, out); err != nil {
			p.sess.SendError(err); continue
		}
		metering.Record(ctx, r.Usage()) // ASR 秒数也计量
	}
}
```

**要点**：
- Barge-in = `context.Cancel`：一次 `cancel()` 让所有 stage 的 `select ctx.Done()` 同时退出，天然级联——比 Java 版的 `ttsStage.cancel()` 手工编排更可靠。
- 每个 stage 出错只影响当轮语音段，pipeline 不死。
- 帧队列 `chan []byte, 64` 即背压：队列满时 VAD 丢帧（`select default`），保护内存。

### 4.7 计量（无 AOP 的 Go 模式）

Java 用 `@Metering` 切面；Go 用**中间件（预检）+ 显式 Record（实记）**两段式：

```go
// 预检：路由链上（估算量）
r.POST("/v1/chat/completions",
	auth.APIKey(svc, "llm"),
	quota.Precheck(quota.DimLLMTokensIn, quota.EstimateChat), // Redis Lua 预扣
	v1.ChatCompletions)

// 实记：流结束时调用（见 §4.5 的 metering.Record）
func Record(ctx context.Context, u Usage) {
	tid := tenant.From(ctx)
	rdb.IncrBy(ctx, key.QuotaUsed(tid, u.Dimension, period()), u.Amount()) // 实时余量
	enqueuer.EnqueueAsync(taskmeter.Write(u.Row(tid)))                     // asynq 批量落库(500/批)
}
```

对账 cron（asynq Scheduler）：每日凌晨比对 Redis 累计 vs `usage_detail` 聚合，偏差 > 1% 告警。

### 4.8 asynq 任务（替代 RocketMQ + PowerJob）

```go
// 声音克隆（长任务示例）
task := asynq.NewTask("voice:clone", payload,
	asynq.Queue("gpu"), asynq.MaxRetry(2), asynq.Timeout(45*time.Minute),
	asynq.Retention(24*time.Hour))
client.EnqueueTask(task)

// 定时（替代 PowerJob cron）
sched.Register("0 3 1 * *", asynq.NewTask("bill:monthly", nil))   // 月账单
sched.Register("0 4 * * *", asynq.NewTask("meter:reconcile", nil)) // 每日对账
sched.Register("@every 10m", asynq.NewTask("mcp:healthcheck", nil))

// asynq.Web → :8192 进度/重试 UI（替代 PowerJob 控制台）
```

Webhook 重试（5/30m/2h/6h/24h）：asynq `ProcessIn` 链或自定义队列。

---

## 5. 数据层落点

### 5.1 MySQL（GORM）

- 模型显式 `gorm:"column:tenantId"`（camelCase 列名，与 xiaozhi 约定一致，`NamingStrategy` 关闭复数+转换）
- 公共字段内嵌：

```go
type BaseDO struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TenantID   int64     `gorm:"column:tenantId"` // TenantModel，由插件填充
	CreateTime time.Time `gorm:"column:createTime;autoCreateTime"`
	UpdateTime time.Time `gorm:"column:updateTime;autoUpdateTime"`
	IsDeleted  int8      `gorm:"column:isDeleted;default:0"` // gorm.io/plugin/soft_delete
}
```

### 5.2 PgVector（pgx 直写，不走 GORM）

```go
// internal/rag/store.go
func (s *Store) ensureTable(ctx context.Context, tenantID, kbID int64, dim int) error {
	// tid/kbid 为 int64，%d 无注入面；表名白名单字符校验双保险
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS rag_chunk_tenant_%d_kb_%d (
			id BIGSERIAL PRIMARY KEY, docId BIGINT NOT NULL, chunkIndex INT NOT NULL,
			content TEXT NOT NULL, embedding vector(%d) NOT NULL, metadata JSONB,
			createTime TIMESTAMP DEFAULT now())`, tenantID, kbID, dim))
	...
	// ivfflat lists=100（复用 DATABASE.md §6.2）
}
```

### 5.3 迁移

golang-migrate，SQL 直接取 DATABASE.md 的 DDL（V1 init / V2 xiaozhi 迁移）。DATABASE.md **零改动**。

---

## 6. 配置样例（config.yaml）

```yaml
server: { port: 8190, adminPort: 8191, internalToken: "${INTERNAL_TOKEN}" }
mysql:  { dsn: "user:pass@tcp(127.0.0.1:3306)/ykt_aisaas?parseTime=true" }
postgres: { dsn: "postgres://user:pass@127.0.0.1:5432/ykt_aisaas_rag" }
redis:  { addr: "127.0.0.1:6379", password: "${REDIS_PASSWORD}" }
minio:  { endpoint: "127.0.0.1:9000", bucket: "ykt-aisaas" }
tenant:
  skipTables: [ykt_aisaas_tenant, ykt_aisaas_plan, ykt_aisaas_dict_type, ykt_aisaas_dict_item,
               ykt_aisaas_admin_user, ykt_aisaas_admin_role, ykt_aisaas_balance,
               ykt_aisaas_balance_transaction, ykt_aisaas_internal_tenant_config]
crypto: { aesKey: "${AES_KEY}" }   # model_registry.apiKeyEnc 解密
realtime:
  path: "/internal/xiaozhi/v1/realtime"
  allowedCIDRs: ["127.0.0.1/8"]    # 同物理机
  vadModel: "./models/silero_vad.onnx"   # 复用 xiaozhi 的模型文件
quota: { precheck: true }
metering: { batchSize: 500, flushSec: 3 }
```

---

## 7. Dockerfile（多阶段，~30MB 产物）

```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 apk add --no-cache gcc musl-dev opus-dev && \
    go build -trimpath -ldflags="-s -w" -o /out/aisaas ./cmd/aisaas

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata libopus
COPY --from=build /out/aisaas /usr/local/bin/aisaas
EXPOSE 8190 8191
ENTRYPOINT ["aisaas"]
```

（同物理机也支持裸跑：`make build` → systemd service，见 deploy/aisaas.service。）

---

## 8. 工期重估（10-12 周）

> 对比 v1.2 Java 版 8-9 周：+2~3 周 = Provider 全新实现（-拷贝捷径）+ 团队 Go ramp（前 2 周按 60% 效率计）。

| 周 | 平台主线 | 产出验证 |
|---|---|---|
| **W1** | Go 入训（1 周：effective-go + 本项目规范/代码骨架评审）+ 项目脚手架（gin/gorm/租户插件/config/slog/swagger）+ migrations V1 | 服务起得来，/healthz + swagger |
| **W2** | platform 全量：auth(APIKey/JWT/RequirePerm) + quota Lua + metering 骨架 + tenantm（租户/成员/APIKey CRUD） | 多租户隔离测试套件 ✅（**本周必须过**） |
| **W3** | openaiclient（SSE+toolcall）+ 模型注册表 + llm 服务 + persona | `POST /v1/chat/completions` 与 openai-python 对拍 ✅ |
| **W4** | tts（Aliyun CosyVoice WS 流式 + Edge 移植）+ asr（Paraformer 流式，含情绪） | `/v1/audio/speech` `/v1/audio/transcriptions` ✅ |
| **W5** | realtime：协议移植（hello/listen/…）+ session/registry + opus + vad | WS 握手/收发帧/超时清理 ✅ |
| **W6** | realtime：pipeline（VAD→ASR→LLM→TTS）+ **Barge-in** + 计量接入 | 端到端语音对话 demo（模拟设备客户端）✅ |
| **W7** | rag（pgvector store/ingest/retrieve）+ mcp（mcp-go client + registry） | 知识库上传→检索→引用回答 ✅ |
| **W8** | billing（plan/subscription/balance/bill + asynq cron）+ internal api + admin api | 充值→超额→拒止→月账单 ✅ |
| **W9** | job（voiceclone/finetune 占位 + webhook 重试）+ web 控制台联调（Vue 复用） | 控制台全流程 ✅ |
| **W10** | 压测（P95<3s 验证）+ 安全（隔离套件/限流/审计）+ 文档 | 全量回归 ✅ → **MVP 上线** |
| **W11-12** | buffer：性能调优 / xiaozhi 灰度问题修复 | - |

**xiaozhi-server 并行改造**（不变，协议语言无关）：W5 起 Persona 注入平台 client，W7 生产切换，W10 压测联合演练。

**人员**：3 后端（其中至少 1 人有 Go 生产经验做 review 门禁）+ 1 前端（Vue 全程复用）。

---

## 9. 风险与对策

| 风险 | 概率 | 影响 | 对策 |
|---|---|---|---|
| 团队 Go 不熟，前期质量差 | 高 | 延期 1-2 周 | W1 入训；前 2 周强制双人对代码；golangci-lint（errcheck/gocritic）+ CI 门禁 |
| GORM 租户插件遗漏场景（预加载/Join/Raw） | 中 | **串租户（严重）** | 隔离测试套件覆盖：CRUD/Preload/Join/Omit；lint 禁租户表裸 SQL；CI 每跑必测 |
| hraban/opus + sherpa-onnx-go CGo 构建问题 | 中 | 环境搭建卡壳 | 统一 Docker 构建；opus 静态链接；W5 第一天先做技术验证 spike |
| Edge-TTS 非官方协议变更 | 中 | 兜底 TTS 失效 | Aliyun 为主、Edge 仅免费层兜底；接口留 SelfHosted 位 |
| openaiclient 自研踩协议坑（SSE 半行/usage 缺失） | 中 | 流式不稳 | W3 与 openai-python/JS SDK 双端对拍 + 弱网测试（代理截断） |
| asynq 单 Redis 承载计量写入 | 低 | 积压 | 批量落库 500/批；超阈值同步降级写库；规模化换 Kafka（预留 Writer 接口） |
| 无 Spring AI 的 toolcall 循环 bug | 中 | 工具调用死循环 | 5 轮上限 + 超时熔断 + 单测覆盖 OpenAI 规范用例 |

---

## 10. v1.2 文档效力对照

| 文档 | 效力 |
|---|---|
| DATABASE.md | ✅ **100% 有效**（SQL 迁移文件直接生成） |
| API.md | ✅ **100% 有效**（协议与语言无关） |
| ARCHITECTURE.md | §3 技术栈 / §7.3-7.5 代码示例 → **被本文取代**；业务架构/隔离矩阵/部署/性能 SLA/xiaozhi §11-12 → 有效 |
| MODULES.md | 模块边界/职责/开发任务 → 有效；Maven 树/pom/依赖清单 → **被 §3 项目结构取代** |
| OVERVIEW-NON-TECHNICAL.md | ✅ 有效（对业务方无感知语言变化） |
| CHANGELOG.md | 追加 v2.0-GO 条目 |

---

## 11. 待评审

- [ ] Go 版本 1.23（需团队确认本地工具链；1.22 亦可，1.23 有 range-over-func 实验）
- [ ] llm 客户端自研 vs Eino（建议：自研 500 行先跑，接口留缝，复杂编排需求出现再评估 Eino）
- [ ] asynq 替代 MQ 的规模上限确认（当前预估：日 10 万调用 × 平均 3 条计量 ≈ 30 万任务/日，asynq 轻松承载；百万级再迁 Kafka）
- [ ] CGo 镜像里 musl vs glibc（alpine+musl 体积小；若 sherpa-onnx-go 兼容问题则换 debian-slim）
- [ ] 前端 swag 注解文档 vs 独立 OpenAPI 文件（建议 swag 注解，CI 生成 openapi.json 供前端 mock）
