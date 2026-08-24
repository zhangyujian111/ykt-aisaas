# YKT AI SaaS 平台 · 架构设计

> 版本：**v2.2（2026-08-24）** — 技术栈已切换 Go，实现细节以代码与 GOLANG-IMPLEMENTATION.md 为准
> 状态：MVP 基线 · 待评审
> 关联：[DATABASE.md](./DATABASE.md) · [MODULES.md](./MODULES.md) · [API.md](./API.md) · [OVERVIEW-NON-TECHNICAL.md](./OVERVIEW-NON-TECHNICAL.md) · [CHANGELOG.md](./CHANGELOG.md)
>
> **v2.2 关键变更**：Java 实现内容已全部移除，本文只保留与语言无关的架构决策；技术栈/代码结构见 [GOLANG-IMPLEMENTATION.md](./GOLANG-IMPLEMENTATION.md) 与 [MODULES.md](./MODULES.md)（对齐实际代码）。
>
> 沿用决策：WebSocket 仅通路 B；X-Internal-Token 鉴权；同物理机部署；xiaozhi-dialogue 拷贝不抽 SPI。

---

## 0. 读者指南

| 你是 | 看哪几章 |
|---|---|
| 架构师/技术负责人 | 全文 |
| 后端工程师 | §3 技术栈 · §4 模块化单体 · §5 多租户 · §6 AI 接入 · §7 流式 · §8 计量 |
| DBA | §3 技术栈 · §5 多租户 · §10 部署 |
| 运维/SRE | §3 技术栈 · §10 部署 · §11 演进 |
| 产品/业务方 | §1 系统全景 · §2 整体架构图 · [OVERVIEW-NON-TECHNICAL.md](./OVERVIEW-NON-TECHNICAL.md) |

---

## 1. 系统全景

### 1.1 平台定位

YKT AI SaaS 平台（下称"平台"）是 ykt 工作区的**第三条产品线**，是**内外统一的 AI 能力平台**：

- **对内**：作为 xiaozhi-server 的 AI 后端，承接所有设备对话的 LLM/TTS/ASR/RAG/MCP 调用（**MVP 即接入**）
- **对外**：作为多租户 SaaS，向第三方企业/开发者提供同样的 AI 能力 API

| 系统 | 角色 | 部署 | 端口 |
|---|---|---|---|
| `xiaozhi-server` | 设备协议层 + 设备/消息管理（**瘦身**，剥离 AI 实现） | 独立，与平台同机房 | 8091/8092 |
| `ykt-admin` | 内部运维管控（旁路监控） | 独立 | 8090 |
| **`ykt-aisaas`（本平台）** | **内外统一 AI 能力平台**（xiaozhi-server 的内部租户 + 对外多租户） | **独立，与 xiaozhi-server 同机房** | **8190/8191** |

- **红线**：
- 平台与 ykt-admin **完全独立**，互不依赖
- 平台数据库与 xiaozhi-server、ykt-admin **完全隔离**，禁止跨库写
- xiaozhi-server 作为平台的"内部超级租户"，享受平台所有能力（**MVP 即上线**，保留 1 个月稳定期后删除 `xiaozhi-ai` 模块）
- xiaozhi-server 与平台**同物理机部署**（localhost 调用，< 1ms 延迟）

### 1.2 平台能力清单（10 大能力）

| # | 能力 | MVP 阶段 | xiaozhi-server 是否依赖 | 接入方式 |
|---|---|---|---|---|
| 1 | 大模型调用（LLM） | P0 | **是**（设备对话核心） | OpenAI 兼容协议，商用 API |
| 2 | TTS 合成（含情绪/性格） | P0 | **是**（设备回复必需） | OpenAI `/v1/audio/speech` 兼容 + 厂商原生 |
| 3 | ASR 识别（含情绪） | P0 | **是**（设备听写必需） | OpenAI `/v1/audio/transcriptions` 兼容 + 厂商原生 |
| 4 | RAG 检索增强 | P0 | **是**（设备关联知识库） | 自有协议，PgVector |
| 5 | MCP Client / 工具调用 | P0 | **是**（设备工具，IoT 控制） | mcp-go / 自研工具执行器 |
| 6 | 情绪/性格 Persona | P0（升级，原 P1） | **是**（AI 角色管理） | 影响 system prompt + TTS 参数 |
| 7 | 声音克隆 | P1 | 否（可选增强） | 异步长任务 |
| 8 | 模型微调 | P1 | 否 | 异步长任务（占位 + 调度框架） |
| 9 | 流式处理 | **P0 含 WebSocket** | **是**（设备双向实时流） | SSE（外部）+ **WebSocket（xiaozhi-server）** |
| 10 | 多租户 + 计量计费 | P0 | **是**（内部租户独立计量） | 字段隔离 + 切面计量 |

**xiaozhi-server 关键约束**（驱动 MVP 升级）：
- **设备对话全链路 P95 < 3 秒**（VAD → ASR → LLM 首字 → TTS 首音频）
- **WebSocket 双向流**是设备实时通信的基础，不能降级为 SSE
- **情绪识别**（ASR 检测用户情绪）+ **情绪合成**（TTS 表达情绪）必须支持
- **设备级 Persona**（每台设备绑定 AI 角色，与 xiaozhi `sys_role` 表对应）

---

## 2. 整体架构图

### 2.1 部署架构

**v1.1 关键变化**：xiaozhi-server 不再有 AI 模块，所有 AI 调用走平台；新增 `ai-realtime` 模块（WebSocket 实时流，**P0**）；xiaozhi-server 与平台**同机房部署**（局域网 < 5ms 延迟）。

