# YKT AI SaaS 平台 · 模块详细设计（Go 实现）

> 版本：**v2.2（2026-08-24）** — 与代码实现对齐（Java 版设计已于 v2.0-GO 废弃）
> 状态：**已实现 v0.1 可运行版本**
> 关联：[ARCHITECTURE.md](./ARCHITECTURE.md) · [DATABASE.md](./DATABASE.md) · [API.md](./API.md) · [GOLANG-IMPLEMENTATION.md](./GOLANG-IMPLEMENTATION.md)
>
> 本文档描述 `ykt-aisaas/` 的**实际代码结构**。历史 Java/Maven 设计见 [CHANGELOG.md](./CHANGELOG.md) v1.x 记录。

---

## 0. 设计原则

### 0.1 命名约定

- **Go module**：`ykt.dev/aisaas`
- **包根**：`ykt.dev/aisaas/{internal,pkg,cmd,tools}`
- **表前缀**：`ykt_aisaas_*`，字段 camelCase（GORM `NoLowerCase` + 显式 column tag）
- **ID**：全局 snowflake（`platform/ids`，进程内单例防撞号）

### 0.2 模块边界规则

1. `internal/platform` 在最底层，**不 import 任何业务包**（errs/config/tenant/database/redisx/auth/quota/metering/web/ids/crypto）
2. 业务包 `llm/tts/asr/rag/mcp/billing/tenantm` **互不 import**；跨模块协作通过接口注入（如 chat 的 `Retriever`/`ToolSource`）+ 在 `server`（装配方）组合
3. 业务包对外只暴露接口 + DTO，不暴露 GORM 模型（`apikey.DO` 例外，同包 handler 直用）
4. `pkg/openaiclient` 零业务依赖，可独立复用

---

## 1. 目录总览（已实现 ✅ = v0.1 已含）

```
ykt-aisaas/
├── cmd/aisaas/main.go              ✅ 装配：config→db→redis→pg→各服务→router→优雅停机
├── internal/
│   ├── platform/                   ⭐ 框架层（所有业务包依赖）
│   │   ├── errs/                   ✅ 错误码枚举 + Error + HTTP 映射（API.md §8 对齐）
│   │   ├── config/                 ✅ viper（yaml + AISAA_* env 覆盖）
│   │   ├── tenant/                 ✅ context key + With/From/FromSafe（唯一租户传播机制）
│   │   ├── database/               ✅ GORM 装配 + BaseDO + TenantPlugin（QUD 自动 WHERE / Create 填充 / 缺 ctx fail-fast）
│   │   ├── redisx/                 ✅ 连接 + Key 规范 + 配额预扣 Lua（原子防超卖）
│   │   ├── auth/                   ✅ ApiKey 校验（sha256 + Redis 缓存 + postCheck 后缓存）+ APIKey/InternalToken 中间件 + scope/IP 白名单
│   │   ├── quota/                  ✅ Guard（PrecheckDim 多维度预扣；Redis 故障放行，事后对账）
│   │   ├── metering/               ✅ Recorder（channel + 批量 flush → usage_detail；Pricer 计价浮点累计；Consumer 扣费回调）
│   │   ├── web/                    ✅ gin 装配/Recovery/CORS/RequestID + Result 信封 + AbortOpenAI + SSEWriter（心跳/x- 事件/[DONE]）
│   │   ├── ids/                    ✅ snowflake 全局单例
│   │   └── crypto/                 ✅ API Key 生成/SHA-256 + AES-256-GCM（model_registry 密钥加密）
│   ├── llm/                        ✅ Registry（模型路由：租户私有>全局，缓存 5min，ResolveFor 按 type）+ ChatService
│   ├── tts/                        ✅ Service（OpenAI 兼容透传 + x-emotion）
│   ├── asr/                        ✅ Service（multipart 透传 + duration/emotion + 秒数估算）
│   ├── rag/                        ✅ chunker（rune 滑窗中文友好）/ store（PgVector 动态表）/ kb（元数据）/ ingest（异步向量化管道）/ retrieve（检索 + BuildContext）
│   ├── mcp/                        ✅ Repo/Service（http+builtin 工具执行器 + 租户可见性 + 绑定）
│   ├── billing/                    ✅ QuotaLoader（DB→Redis 启动+每小时）+ Service（余额/充值/流水/overview）
│   ├── tenantm/apikey/             ✅ DO/Repo/Service/Handler（创建返回明文 Key 仅一次）
│   └── server/                     ✅ 路由装配
│       ├── router.go               v1 + api/v1 + internal/api/v1 三组
│       ├── v1/                     ✅ chat.go（流式/非流式/RAG 注入/工具循环）+ audio.go（speech/transcriptions）
│       ├── apiv1/                  ✅ apikey.go + kb.go + mcp.go + billing.go
│       └── internalapi/            ✅ internalapi.go（签发 Key）+ recharge.go（充值）
├── pkg/openaiclient/               ✅ Chat（Complete/Stream SSE 解析含半行保护）+ Embed + Speech + Transcribe
├── tools/
│   ├── mockupstream/               ✅ OpenAI 兼容 mock（chat 流式/tool_calls/embeddings/audio/speech/echo）
│   └── seedmodel/                  ✅ 模型注册 seed（AES 加密 -type chat/tts/asr/embedding）
├── migrations/                     ✅ 000001 init / 000002 seed / 000003 knowledge / 000004 mcp / 000005 billing
├── deploy/docker-compose-deps.yml  ✅ MySQL:13306 / Redis:16379 / PgVector:15432（端口错开宿主）
├── config.yaml / Makefile / Dockerfile / README.md
└── docs/                           设计文档（本目录）
```

