# YKT AI SaaS 平台 · 架构设计（技术版）

> 版本：v0.1-snapshot（2026-08-24，与代码 commit 同步）
> 状态：**MVP 已实现可运行**（本文描述的是已落地的系统，非规划）
> 代码：`ykt-aisaas/`（Go，49 文件核心代码，含 12 项 e2e 验证通过）

---

## 1. 系统定位与边界

### 1.1 三系统格局

| 系统 | 职责 | 语言 | 部署 | 端口 |
|---|---|---|---|---|
| **ykt-aisaas**（本平台） | **内外统一 AI 能力平台**：多租户 SaaS + xiaozhi-server 的 AI 后端 | Go 1.23 | 独立进程 | 8190 |
| xiaozhi-server | 设备协议层 + 设备/消息管理（**AI 能力全部改走本平台**） | Java 21 | 同物理机 | 8091/8092 |
| ykt-admin | 内部运维管控（旁路监控，不耦合平台业务） | Java→Go 迁移中 | 独立 | 8090 |

### 1.2 平台能力清单（全部已实现 ✅）

| # | 能力 | 接口 | 验证状态 |
|---|---|---|---|
| 1 | LLM 对话 | `POST /v1/chat/completions`（流式 SSE/非流式） | ✅ e2e |
| 2 | TTS 语音合成 | `POST /v1/audio/speech`（chunked 流式 + x-emotion） | ✅ e2e（WAV 回传） |
| 3 | ASR 语音识别 | `POST /v1/audio/transcriptions`（multipart + 情绪透传） | ✅ e2e |
| 4 | 模型列表 | `GET /v1/models` | ✅ |
| 5 | RAG 知识库 | 建库/上传/异步向量化/检索 + chat `x-knowledge-base-ids` 自动注入 | ✅ e2e（citations 事件） |
| 6 | MCP 工具调用 | 工具注册（http/builtin）/绑定/直调 + chat `x-tools-mcp` 5 轮循环 | ✅ e2e（tool-call 事件） |
| 7 | 多租户 | 字段级隔离（GORM 插件 + context） | ✅ 5 项隔离测试 |
| 8 | API Key 鉴权 | sha256 哈希存储/Redis 缓存/scope/IP 白名单 | ✅ |
| 9 | 配额 | Redis Lua 原子预扣 + DB 自动加载 | ✅（超额 402 拒止） |
| 10 | 计量计费 | 多维计量/按模型价格扣余额/流水/用量查询/充值 | ✅（分单位精确扣减） |

待实现：WebSocket 实时流（通路 B，xiaozhi 设备直连）、在线支付、月账单 cron、rerank、DashScope 原生适配器。

### 1.3 红线

- 平台 DB 与 xiaozhi/ykt-admin **完全隔离**，禁止跨库写
- 租户上下文缺失 → **fail-fast**（错误码 50001），绝不静默放行
- API Key 明文不入库（仅创建时一次性返回）

---

## 2. 整体架构

### 2.1 请求链路（chat 完整示例）

```
POST /v1/chat/completions
  │
  ├─ RequestID 中间件（X-Request-Id 生成/透传）
  ├─ auth.Middleware：API Key sha256 → Redis/DB → scope/IP/过期/租户状态校验
  │    └─ tenant.With(ctx, tenantID)   ← 全平台唯一租户注入点
  ├─ ChatHandler
  │    ├─ Registry.ResolveFor(modelID, "chat")：租户私有 > 全局模型，AES-GCM 解密上游密钥
  │    ├─ quota.PrecheckDim：Redis Lua 原子预扣（估算输入 token）
  │    ├─ [RAG] Retriever.RetrieveForChat → 检索 top5 → system prompt 注入
  │    ├─ [MCP] Tools.ListTools 装载租户工具（x-tools-mcp 时）
  │    ├─ 路径 A 无工具 → openaiclient 直连上游（流式 SSE 透传）
  │    └─ 路径 B 带工具 → Complete→tool_calls→Execute→role:tool 回填→再调（≤5 轮）
  │         流式输出：x-tool-call 事件 → 合成 chunk → x-metering → [DONE]
  └─ metering.RecordWithCtx → Redis INCR（实时余量）→ 批量 flush：
       usage_detail（含 costCents）+ 按价扣余额 + 流水
```

### 2.2 数据流

```
租户应用 ──API Key──► 平台 :8190 ──OpenAI 协议──► 上游推理层
                                                    ├─ 商用：DeepSeek/智谱/DashScope
                                                    └─ 自建：vllm/cosyvoice/FunASR
       平台存储：
       MySQL（业务/计量/计费）─ 用户库
       Redis（鉴权缓存/配额）─ 用户库
       PgVector（RAG 向量）  ─ 用户库（动态表 per 租户×知识库）
```

上游切换 = `seedmodel` 改一条 baseUrl 记录，**业务零改动**。

### 2.3 部署拓扑（同物理机）

```
┌────────────────────────── 物理机 16C32G ──────────────────────────┐
│  xiaozhi-server(:8091/8092) ⇄ ykt-aisaas(:8190)   localhost <1ms │
│  Docker：MySQL(:13306) Redis(:16379) PgVector(:15432)            │
│  Nginx :80/:443 反代                                              │
└───────────────────────────────────────────────────────────────────┘
```