```
   ┌───────────────────────────────────────────────────────────────────────────┐
   │                         终端用户 / 客户端                                  │
   │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
   │  │ 小智 AI 设备│  │ 第三方 App  │  │ 浏览器      │  │ 后端服务        │ │
   │  │ (WebSocket) │  │ (HTTPS+SSE)│  │ (Web 控制台)│  │ (HTTPS)         │ │
   │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └────────┬────────┘ │
   └─────────┼────────────────┼────────────────┼──────────────────┼──────────┘
             │ WSS            │ HTTPS/SSE      │ HTTPS            │ HTTPS
             ▼                ▼                ▼                  ▼
   ┌─────────────────┐  ┌──────────────────────────────────────────────────────┐
   │                 │  │                                                      │
   │  xiaozhi-server │  │              ykt-aisaas 平台                         │
   │  (瘦身后)       │  │   ┌────────────────────────────────────────────┐    │
   │  :8091 API      │──┼──►│  Gateway / ApiKeyAuthFilter                │    │
   │  :8092 设备 WS  │  │   │   · 解析 API Key → TenantContext           │    │
   │                 │  │   │   · xiaozhi-server = 内部超级租户          │    │
   │  · 设备协议层   │  │   │   · 配额预扣 + 限流 + 审计                 │    │
   │  · 设备/消息    │  │   └─────────────────┬──────────────────────────┘    │
   │  · SessionMgr   │  │                     │                              │
   │                 │  │   ┌─────────────────▼──────────────────────────┐  │
   │  调用平台：     │  │   │  AI 能力服务（10 大模块）                  │  │
   │  · /v1/chat     │  │   │   LLM/TTS/ASR/RAG/MCP/Persona/            │  │
   │  · /v1/audio/*  │  │   │   VoiceClone/Finetune/Stream/Realtime      │  │
   │  · /v1/realtime │  │   │                                            │  │
   │   (WebSocket)   │  │   │  ⭐ ai-realtime：WebSocket 双向流（P0）    │  │
   └─────────────────┘  │   │   兼容 OpenAI Realtime API 协议            │  │
                        │   └─────────────────┬──────────────────────────┘  │
                        │                     │                              │
                        │   ┌─────────────────▼──────────────────────────┐  │
                        │   │  Provider Layer（拷贝自 xiaozhi-ai）       │  │
                        │   │   OpenAI/DeepSeek/智谱/阿里/讯飞/Edge      │  │
                        │   └─────────────────┬──────────────────────────┘  │
                        │                     │                              │
                        │   ┌─────────────────▼──────────────────────────┐  │
                        │   │  GORM + TenantPlugin（租户拦截）          │  │
                        │   │  Metering 切面 · AISAAS 计量                │  │
                        │   └──────────────────────────────────────────────┘  │
                        └─────────┬──────────────┬───────────────┬───────────┘
                                  │              │               │
                                  ▼              ▼               ▼
                          ┌──────────┐    ┌──────────┐    ┌──────────┐
                          │ MySQL    │    │ PostgreSQL│    │ Redis    │
                          │ 业务/计费│    │ +PgVector │    │ 缓存/配额│
                          │ :3306    │    │ :5432     │    │ :6379    │
                          └──────────┘    └──────────┘    └──────────┘
                                                                │
                                                                ▼
                                                        ┌──────────────────┐
                                                        │ RocketMQ :9876   │
                                                        │ · 计量事件       │
                                                        │ · 长任务调度     │
                                                        └──────────────────┘
```

> ❌ 旧版本（v1.0）已废弃：原架构假设 xiaozhi-server 双轨运行，AI 调用不走平台。新架构中 xiaozhi-server 作为平台首个内部租户，所有 AI 调用通过平台完成。

### 2.2 外部依赖（商用 AI API）

```
平台 Provider Layer
   │
   ├──► OpenAI（gpt-4o / gpt-4o-mini）            — LLM 主力
   ├──► DeepSeek（deepseek-chat / deepseek-reasoner）— LLM 廉价
   ├──► 智谱 GLM（glm-4 / glm-4-flash）           — LLM 国产备选
   ├──► 阿里 DashScope（qwen / embedding / cosyvoice）— LLM/TTS/ASR/Embedding
   ├──► 微软 Edge-TTS                              — TTS 免费兜底
   └──► 后期自建：vllm / ollama / cosyvoice-server — 自建模型，统一 OpenAI 协议
```

---

## 3. 技术栈

### 3.1 锁定版本（Go）

| 类别 | 选型 | 版本 | 备注 |
|---|---|---|---|
| 语言 | **Go** | 1.23+ | v2.0-GO 起（Java 版废弃） |
| HTTP 框架 | Gin | 1.10 | SSE/WS 共存 |
| WebSocket | gorilla/websocket | — | realtime 通路 B（Phase 3） |
| LLM 客户端 | 自研 openaiclient | — | ~500 行，SSE 半行保护 |
| MCP | mark3labs/mcp-go + 自研执行器 | — | http/builtin 工具 |
| ORM | GORM + 自研 TenantPlugin | 1.25 | 多租户 QUD 拦截 |
| 向量库 | pgx/v5 + pgvector-go | — | 动态表 per (tenant,kb) |
| 鉴权 | golang-jwt/v5 + 自研 ApiKey 中间件 | — | sha256 哈希 + Redis 缓存 |
| 缓存/配额 | go-redis + Lua | 9.x | 原子预扣防超卖 |
| 任务 | 自研 goroutine 管道 | — | rag ingest 等（asynq 待引入） |
| 对象存储 | minio-go（待引入） | — | MVP 文本内联 DB |
| DB 迁移 | golang-migrate | — | migrations/ 000001-000005 |
| 日志 | slog（标准库 JSON） | — | — |

**中间件**：MySQL 8.0（:13306）、Redis 7（:16379）、PostgreSQL 16+PgVector（:15432）。~~RocketMQ/PowerJob~~ 已删除（asynq 或自研管道替代）。