---

## 2. 请求链路（chat 完整示例）

```
POST /v1/chat/completions
  │
  ├─ web.RequestIDMiddleware
  ├─ auth.Middleware(svc, "")            → sha256 查缓存/DB → scope/IP 校验 → tenant.With(ctx)
  ├─ ChatHandler.Completions
  │   ├─ body 解析（含 x-knowledge-base-ids / x-tools-mcp 扩展）
  │   ├─ registry.Resolve(modelID)       → 租户私有>全局 + AES 解密上游密钥
  │   ├─ quota.PrecheckDim(llm_tokens_in, 估算)  → Redis Lua 预扣
  │   ├─ Retriever.RetrieveForChat       → RAG 检索 + system prompt 注入（可选）
  │   ├─ Tools.ListTools                 → MCP 工具装载（x-tools-mcp 时）
  │   ├─ 路径 A：无工具 → 直接流式/非流式透传
  │   │   └─ 流式：SSE 透传 chunk → x-metering 事件 → [DONE]
  │   └─ 路径 B：带工具 → runToolLoop（Complete→tool_calls→Execute→role:tool 回填→再调，≤5 轮）
  │              → 流式：x-tool-call 事件 + 合成 chunk；非流式：x-tool-calls 字段
  └─ metering.RecordWithCtx ×N           → Redis INCR + 批量落库 + 计价扣费
```

---

## 3. 关键接口契约（跨模块协作点）

```go
// server/v1 声明，rag.Retriever / mcp.Service 实现（接口隔离，业务包互不依赖）
type Retriever interface {
    RetrieveForChat(ctx, kbIDs []int64, query string, topK int) []rag.Citation
    BuildContext(cits []rag.Citation) string
}
type ToolSource interface {
    ListTools(ctx) []openaiclient.Tool
    Execute(ctx, name string, args json.RawMessage) mcp.ToolCallEvent
}

// metering 声明，llm.Registry / billing.Service 实现
type Pricer interface{ Price(modelID string) (in, out float64) }
type Consumer interface{ Consume(ctx, tenantID int64, bizType string, costCents int64, refID string) }

// main.go 装配时 Bind：
meter.BindPricer(registry); meter.BindConsumer(billingSvc)
ragIngest.BindStore(func(ctx, tid, kbID, docID, chunks, vecs) error { ... })  // 打破 Service↔Ingestor 循环
```

---

## 4. 多租户机制（context 唯一真相）

| 场景 | 传播方式 |
|---|---|
| HTTP 请求 | `auth.Middleware` → `tenant.With(c.Request.Context(), bo.TenantID)` |
| GORM 查询 | `db.WithContext(ctx)` → TenantPlugin 从 ctx 取租户拼 WHERE / 填充 Create |
| 异步任务（rag ingest） | goroutine 入口显式 `tenant.With(context.Background(), tid)`（已在 ingest 踩坑修复） |
| 计量落库 | Record 自带 TenantID 字段（批量写不走插件） |
| 内部接口 | `InternalToken` 中间件注入 tenantId=1 + Unlimited |

**红线**：租户上下文缺失 → `50001 TenantContextLost` fail-fast，绝不静默放行（隔离测试套件覆盖）。

---

## 5. 测试与验证

| 套件 | 覆盖 |
|---|---|
| `database/tenant_plugin_test.go` | 5 项：查询隔离/无 ctx 拒绝/Create 强制填充/跨租户 Update-Delete 零命中/豁免表 |
| `openaiclient/client_test.go` | SSE 半行拼接/usage 尾包/[DONE]/上游错误透传 |
| `rag/chunker_test.go` | 滑窗/段落优先/重叠正确性/空输入 |
| e2e（已执行） | chat 流非流/TTS WAV/ASR 回传/RAG 检索+chat 注入/MCP 循环/计费扣费/超额拒止/租户隔离 |

---

## 6. 待实现（backlog）

| 项 | 说明 |
|---|---|
| realtime 通路 B | WebSocket xiaozhi 协议 + VAD/Opus + pipeline + Barge-in（Phase 3） |
| 在线充值 | 支付宝/微信（当前管理端记账） |
| 月账单 cron | bill 表已建，定时汇总 |
| rerank | 检索重排 |
| DashScope 原生适配器 | 解锁 CosyVoice 情绪标签/克隆训练 API |
| 平台运营后台 | /admin/api/v1（当前用 internal 接口 + DB 直查代替） |