产物：单二进制 ~30MB（Docker 多阶段构建），常驻内存 ~200MB。

---

## 3. 技术栈

| 层 | 选型 | 说明 |
|---|---|---|
| HTTP | Gin 1.10 | 中间件模型 + SSE 共存 |
| ORM | GORM 1.25 + **自研 TenantPlugin** | QUD 自动 WHERE tenantId / Create 填充 |
| 向量 | pgx/v5 + pgvector-go | 动态表，cosine 检索（小数据顺序扫，上量后 ivfflat） |
| LLM 客户端 | **自研 openaiclient（~500 行）** | SSE 半行保护/usage 尾包/tool_calls |
| 鉴权 | golang-jwt/v5 + 自研 ApiKey 中间件 | |
| 配额 | go-redis + Lua | 原子预扣防超卖 |
| 迁移 | golang-migrate | 000001-000005 |
| 日志 | slog（标准库 JSON） | |
| 测试 | sqlite 内存库 + httptest | 隔离/SSE/chunker 三套件 |

**架构原则**：框架只做路由和连接；多租户/计量/计费/模块边界全部自研（`internal/platform`）。

---

## 4. 核心机制

### 4.1 多租户（唯一机制：context 传播）

```go
tenant.With(ctx, tid)          // auth 中间件 / 异步任务入口注入
GORM TenantPlugin              // 从 ctx 取租户 → 拼 WHERE / 填充
tenant.From(ctx)               // 缺失即 panic（fail-fast）
```

异步任务（RAG ingest goroutine）入口必须显式 `tenant.With`——已在开发中踩坑验证（错误码 50001 当场暴露而非串数据）。

**隔离矩阵**：DB 字段级（自动 WHERE）/ Redis Key 前缀 / PgVector 独立表 / API Key 绑定租户 / 计量按租户。

### 4.2 模型路由（Registry）

`model_registry` 表：`tenantId IS NULL` 为全局模型，非 NULL 为租户私有（如微调产物）。解析优先级 私有 > 全局，进程内缓存 5min，上游密钥 AES-256-GCM 加密存储。

### 4.3 计量计费（两段式，无 AOP）

```
预检：quota.PrecheckDim → Redis Lua（INCRBY vs limit，防超卖）
实记：metering.Record → channel → 批量 flush（200条/3s）
      → usage_detail（costCents 按模型价浮点计算）
      → 按批聚合扣余额（浮点累计后取整，0.1分级费用不截断）→ 流水
对账：Redis 计数 vs DB 聚合（cron 待实现）
```

限额加载：QuotaLoader 启动全量 + 每小时刷新 DB→Redis。

### 4.4 OpenAI 协议兼容（含平台扩展）

请求扩展字段：`x-knowledge-base-ids`（RAG）/ `x-tools-mcp`（工具）/ `x-emotion`（TTS 情绪）/ `x-device-id`。
SSE 扩展事件：`x-metering`（用量）/ `x-rag-citations`（引用）/ `x-tool-call`（工具执行）。
OpenAI SDK（任意语言）改 `base_url` 即可接入。

---

## 5. xiaozhi-server 集成（已就绪）

| xiaozhi 原调用 | 替换为 | 状态 |
|---|---|---|
| ChatModelFactory | `/v1/chat/completions`（OpenAI SDK 指向平台） | ✅ |
| TtsServiceFactory | `/v1/audio/speech` | ✅ |
| SttServiceFactory | `/v1/audio/transcriptions` | ✅ |
| knowledgeRagService | `/api/v1/knowledge-bases/{id}/search` + chat 注入 | ✅ |
| XiaoZhiToolCallingManager | chat `x-tools-mcp:true` | ✅ |
| 设备 WebSocket 直连 | 通路 B（待实现，Phase 3） | 📅 |

内部超级租户：tenantId=1，`X-Internal-Token` + loopback，不限配额。

---

## 6. 质量与运维

| 项 | 现状 |
|---|---|
| 测试 | 租户隔离 5 项 + SSE 解析 + chunker 4 项（`go test` 全绿） |
| e2e | 12 项验证记录（chat/TTS/ASR/RAG/MCP/计费/隔离/超额） |
| 日志 | slog JSON（requestId 贯穿） |
| 优雅停机 | 信号量 → 10s 排空 |
| 监控 | /healthz；Prometheus 指标待接 |
| 回滚 | xiaozhi-server 侧 feature flag（改造方案内置） |

---

## 7. 演进路线

| 阶段 | 内容 |
|---|---|
| 当前（v0.1） | 上述全部能力，mock 上游验证 |
| 接下来 | 真实上游接入（DeepSeek/CosyVoice/FunASR，seedmodel 一条命令）+ xiaozhi-server 切换 |
| Phase 3（触发器制） | WebSocket 通路 B（设备实时流）、在线支付、月账单 |
| 规模化 | K8s 多副本（租户上下文无状态，天然水平扩展）、PgVector hnsw 索引、asynq 任务队列 |