### 3.2 框架层（internal/platform）

平台自有框架层：`errs/config/tenant/database/redisx/auth/quota/metering/web/ids/crypto`。详见 [MODULES.md §1](./MODULES.md)。

## 4. 模块化单体架构

### 4.1 为什么是单体（不是微服务）

| 因素 | 决策依据 |
|---|---|
| MVP 压力 | 单体开发快，无注册中心/网关运维成本 |
| 团队规模 | Go 主力（aisaas/admin-go），无需跨语言协作 |
| 流量预期 | "能跑就行"，单实例足够 |
| 演进成本 | 每个能力一个 Go 包，未来按目录拆服务即可 |

### 4.2 代码结构（详见 [MODULES.md](./MODULES.md)）

Go 单模块工程 `ykt.dev/aisaas`：`cmd/aisaas` + `internal/platform`（框架层）+ `internal/{llm,tts,asr,rag,mcp,billing,tenantm}`（业务包）+ `internal/server`（路由装配）。业务包互不依赖，跨模块经接口注入协作。

### 4.3 拆分边界（**为未来微服务化预留**）

每个 `ykt-aisaas-ai-*` 模块对外暴露的 Service 接口必须：
1. 只用基础类型 + DTO（**禁止泄露 DO**）
2. 不依赖其他 `ai-*` 模块（如必须依赖，走 framework 抽象）
3. 通过 `applicationEvent` 而非直接调用做跨模块异步通知

这样未来要拆"LLM 服务""TTS 服务"独立部署时，**每个模块加 Controller + 启动器即可独立成服务**。

---

## 5. 多租户架构（**核心**）

### 5.1 隔离策略

**字段级隔离**（shared DB / shared schema）：
- 所有业务表强制带 `tenantId BIGINT NOT NULL`
- GORM `TenantPlugin` 自动拼 `WHERE tenantId=?`（Create 自动填充）
- 系统表（`ykt_aisaas_tenant` / `ykt_aisaas_plan` / `ykt_aisaas_dict`）通过 ignore 列表豁免

### 5.2 租户上下文（Go context 唯一机制）

```go
// platform/tenant/ctx.go —— 全平台唯一传播方式
func With(ctx context.Context, tenantID int64) context.Context
func From(ctx context.Context) int64          // 缺失 panic（fail-fast）
func FromSafe(ctx context.Context) (int64, bool)
```

- 同步请求：auth 中间件注入 → GORM TenantPlugin 从 ctx 拼条件
- 异步任务：goroutine 入口显式 `tenant.With`（rag ingest 已验证）
- ~~Java 版 TTL + Reactor Context 双路径与 5 条陷阱~~ 结构性消失（v1.2 §5.2 已废弃）

### 5.3 资源隔离矩阵

| 资源 | 隔离方式 | 示例 |
|---|---|---|
| MySQL 业务表 | `WHERE tenantId=?` 自动拼 | `SELECT ... FROM ykt_aisaas_apikey WHERE tenantId=1001` |
| Redis Key | `aisaas:tenant:{tid}:*` 前缀 | `aisaas:tenant:1001:quota:daily:20260812` |
| PgVector collection | `tenant_{tid}_kb_{kbid}` | `tenant_1001_kb_5` |
| MinIO 路径 | `/tenant/{tid}/{bizType}/{uuid}` | `/tenant/1001/audio/abc.mp3` |
| API Key | 每租户独立，gateway 解析 → 注入 TenantContext | `sk-aisaas-xxx` |
| 私有微调模型 | `model_registry.tenantId` 绑定 | 模型权重 `/tenant/{tid}/finetune/...` |
| 共享商用模型 | 共享调用池（共享 apiKey），按租户计量 | OpenAI 共用平台 apiKey |
| MCP 工具 | `mcp_tool.tenantId`（NULL=全局可见） | 全局天气工具 + 租户私有 CRM 工具 |

### 5.4 大客户升级路径（**P2，MVP 不实现**）

为关键客户提供**独立 schema** 模式：
- 同实例不同 schema：`tenant_1001. ykt_aisaas_*`
- 通过 `@DS("tenant_1001")` 动态切换数据源
- 现有 `TenantLineInnerInterceptor` 在该模式下 disable

---

## 6. AI 能力接入（**OpenAI 协议统一**）

### 6.1 核心原则

> **所有 AI 接入走 OpenAI 兼容协议**。商用 API 选支持 OpenAI 协议的厂商；自建模型用 vllm/ollama/cosyvoice-server 等 OpenAI 兼容 inference server。

### 6.2 LLM 接入

```go
// internal/llm/registry.go —— 一个客户端搞定所有 OpenAI 兼容 provider
resolved, err := registry.ResolveFor(ctx, modelID, "chat")
// resolved.Client = openaiclient.New(row.BaseURL, aesDecrypt(row.APIKeyEnc))
// OpenAI / DeepSeek / 智谱 / DashScope / 自建 vllm —— 仅 baseUrl 不同
```

**模型注册表**（详见 [DATABASE.md §3.5](./DATABASE.md)）：
- 系统级模型：`tenantId IS NULL`，所有租户可用
- 租户私有模型：`tenantId = ?`，仅本租户可见（如微调产出）

**MVP 默认模型清单**：

| provider | baseUrl | 用途 | 价格（每千 token 输入/输出） |
|---|---|---|---|
| OpenAI | `https://api.openai.com/v1` | gpt-4o-mini 兜底 / gpt-4o 高质量 | $0.15 / $0.60（4o-mini） |
| DeepSeek | `https://api.deepseek.com/v1` | deepseek-chat 廉价主力 | ¥0.001 / ¥0.002 |
| 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4` | glm-4-flash 免费 / glm-4 国产备选 | ¥0.001 / ¥0.001 |
| 阿里 DashScope（OpenAI 兼容） | `https://dashscope.aliyuncs.com/compatible-mode/v1` | qwen-turbo / qwen-plus | ¥0.0003 / ¥0.0006 |
| 自建 vllm | `http://vllm:8000/v1` | 内部模型 | 内部成本 |

### 6.3 TTS / ASR 接入

OpenAI 协议有 `/v1/audio/speech` 和 `/v1/audio/transcriptions`，但**各厂商私有协议更强大**（情绪、声音克隆、流式）。MVP 策略：

| 能力 | 商用 MVP | 自建路线 |
|---|---|---|
| TTS 主力 | Aliyun CosyVoice（DashScope 协议） | CosyVoice 部署成 OpenAI 兼容 server |
| TTS 兜底 | 微软 Edge-TTS（免费） | 同上 |
| ASR 主力 | Aliyun Paraformer | FunASR 部署成 OpenAI 兼容 server |
| Embedding | Aliyun text-embedding-v3 / OpenAI text-embedding-3 | bge-m3 自建 |

**Provider 抽象**：`tts.Service` / `asr.Service` —— OpenAI 兼容透传（`/v1/audio/speech` chunked 流式、`/v1/audio/transcriptions` multipart），租户/模型路由由 `llm.Registry.ResolveFor(ctx, modelID, "tts"|"asr")` 统一承担。情绪字段（`x-emotion`）双向透传。

### 6.4 RAG 接入

- **向量库**：PostgreSQL + PgVector 扩展（同主机）
- **每个租户的每个知识库一张表**：`rag_chunk_tenant_{tid}_kb_{kbid}`（按规则命名）
- **Embedding**：复用 LLM Provider 体系（`getEmbeddingModel(cfg)`）
- **检索流程**：

```
Query 文本 → Embedding → PgVector 余弦检索 topK=10
  → 可选 Rerank（DashScope rerank 模型）
  → 拼 Prompt → LLM 流式响应
```

---

## 7. 流式架构（**v1.1 重大调整：WebSocket 升 P0**）

### 7.1 协议选型

| 协议 | 用途 | MVP |
|---|---|---|
| **WebSocket 双向流**（通路 B：xiaozhi 内部协议） | ⭐ **xiaozhi-server 设备实时对话**（音频上行 + 文本/音频下行） | **✅ P0** |
| ~~WebSocket（通路 A：OpenAI Realtime 兼容）~~ | ~~第三方租户实时对话~~ | **❌ 延后到 V1.1** |
| **SSE** | LLM 流式响应、ASR 流式结果（外部租户、Web 控制台） | ✅ P0 |
| **HTTP Chunked** | TTS 单向音频流（外部租户） | ✅ P0 |
| **gRPC stream** | 服务间流式 | 不做（单体） |

**为什么 MVP 只做通路 B**：
- xiaozhi-server 接入是 MVP 必需场景，通路 B 满足
- 外部租户的实时对话需求暂用 SSE + HTTP Chunked（单向流足够，不需要双向）
- 通路 A 留到 V1.1，平台对外开放实时 API 时再做
- **可节省 1-2 周 MVP 工时**

### 7.2 SSE 事件格式

LLM 流式响应（兼容 OpenAI）：
```
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"你"},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"好"},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"finish_reason":"stop","index":0}]}

data: [DONE]
```

平台扩展事件（前缀 `x-`，避免与 OpenAI 冲突）：
```
event: x-metering
data: {"inputTokens":12,"outputTokens":8,"costCents":0.05}

event: x-quota
data: {"remaining":9980,"resetAt":"2026-08-13T00:00:00+08:00"}

event: x-tool-call
data: {"tool":"weather","args":{"city":"北京"},"result":{...}}
```

### 7.3 流式抽象

```go
// internal/platform/web/sse.go
type SSEWriter struct{ ... }          // data:/event:/[DONE]/15s 心跳注释行
func NewSSE(c *gin.Context) *SSEWriter
func (s *SSEWriter) WriteData(v any)  // data: {...}
func (s *SSEWriter) WriteEvent(name string, v any) // x-metering / x-tool-call / x-rag-citations
```

音频流（TTS）走 HTTP chunked 透传；WebSocket（通路 B）Phase 3 引入 gorilla/websocket。

### 7.4 背压控制

- **音频流（TTS / 设备音频下行）**：消费慢于生产时丢帧（保留关键帧）
- **LLM 流式**：消费慢于生产 30s 自动断开（防止僵尸连接）
- **ASR 流式（设备音频上行）**：客户端音频上传分块大小 100ms，超过 1s 没收到 → 主动 ping
- **WebSocket 设备实时**：每帧 Opus 20ms，背压策略 = 丢弃旧帧 + 通知客户端减速

### 7.5 WebSocket 实时流（**v1.2：MVP 只做通路 B**）

平台规划两条 WebSocket 通路，**MVP 只做通路 B**：

#### 通路 A：`/v1/realtime`（OpenAI Realtime API 兼容） — **延后到 V1.1**

**用途**：第三方租户 / 未来 xiaozhi-server 升级的实时对话接入
**鉴权**：URL 参数 `?api_key=sk-aisaas-xxx`
**协议**：完全兼容 [OpenAI Realtime API](https://platform.openai.com/docs/guides/realtime)
**MVP 状态**：❌ 不做。外部租户 MVP 阶段用 SSE + HTTP Chunked 即可满足需求。

#### 通路 B：`/internal/xiaozhi/v1/realtime`（xiaozhi-server 专用） — **MVP P0 必做**

**用途**：xiaozhi-server 复用现有设备协议（自定义 JSON + Opus 帧），平台透明对接 AI 调用
**鉴权**：`X-Internal-Token` Header（内部超级租户专用密钥）+ IP 白名单（生产强制；MVP 因同物理机可豁免）
**协议**：兼容 xiaozhi 现有协议（`hello`/`listen`/`iot`/`abort`/`goodbye`/`mcp` 消息类型）
**实现位置**：`ykt-aisaas-ai-realtime` 模块（详见 [MODULES.md](./MODULES.md)）
**实现方式**：从 `xiaozhi-dialogue` **拷贝**关键类（WebSocketHandler、ChatSession、SessionManager、消息类、VAD、Opus codec），不抽 SPI 共享（解耦优先）

#### 为什么不做通路 A（MVP 阶段）

| 角度 | 分析 |
|---|---|
| **必要性** | xiaozhi-server 接入只需要通路 B（保留现有协议零改造风险） |
| **外部租户需求** | MVP 阶段外部租户的"实时对话"场景少，SSE 单向流（LLM 流式响应）+ HTTP Chunked（TTS 音频）+ POST（ASR）够用 |
| **开发成本** | 通路 A 需要 OpenAI Realtime 完整事件实现（约 1-2 周），延后到 V1.1 |
| **未来兼容** | 通路 B 的实现已经拷贝了 xiaozhi-dialogue 协议，未来加通路 A 不冲突 |

---

## 8. 计量计费架构（**SaaS 命脉**）

### 8.1 计费模型

```
                       ┌──────────────────────────────┐
                       │    租户开通（注册即开通）     │
                       └──────────────┬───────────────┘
                                      │ 选择套餐
                                      ▼
                       ┌──────────────────────────────┐
                       │  套餐 Plan                    │
                       │  · 免费版 / 标准 / 企业       │
                       │  · 月固定费 + 含配额          │
                       └──────────────┬───────────────┘
                                      │ + 预付余额
                                      ▼
   每次调用  ─────────────────►  预扣配额（Redis Lua 原子）
                                      │
                                      ▼
                              实际计量（@Metering 切面）
                                      │
                                      ▼
                          RocketMQ → 计费聚合 Worker
                                      │
                          ┌───────────┴───────────┐
                          ▼                       ▼
                  配额实际扣减                 生成账单（按月）
                          │                       │
                          ▼                       ▼
                  余额不足 → 拒绝调用         用户充值（支付宝/微信）
```

### 8.2 计量维度

| 资源 | 单位 | 计量时机 | 计费时机 |
|---|---|---|---|
| LLM input tokens | 个 | 调用前估算 | 调用后实际 |
| LLM output tokens | 个 | 流式累计 | 流式结束 |
| TTS 字符数 | 个 | 调用前 | 调用后 |
| TTS 秒数 | 秒 | 调用后 | 调用后 |
| ASR 秒数 | 秒 | 调用后 | 调用后 |
| Embedding tokens | 个 | 调用后 | 调用后 |
| RAG 检索次数 | 次 | 调用前 | 调用前 |
| 文档存储 | MB·天 | 每日定时 | 月结 |
| 声音克隆 | 分钟 | 任务完成 | 任务完成 |
| 模型微调 | GPU·小时 | 任务完成 | 任务完成 |

### 8.3 计量实现（Go 无 AOP，两段式）

```go
// 预检：handler 内 quota.PrecheckDim(ctx, dim, 估算量) —— Redis Lua 原子预扣
// 实记：响应完成 meter.RecordWithCtx(ctx, Record{...})
//        → Redis INCR（实时余量）→ 批量 flush usage_detail + 按价扣余额（浮点累计取整）
```

### 8.4 配额预扣（防超卖）

```lua
-- Redis Lua：原子预扣
local key = KEYS[1]  -- aisaas:tenant:1001:quota:llm_tokens:202608
local amount = tonumber(ARGV[1])
local remaining = tonumber(redis.call('GET', key) or '0')
if remaining < amount then
    return -1  -- 配额不足
end
return redis.call('DECRBY', key, amount)
```

---

## 9. 长任务架构（声音克隆 / 模型微调）

### 9.1 任务生命周期

```
submit → queued → running → success
                    │
                    ├─► failed
                    ├─► cancelled
                    └─► timeout
```

### 9.2 调度架构（PowerJob）

```
租户提交任务
    │
    ▼
ykt_aisaas_async_job 表（status=queued）
    │
    ▼
RocketMQ delay queue
    │
    ▼
Worker 节点（PowerJob 实例）
    │
    ├─► 状态回调（progress 5%/10%/.../100%）
    │       │
    │       ▼
    │   WebSocket / Webhook 推送给客户端
    │
    └─► 完成：status=success，产物路径写入 MinIO
            │
            ▼
        计量 + 计费
```

### 9.3 GPU 资源管理（MVP）

MVP 阶段不做精细 GPU 调度，简单策略：
- Worker 节点绑定 GPU 卡（环境变量）
- 同时只跑 1 个任务（串行）
- 后续扩展：vGPU 切分 / 时间片轮转

---

## 10. 部署架构

### 10.1 MVP 单机部署（**同物理机：平台 + xiaozhi-server**）

> **v1.2 关键变化**：xiaozhi-server 与平台**必须同物理机**（localhost 调用 < 1ms 延迟），否则设备对话延迟不达标。

```
┌──────────────────────────────────────────────────────────────────────────┐
│  物理机 / VM（推荐：16C 32G + 200G SSD，含 GPU 用于 ASR/TTS 兜底）       │
│                                                                          │
│  ┌────────────────────┐  ┌────────────────────┐  ┌──────────────────┐  │
│  │ xiaozhi-server     │  │ ykt-aisaas         │  │ PowerJob Server  │  │
│  │ (瘦身后，无 AI 模块)│  │ :8190 API          │  │ :7700            │  │
│  │ :8091 API          │  │ :8191 Admin        │  └──────────────────┘  │
│  │ :8092 设备 WS      │  │  内部租户调用走    │                        │
│  │                    │──►│  localhost:8190    │  ┌──────────────────┐  │
│  │ 调用方式：         │  │  (< 1ms 延迟)      │  │ RocketMQ         │  │
│  │ HTTP / WS 内网调用 │  │  WebSocket 通路 B  │  │ :9876 / :10911   │  │
│  └────────────────────┘  └────────────────────┘  └──────────────────┘  │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │  Docker Compose（基础设施）                                         │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐           │ │
│  │  │ MySQL 8  │  │PostgreSQL│  │ Redis 7  │  │ MinIO    │           │ │
│  │  │ :3306    │  │+PgVector │  │ :6379    │  │ :9000    │           │ │
│  │  │          │  │ :5432    │  │          │  │ :9001    │           │ │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘           │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│  ┌──────────────────┐                                                    │
│  │ Nginx :80/:443   │  ← 反代 :8190/:8191/:8091/:8092                   │
│  └──────────────────┘                                                    │
└──────────────────────────────────────────────────────────────────────────┘
```

**为什么必须同物理机**：
- xiaozhi-server 与平台之间 WebSocket 通路 B 是高频小帧（20ms/帧 Opus）
- 跨网络即使同机房（< 5ms）也会引入额外延迟和抖动
- 同物理机走 localhost：网络延迟 ≈ 0，CPU 切换极快
- 简化运维（无内网配置、无防火墙、无 DNS）

**未来演进**（V2.0 集群化时）：
- 改为同 K8s 集群（Pod 间通信仍走虚拟网络）
- 或同机房专用内网（10GbE + SR-IOV）
- 届时需要重新评估延迟 SLA

**性能 SLA（设备对话全链路）**：
- ASR 首 token：< 500ms（VAD 后第一段音频识别）
- LLM 首 token：< 1.5s
- TTS 首音频：< 800ms
- **总链路 P95 < 3 秒**

### 10.2 演进到集群（量级上来后）

```
        Nginx LB / SLB
            │
   ┌────────┼────────┐
   ▼        ▼        ▼
 [API Pod][API Pod][API Pod]  ← K8s Deployment，水平扩容
   │
   ├─► MySQL 主从 / 读写分离
   ├─► PostgreSQL 主从 + PgVector 节点池
   ├─► Redis Cluster
   ├─► RocketMQ Dledger 集群
   ├─► MinIO Distributed
   └─► GPU Node Pool（克隆/微调 Worker）
```

---

## 11. 与 xiaozhi-server 的融合路径（**v1.1 重大调整：双轨→MVP 即融合**）

### 11.1 两阶段融合（**取代原三阶段**）

```
═══ 阶段 1（MVP，0-3 个月）═══
   平台开发 + xiaozhi-server 同步改造（并行）
   xiaozhi-server：
     · 保留设备协议层（WebSocketHandler/ChatSession/SessionManager）
     · AI 调用入口从 ChatModelFactory 改为 OpenAiChatModel（调平台）
     · TtsServiceFactory/SttServiceFactory 改为调平台 /v1/audio/*
     · XiaoZhiToolCallingManager 改为调平台 MCP
     · 保留 xiaozhi-ai 模块作为 fallback（feature flag 控制，1 个月观察期）

═══ 阶段 2（稳定期，3-4 个月）═══
   验证 1 个月稳定后：
     · 删除 xiaozhi-server 的 xiaozhi-ai 模块
     · 删除 xiaozhi-server 的 sys_config 中 LLM/TTS/STT 配置（已迁到平台）
     · xiaozhi-server 瘦身成纯设备协议层 + 设备/消息管理
     · 平台成为 ykt 工作区唯一 AI 平台
```

### 11.2 xiaozhi-server 改造详细方案（见 §12）

改造范围、数据迁移、代码删除清单详见 §12。

### 11.3 阶段 1 的具体改造（**关键**）

xiaozhi-server 侧（语言无关的 HTTP 调用替换）：
- LLM：OpenAI SDK `baseUrl = http://127.0.0.1:8190/v1` + 内部租户 Key
- TTS：`POST /v1/audio/speech`（chunked 音频流）
- ASR：`POST /v1/audio/transcriptions`（VAD 切段后逐段 multipart）
- RAG：`POST /api/v1/knowledge-bases/{id}/search`
- MCP：chat 请求带 `x-tools-mcp:true`，工具注册到平台
详见 MODULES.md 与 README「xiaozhi-server 接入映射」。

xiaozhi-server 一行代码改造，立即享受：
- 平台所有商用模型（gpt-4o / deepseek / glm-4 / qwen ...）
- 统一计量、统一审计
- 后续接入新能力（克隆/微调）免开发

---

## 12. xiaozhi-server 改造方案（**v1.1 新增**）

### 12.1 改造范围

| 模块/包 | 改造方式 | 改造后状态 |
|---|---|---|
| `xiaozhi-ai/llm/factory/` (ChatModelFactory + 7 个 Provider) | **删除**（迁移到平台） | ❌ 删除 |
| `xiaozhi-ai/tts/` (TtsServiceFactory + 8 个 Provider) | **删除**（迁移到平台） | ❌ 删除 |
| `xiaozhi-ai/stt/` (SttServiceFactory + 7 个 Provider) | **删除**（迁移到平台） | ❌ 删除 |
| `xiaozhi-ai/knowledge/` (RAG + Chroma 工厂) | **删除**（迁移到平台） | ❌ 删除 |
| `xiaozhi-ai/tool/` (MCP Client + Tool Registry) | **删除**（迁移到平台） | ❌ 删除 |
| `xiaozhi-dialogue/` (WebSocket 协议/SessionMgr/ChatSession) | **保留**，仅替换内部 AI 调用 | ✅ 保留 |
| `xiaozhi-dialogue/runtime/Persona` | **改造**，注入平台的 `OpenAiChatModel` 和 `TtsClient` | 🔧 改造 |
| `xiaozhi-dialogue/audio/vad` (VAD) | **保留**（VAD 是设备协议层逻辑，留在 xiaozhi） | ✅ 保留 |
| `xiaozhi-service/{device,user,role,message,config,template,permission,agent,mcpserver,firmware,music,knowledge,companion,security,storage}` | **保留**（业务管理） | ✅ 保留 |
| `xiaozhi-server/` (HTTP API 启动器) | **保留** | ✅ 保留 |
| `xiaozhi-common/` | **保留**（去掉对 xiaozhi-ai 的依赖） | 🔧 改造 |

### 12.2 数据迁移（DB）

#### 12.2.1 从 xiaozhi 库迁到平台库

| xiaozhi 表 | 平台表 | 迁移策略 |
|---|---|---|
| `sys_config` 中 `type IN ('llm','tts','stt','embedding')` 的记录 | `ykt_aisaas_model_registry`（系统级，tenantId=xiaozhi 内部租户 ID） | 一次性脚本迁移 |
| `sys_role`（AI 角色） | `ykt_aisaas_persona`（系统预置） | 一次性脚本迁移，prompt + voice 字段映射 |
| `sys_template`（提示词模板） | 合并进 `ykt_aisaas_persona.systemPrompt` | 一次性脚本迁移 |
| `sys_mcp_tool_exclude` | 平台 MCP 工具的租户禁用配置（`ykt_aisaas_tenant_mcp_binding.enabled=0`） | 一次性脚本迁移 |
| `sys_knowledge_base` / `_document` / `_intent` / `_intent_log` | `ykt_aisaas_knowledge_base` / `_document` + PgVector 表 | 一次性脚本迁移（含向量化重算或保留原向量） |

#### 12.2.2 保留在 xiaozhi 库（不迁移）

- `sys_device`（设备主表）
- `sys_message`（消息记录，但 tokens 字段不再使用，由平台计量）
- `sys_summary`（摘要）
- `sys_user` / `sys_auth_role` / `sys_permission`（管理后台用户）
- `sys_operation_log`（操作审计）
- `sys_code`（验证码）

#### 12.2.3 迁移时机

```
Week 6（MVP 中段）：写迁移脚本 + 灰度环境验证
Week 8（MVP 上线前）：生产环境迁移 + 数据校对
Week 8 后：xiaozhi-server 保留只读 sys_config 7 天（应急回滚）
Week 12：删除 xiaozhi 库中的 sys_config AI 配置部分
```

### 12.3 改造工作量（人天）

| 任务 | 工时 | 备注 |
|---|---|---|
| 移除 `xiaozhi-ai` 依赖，pom 调整 | 1 | - |
| `Persona` 改造（注入平台 Client） | 3 | ChatModel 替换最复杂 |
| `WebSocketHandler` 链路适配 | 2 | 主要保留 |
| `DialogService` / `MessageService` 去掉 token 统计（平台接管） | 1 | - |
| `AgentService` / `McpServerService` 改造（调平台 MCP） | 2 | - |
| `KnowledgeBase*Service` 改造（调平台 RAG） | 2 | - |
| 数据迁移脚本（Flyway） | 2 | - |
| 灰度环境联调 | 3 | 含压测 |
| 生产切换 + 1 个月观察期 | 5 | 含应急处理 |
| **小计** | **21 人天 ≈ 4 周** | 与平台并行 |

### 12.4 风险与应急

| 风险 | 应急 |
|---|---|
| 平台故障导致设备对话不可用 | xiaozhi-server 保留 fallback：feature flag `xiaozhi.ai.fallback=true` 时切回自研（保留 xiaozhi-ai 模块 1 个月） |
| 性能不达标（P95 > 3s） | 排查链路：VAD → 网络 → ASR → LLM 首字 → TTS 首音频；优先优化首字延迟 |
| 数据迁移丢失 | 灰度环境全量演练 + 生产迁移前备份 + 双写 7 天对账 |
| MCP 工具调用失败率高 | 平台 MCP 模块加 fallback（重试 + 超时降级） |
| 设备 Persona 与原 sys_role 行为不一致 | 迁移脚本 + 100 台设备灰度 + 用户问卷 |

### 12.5 内部超级租户配置

平台为 xiaozhi-server 创建特殊租户：

```sql
INSERT INTO ykt_aisaas_tenant (id, code, name, status, planId, remark)
VALUES (1, 'xiaozhi-internal', '小智 AI 内部租户', 1, NULL, '内部超级租户，无配额限制');

INSERT INTO ykt_aisaas_apikey (id, tenantId, name, apiKey, apiKeyHash, keyPrefix, scope, status, createdBy)
VALUES (1, 1, 'xiaozhi-server 内部调用', 'sk-aisaas-internal-xiaozhi-{random}', '{hash}', 'sk-aisaas', '["llm","tts","asr","rag","mcp","persona","voice_clone","finetune"]', 1, 1);

-- 内部租户特殊配置（不走配额扣减）
INSERT INTO ykt_aisaas_internal_tenant_config (tenantId, isUnlimited, rateLimitOverride)
VALUES (1, 1, '{"llmQps":500,"ttsConcurrent":1000}');
```

### 12.6 接口调用映射

| xiaozhi-server 原调用 | 改造后调用平台 |
|---|---|
| `chatModelFactory.getModel(cfg).call(prompt)` | 平台 `POST /v1/chat/completions` |
| `chatModelFactory.getModel(cfg).stream(prompt)` | 平台 `POST /v1/chat/completions` (stream=true) |
| `ttsServiceFactory.get(cfg).synthesize(text)` | 平台 `POST /v1/audio/speech` |
| `sttServiceFactory.get(cfg).recognize(audio)` | 平台 `POST /v1/audio/transcriptions` |
| `knowledgeRagService.search(query, kbId)` | 平台 `POST /api/v1/knowledge-bases/{id}/search` |
| `toolCallingManager.executeTools(...)` | chat `x-tools-mcp:true`（平台侧 5 轮工具循环） |

---

## 13. 关键技术决策（Trade-off）

| 决策点 | 选择 | 拒绝方案 | 理由 |
|---|---|---|---|
| 架构 | 模块化单体 | 微服务 | MVP 抢时间，模块边界清晰即可未来拆 |
| 多租户 | 字段隔离 | schema 隔离 / 独立 DB | 运维简单，足够支撑早期租户量 |
| AI 协议 | OpenAI 兼容 | 自定义协议 | 事实标准，商用→自建无缝切换 |
| 向量库 | PgVector | Milvus / Qdrant | 复用 PG 运维，MVP 够用，未来可平滑迁 |
| MQ | RocketMQ | Kafka / RabbitMQ | 长任务延迟队列原生支持，国产运维熟悉 |
| 任务调度 | PowerJob | XXL-Job | 支持工作流（克隆/微调多步骤） |
| **流式** | **SSE + WebSocket 单通路 B**（v1.2） | ~~双通路 A+B~~ | MVP 只做 B（xiaozhi 用），A 延后 V1.1 |
| **WebSocket 通路 A** | **延后到 V1.1**（v1.2） | ~~MVP 同时做~~ | 节省 1-2 周，外部租户 SSE 够用 |
| 计量 | 切面 + Redis 预扣 + MQ 异步落库 | 同步落库 | 性能优先，最终一致 |
| 前端 | Vue 3 + Element Plus | React / Ant Design | 与 ykt-admin/xiaozhi web 风格统一 |
| **xiaozhi-server 改造** | **MVP 即接入**（v1.1） | ~~双轨运行~~ | 避免维护两套 AI 调用栈，统一从首日开始 |
| **xiaozhi-ai 模块** | **保留 1 个月观察期后删除** | 立即删除 | 应急回滚需要，1 个月后确认稳定再删 |
| **xiaozhi-dialogue 复用方式** | **拷贝关键类**（v1.2） | ~~抽 SPI 共享~~ | 解耦优先，避免双向依赖 |
| **内部超级租户鉴权** | **`X-Internal-Token` Header**（v1.2） | ~~mTLS 双向证书~~ | MVP 简单，规模化后再升级 |
| **平台与 xiaozhi-server 部署** | **同物理机**（v1.2） | ~~同机房不同机~~ | localhost < 1ms 延迟，运维最简单 |

---

## 13. 风险清单与对策

| 风险 | 概率 | 影响 | 对策 |
|---|---|---|---|
| 多租户上下文在 Reactor 流式中丢失 | 高 | 数据串租户（**严重**） | §5.2 陷阱清单逐项验证 + 单元测试覆盖 |
| 商用 API 限流导致平台 5xx | 中 | 用户体验差 | Provider 层加 fallback（OpenAI→DeepSeek→智谱） |
| 计量与实际偏差 | 中 | 计费纠纷 | 每日对账任务 + 监控告警 |
| 长任务卡死 | 中 | GPU 占用 | PowerJob 心跳 + 超时强制失败 |
| 向量库膨胀 | 低 | 查询变慢 | 按租户分表 + 索引优化 + 定期归档 |
| 套餐滥用 | 中 | 成本失控 | Redis 实时配额 + 异步对账 + 异常告警 |
| API Key 泄露 | 中 | 责任事故 | 强制 HTTPS + Key 哈希存储 + 调用 IP 白名单（P1） |

---

## 14. 下一步

| 文档 | 状态 | 内容 |
|---|---|---|
| [DATABASE.md](./DATABASE.md) | v1.1 已更新 | 完整 DDL + 内部租户表 + xiaozhi 数据迁移章节 |
| [MODULES.md](./MODULES.md) | v1.1 已更新 | 新增 ai-realtime 模块 + xiaozhi 改造任务清单 |
| [API.md](./API.md) | v1.1 已更新 | WebSocket 升 P0 + 内部超级租户接口 |
| [OVERVIEW-NON-TECHNICAL.md](./OVERVIEW-NON-TECHNICAL.md) | v1.1 已更新 | 非研发版（含数据流/业务流图） |
| [CHANGELOG.md](./CHANGELOG.md) | v1.1 新增 | 本次修订点清单 |

---

**评审清单**（v1.2，请确认）：
- [ ] §1.1 平台定位（内外统一 AI 能力平台，xiaozhi 即内部租户）
- [ ] §2.1 部署架构（平台 + xiaozhi-server **同物理机**）
- [ ] §3 技术栈版本（Go 1.23 / Gin / GORM / pgx，见 GOLANG-IMPLEMENTATION.md）
- [ ] §5 多租户字段隔离 + Reactor Context 传播方案
- [ ] §6 OpenAI 兼容协议作为唯一 AI 接入协议
- [ ] §7.1 WebSocket 只做通路 B（v1.2），通路 A 延后 V1.1
- [ ] §10 单机部署形态（含性能 SLA：全链路 P95 < 3s）
- [ ] §11 xiaozhi-server 两阶段融合路径
- [ ] §12 xiaozhi-server 改造方案（21 人天，与平台并行 4 周）
- [ ] §13 关键技术决策（v1.2：拷贝 dialogue、X-Internal-Token、同物理机）
