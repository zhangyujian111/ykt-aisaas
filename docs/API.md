# YKT AI SaaS 平台 · API 规范

> 版本：**v2.2（2026-08-24）** — 协议与语言无关；平台实现为 Go，示例已去除平台侧 Java 依赖
> 状态：MVP 基线 · 待评审
> 关联：[ARCHITECTURE.md](./ARCHITECTURE.md) · [MODULES.md](./MODULES.md) · [CHANGELOG.md](./CHANGELOG.md)

---

## 0. 总览

平台对外暴露 **四类 API**（**v1.2：WebSocket 仅保留通路 B**）：

| 类别 | 路径前缀 | 鉴权 | 客户端 | 用途 | MVP |
|---|---|---|---|---|---|
| **OpenAI 兼容接口** | `/v1/*` | API Key（Bearer） | 任何 OpenAI SDK | 直接替换 OpenAI 调用 | ✅ |
| **WebSocket 实时流（通路 B）** | `/internal/xiaozhi/v1/realtime` | `X-Internal-Token` Header + IP 白名单 | xiaozhi-server | 保留 xiaozhi 设备协议，平台透明转发 AI 调用 | ✅ |
| ~~WebSocket 实时流（通路 A）~~ | ~~`/v1/realtime`~~ | ~~API Key（URL 参数）~~ | ~~第三方实时对话~~ | ~~OpenAI Realtime API 兼容~~ | **❌ 延后 V1.1** |
| **SaaS 租户接口** | `/api/v1/*` | Sa-Token（成员登录）+ API Key 双支持 | 浏览器/SDK | 管理租户资源（Key/模型/账单/KB/...） | ✅ |
| **内部超级租户接口** | `/internal/api/v1/*` | `X-Internal-Token` Header | xiaozhi-server | 设备会话注册、数据迁移、实时统计 | ✅ |
| **运营后台接口** | `/admin/api/v1/*` | Sa-Token（运营登录） | 平台运营人员 | 平台管理 | ✅ |

**MVP 阶段外部租户的"实时对话"**：
- 用 **SSE**（LLM 流式响应）+ **HTTP Chunked**（TTS 音频流）+ **POST**（ASR 上传）
- 不需要 WebSocket 双向流（通路 A 延后到 V1.1）
- 大多数 ChatGPT-like 场景 SSE 已足够

**MVP 默认地址**：
- 生产：`https://aisaas.ykt.com`
- 调试：`http://localhost:8190`
- 文档：`http://localhost:8190/doc.html`（Knife4j）

---

## 1. 通用约定

### 1.1 HTTP 方法语义

| 方法 | 用途 | 示例 |
|---|---|---|
| `GET` | 查询、列表、流式（带 `Accept: text/event-stream`） | `GET /v1/models` |
| `POST` | 创建、动作（OpenAI 协议动作均用 POST） | `POST /v1/chat/completions` |
| `PUT` | 全量更新 | `PUT /api/v1/apikeys/{id}` |
| `PATCH` | 部分更新 | `PATCH /api/v1/apikeys/{id}/status` |
| `DELETE` | 删除（软删） | `DELETE /api/v1/apikeys/{id}` |

### 1.2 通用 Headers

```
Authorization: Bearer {apiKey 或 saToken}     # 必填
Content-Type: application/json
X-Request-Id: {client-uuid}                    # 可选，链路追踪
Accept-Language: zh-CN                         # 国际化预留
User-Agent: {client-info}
```

### 1.3 统一响应格式

**成功**（同步业务接口）：
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req-abc123",
  "data": { ... }
}
```

**失败**：
```json
{
  "code": 40201,
  "message": "配额不足，本月 LLM 输入 Token 已用尽",
  "requestId": "req-abc123",
  "data": null,
  "details": {
    "dimension": "llm_tokens_in",
    "used": 1000000,
    "limit": 1000000,
    "resetAt": "2026-09-01T00:00:00+08:00"
  }
}
```

**注意：OpenAI 兼容接口** `data: ` 直接返回 OpenAI 格式，**不包装 Result**。错误时也按 OpenAI 协议：
```json
{
  "error": {
    "message": "配额不足",
    "type": "insufficient_quota",
    "code": "insufficient_quota",
    "requestId": "req-abc123"
  }
}
```

### 1.4 分页

```
GET /api/v1/conversations?page=1&size=20&keyword=xxx&sort=createTime,desc
```

响应：
```json
{
  "code": 0,
  "data": {
    "list": [...],
    "total": 158,
    "page": 1,
    "size": 20,
    "totalPages": 8
  }
}
```

### 1.5 时间格式

- 入参/出参：ISO 8601 字符串 `2026-08-12T10:30:00+08:00`
- 日期：`2026-08-12`
- 时间戳（毫秒）：仅在性能敏感场景使用

### 1.6 命名约定

- URL 路径：**小写下划线**（`/v1/audio/speech`，与 OpenAI 一致） + **camelCase**（SaaS 自有 `/api/v1/apikeys/...`）
- 字段：**camelCase**（与 DB 字段一致）
- 枚举值：**snake_case**（`queued` / `running`）

### 1.7 限流

| 限制维度 | 默认值 | Header 提示 |
|---|---|---|
| 单 API Key QPM | 60（标准套餐）/ 600（企业） | `X-RateLimit-Limit` / `-Remaining` / `-Reset` |
| 单租户 QPM | 600 | 同上 |
| 单租户并发流式 | 50 | `X-Concurrency-Limit` |
| 平台总 QPS | 5000 | 超出返回 429 |

**429 响应**：
```json
{
  "code": 40203,
  "message": "请求过于频繁，请稍后再试",
  "details": { "retryAfterSeconds": 5 }
}
```

---

## 2. 鉴权

### 2.1 三种鉴权方式

| 方式 | Header | 适用 |
|---|---|---|
| **API Key**（程序调用） | `Authorization: Bearer sk-aisaas-xxxxx` | `/v1/*` + `/api/v1/*` 程序访问 |
| **Sa-Token**（成员登录） | `Authorization: Bearer {saToken}` + Cookie | `/api/v1/*` 浏览器登录 |
| **运营 Token** | `Authorization: Bearer {saToken}` | `/admin/api/v1/*` |

### 2.2 API Key 格式

```
sk-aisaas-{32个十六进制字符}
```

- 例：`sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b`
- 显示前缀：`sk-aisaas-9f3c5e2a`（前 16 位，便于 UI 识别）
- 存储：SHA-256 哈希入库，原文不入库（创建时一次性返回）

### 2.3 获取 Sa-Token（成员登录）

```
POST /api/v1/auth/login
{
  "username": "henry",
  "password": "{明文，传输走 HTTPS}",
  "tenantCode": "acme"                       # 可选，从 URL 子域或 Header 自动获取
}

→
{
  "code": 0,
  "data": {
    "tokenName": "satoken",
    "tokenValue": "eyJhbGc...",
    "memberId": 1001,
    "tenantId": 100,
    "tenantName": "Acme Inc.",
    "expiresIn": 7200,
    "roles": ["tenant_admin"]
  }
}
```

### 2.4 鉴权流程

```
请求到达
   │
   ▼
ApiKeyAuthFilter
   │
   ├─ 路径在 /v1/* 或带 Bearer sk-aisaas-  → 走 API Key 鉴权
   │       │
   │       └─► ApiKeyService.validate(apiKey)
   │              │
   │              ├─ 查 Redis 缓存（hash → ApiKeyBO）
   │              ├─ 校验：状态正常 / 未过期 / IP 白名单
   │              └─ 注入 TenantContext + LoginUser
   │
   ├─ 路径在 /api/v1/* 或 /admin/api/v1/*   → 走 Sa-Token 鉴权
   │       │
   │       └─► StpUtil.checkLogin()
   │              │
   │              └─► 从 Sa-Token session 取 LoginUser（含 tenantId/permissions）
   │
   └─ 都没有                                  → 401 Unauthorized
```

---

## 3. OpenAI 兼容接口（/v1/*）

> **设计原则**：与 OpenAI 官方 API 协议 100% 一致，OpenAI SDK 可零代码切换 `baseURL` 接入。

### 3.1 `POST /v1/chat/completions`（LLM 对话）

**完全兼容 OpenAI 协议**，扩展字段加 `x-` 前缀。

#### 请求

```json
{
  "model": "gpt-4o-mini",                     // 必填，可以是租户启用的任何模型ID
  "messages": [
    {"role": "system", "content": "你是一个有帮助的助手"},
    {"role": "user", "content": "你好"}
  ],
  "stream": true,                             // 流式
  "temperature": 0.7,
  "max_tokens": 1000,
  "tools": [                                  // Function Calling
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "查询天气",
        "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
      }
    }
  ],
  
  // ⭐ 平台扩展字段（前缀 x-）
  "x-conversation-id": 12345,                 // 复用已有会话；不传则新建
  "x-persona-id": 88,                         // 使用 Persona
  "x-knowledge-base-ids": [1, 2],             // RAG 检索（自动注入）
  "x-tools-mcp": true,                        // 自动加载租户绑定的 MCP 工具
  "x-emotion-target": "warm",                 // 期望情绪（影响 prompt + 后续 TTS）
  "x-user-id": "user-123",                    // 终端用户标识（用于配额分配/审计）
  "x-metadata": {"device": "xiaozhi-v2"},      // 自定义元数据
  
  // ⭐ v1.1 新增：xiaozhi-server 内部调用专用字段
  "x-device-id": "device-abc-001",            // 设备 ID（内部租户必填）
  "x-device-session-id": 99001,               // 平台 device_session 表 ID（首次接入时分配）
  "x-internal-call": true,                    // 标识内部超级租户调用（跳过配额扣减）
  "x-barge-in-supported": true                // 客户端支持打断（影响 TTS 调度策略）
}
```

#### 响应（非流式）

完全兼容 OpenAI：
```json
{
  "id": "chatcmpl-abc",
  "object": "chat.completion",
  "created": 1723430400,
  "model": "gpt-4o-mini",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "你好！很高兴为你服务。"},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 20, "completion_tokens": 12, "total_tokens": 32}
}
```

#### 响应（流式 `stream: true`）

```
data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"你"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

event: x-metering
data: {"inputTokens":20,"outputTokens":12,"costCents":5}

event: x-quota
data: {"dimension":"llm_tokens_out","remaining":99988,"limit":100000}

data: [DONE]
```

#### 平台扩展事件（前缀 `x-`）

| event | 触发时机 | 内容 |
|---|---|---|
| `x-metering` | 流式结束 | 本次消耗（token、成本） |
| `x-quota` | 流式结束 | 剩余配额 |
| `x-tool-call` | 触发工具调用时 | 工具名/参数 |
| `x-tool-result` | 工具返回时 | 工具结果 |
| `x-rag-citations` | RAG 命中时 | 引用文档片段 |
| `x-emotion` | LLM 输出情绪 tag | 当前情绪 |
| `x-error` | 流式中错误 | 错误信息（流不中断） |

### 3.2 `POST /v1/embeddings`（文本向量化）

```json
{
  "model": "text-embedding-v3",
  "input": ["你好", "今天天气真好"],
  "encoding_format": "float",
  "dimensions": 1024                            // 部分模型支持
}
```

响应：
```json
{
  "object": "list",
  "data": [
    {"object": "embedding", "index": 0, "embedding": [0.0123, -0.0456, ...]},
    {"object": "embedding", "index": 1, "embedding": [0.0789, -0.0123, ...]}
  ],
  "model": "text-embedding-v3",
  "usage": {"prompt_tokens": 12, "total_tokens": 12}
}
```

### 3.3 `POST /v1/audio/speech`（TTS）

```json
{
  "model": "cosyvoice-v2",
  "input": "你好，今天天气真好",
  "voice": "longxiaoxia",                      // 上游声音ID
  "response_format": "mp3",                    // mp3/opus/wav/flac
  "speed": 1.0,
  "pitch": 1.0,
  
  "x-emotion": "happy",                        // ⭐ 扩展：情绪
  "x-persona-id": 88,                          // ⭐ 扩展：Persona 自动选 voice
  "x-stream": true                             // ⭐ 扩展：流式（HTTP chunked）
}
```

响应：
- 非流式：`audio/mp3` 二进制
- 流式：`application/octet-stream` chunked，每 chunk 一段 PCM/Opus

### 3.4 `POST /v1/audio/transcriptions`（ASR）

multipart/form-data：
```
file: (binary) audio.wav
model: paraformer-v2
language: zh
prompt: "前几句话的文本（提高准确率）"
response_format: json                          // json/text/srt/vtt
x-stream: false                                // 流式（WebSocket 推荐用 /v1/audio/transcriptions/stream）
```

响应：
```json
{
  "text": "你好，今天天气真好",
  "x-emotion": {
    "type": "happy",
    "score": 0.85
  },
  "x-segments": [
    {"start": 0.0, "end": 1.5, "text": "你好"}
  ]
}
```

### 3.5 `POST /v1/audio/transcriptions/stream`（ASR 流式，平台扩展）

**HTTP Chunked Upload + SSE Response**：
- 请求：客户端把音频流 chunked 上传（每 chunk 100ms）
- 响应：流式输出识别结果

或更推荐用 **WebSocket**（见 §5）。

### 3.6 `GET /v1/models`（列出可用模型）

```
GET /v1/models?type=chat&enabled=true
```

响应：
```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "created": 1720000000,
      "owned_by": "openai",
      "x-type": "chat",
      "x-modality": ["text", "vision", "function"],
      "x-context-length": 128000,
      "x-price-input-cents": 0.105,
      "x-price-output-cents": 0.42,
      "x-default": true
    }
  ]
}
```

### 3.7 兼容性矩阵

| OpenAI SDK | 是否兼容 |
|---|---|
| `openai-python` | ✅ |
| `openai-node` | ✅ |
| `openai-go` | ✅ |
| LangChain (任何语言) OpenAI wrapper | ✅ |
| Java OpenAI SDK（任意实现） | ✅（**xiaozhi-server 用此方式接入**） |

---

## 4. SaaS 租户接口（/api/v1/*）

### 4.1 模块路由总览

| 模块 | 路径 |
|---|---|
| 鉴权 | `/api/v1/auth/*` |
| 当前用户 | `/api/v1/me` |
| API Key 管理 | `/api/v1/apikeys` |
| 模型配置 | `/api/v1/models` |
| 会话 | `/api/v1/conversations` |
| 知识库 | `/api/v1/knowledge-bases` |
| Persona | `/api/v1/personas` |
| 声音库 | `/api/v1/voices` |
| MCP 工具 | `/api/v1/mcp` |
| 长任务 | `/api/v1/jobs` |
| 计费 | `/api/v1/billing` |
| 用量 | `/api/v1/usage` |
| 成员管理 | `/api/v1/members` |
| 租户设置 | `/api/v1/tenant` |

### 4.2 鉴权 `/api/v1/auth/*`

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/login` | 登录（用户名+密码） |
| POST | `/auth/logout` | 登出 |
| POST | `/auth/refresh` | 刷新 Token |
| GET | `/auth/captcha` | 获取验证码（登录页用） |
| POST | `/auth/forgot-password` | 忘记密码（发邮件） |
| POST | `/auth/reset-password` | 重置密码（凭 token） |

### 4.3 当前用户 `/api/v1/me`

```
GET /api/v1/me
→
{
  "code": 0,
  "data": {
    "memberId": 1001,
    "username": "henry",
    "nickname": "Henry",
    "avatar": "https://...",
    "email": "henry@acme.com",
    "tenant": {
      "id": 100,
      "code": "acme",
      "name": "Acme Inc.",
      "plan": {"code": "standard", "name": "标准版"},
      "expireTime": "2026-12-31T23:59:59+08:00"
    },
    "roles": ["tenant_admin"],
    "permissions": ["apikey:create", "apikey:disable", ...]
  }
}
```

### 4.4 API Key 管理 `/api/v1/apikeys`

| 方法 | 路径 | 说明 | 权限 |
|---|---|---|---|
| GET | `/apikeys` | 列表（分页+搜索） | `apikey:list` |
| GET | `/apikeys/{id}` | 详情 | `apikey:list` |
| POST | `/apikeys` | 创建 | `apikey:create` |
| PUT | `/apikeys/{id}` | 更新 | `apikey:update` |
| PATCH | `/apikeys/{id}/status` | 启用/禁用 | `apikey:update` |
| DELETE | `/apikeys/{id}` | 删除 | `apikey:delete` |
| POST | `/apikeys/{id}/rotate` | 轮换（生成新 Key，旧的失效） | `apikey:update` |

**创建 API Key 请求**：
```json
POST /api/v1/apikeys
{
  "name": "生产环境调用",
  "scope": ["llm", "tts", "asr", "rag"],
  "ipWhitelist": ["1.2.3.0/24"],
  "expiresAt": "2027-08-12T00:00:00+08:00"
}
```

**响应**（**仅在创建时返回完整 Key**）：
```json
{
  "code": 0,
  "data": {
    "id": 5001,
    "name": "生产环境调用",
    "apiKey": "sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b",  // ⚠️ 仅此一次
    "keyPrefix": "sk-aisaas-9f3c5e2a",
    "scope": ["llm", "tts", "asr", "rag"],
    "expiresAt": "2027-08-12T00:00:00+08:00",
    "createTime": "2026-08-12T10:00:00+08:00"
  }
}
```

### 4.5 模型配置 `/api/v1/models`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/models` | 列出本租户可用模型（系统级 + 已订阅） |
| GET | `/models/{modelId}` | 详情 |
| GET | `/models/{modelId}/pricing` | 价格明细 |
| PUT | `/models/{modelId}/config` | 租户自定义（alias/限速/参数） |
| POST | `/models/{modelId}/enable` | 启用 |
| POST | `/models/{modelId}/disable` | 禁用 |

### 4.6 会话 `/api/v1/conversations`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/conversations` | 列表（支持按 userId/时间过滤） |
| GET | `/conversations/{id}` | 详情 |
| GET | `/conversations/{id}/messages` | 消息列表（分页） |
| DELETE | `/conversations/{id}` | 删除 |
| POST | `/conversations/{id}/title` | 改标题 |

### 4.7 知识库 `/api/v1/knowledge-bases`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/knowledge-bases` | 列表 |
| POST | `/knowledge-bases` | 创建 |
| GET | `/knowledge-bases/{id}` | 详情（含统计） |
| PUT | `/knowledge-bases/{id}` | 更新 |
| DELETE | `/knowledge-bases/{id}` | 删除 |
| GET | `/knowledge-bases/{id}/documents` | 文档列表 |
| POST | `/knowledge-bases/{id}/documents` | 上传文档（multipart） |
| DELETE | `/knowledge-bases/{id}/documents/{docId}` | 删除文档 |
| POST | `/knowledge-bases/{id}/documents/{docId}/reindex` | 重新索引 |
| POST | `/knowledge-bases/{id}/search` | 检索测试 |

**创建 KB**：
```json
POST /api/v1/knowledge-bases
{
  "name": "产品手册",
  "description": "v2.0 产品手册知识库",
  "embeddingModel": "text-embedding-v3",
  "chunkSize": 800,
  "chunkOverlap": 100
}
```

**检索测试**：
```json
POST /api/v1/knowledge-bases/5/search
{
  "query": "如何配置 WiFi？",
  "topK": 5,
  "rerank": true,
  "includeContent": true
}

→
{
  "code": 0,
  "data": {
    "results": [
      {
        "docId": 101,
        "docName": "用户手册.pdf",
        "chunkIndex": 12,
        "content": "## WiFi 配置\n进入设置→网络...",
        "score": 0.92,
        "rerankScore": 0.95
      }
    ],
    "tokensUsed": 28
  }
}
```

### 4.8 Persona `/api/v1/personas`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/personas` | 列表（含系统预置） |
| POST | `/personas` | 创建 |
| GET | `/personas/{id}` | 详情 |
| PUT | `/personas/{id}` | 更新 |
| DELETE | `/personas/{id}` | 删除 |
| POST | `/personas/{id}/clone` | 复制为副本 |
| POST | `/personas/{id}/bind-voice` | 绑定声音 |
| POST | `/personas/{id}/test` | 测试对话（流式返回） |

### 4.9 声音库 `/api/v1/voices`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/voices` | 列表（系统预置 + 租户克隆） |
| GET | `/voices/{id}` | 详情 |
| GET | `/voices/{id}/sample` | 试听音频（30s） |
| DELETE | `/voices/{id}` | 删除（仅自己克隆的） |
| POST | `/voices/clone` | ⭐ 发起克隆任务（P1） |

**发起声音克隆**：
```json
POST /api/v1/voices/clone
{
  "name": "CEO专属音色",
  "samples": [
    {"url": "https://...", "duration": 30}
  ],
  "provider": "aliyun-cosyvoice",
  "webhookUrl": "https://acme.com/cb"
}

→
{
  "code": 0,
  "data": {
    "jobId": 9001,
    "status": "queued",
    "estimatedMinutes": 15
  }
}
```

### 4.10 MCP 工具 `/api/v1/mcp`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/mcp/servers` | 列出可用 MCP Server |
| GET | `/mcp/tools` | 列出工具 |
| POST | `/mcp/servers` | 注册私有 MCP Server |
| PUT | `/mcp/servers/{id}` | 更新 |
| DELETE | `/mcp/servers/{id}` | 删除 |
| POST | `/mcp/tools/{id}/bind` | 绑定到租户 |
| DELETE | `/mcp/tools/{id}/bind` | 解绑 |
| POST | `/mcp/tools/{id}/test` | 工具调用测试 |
| GET | `/mcp/tools/{id}/calls` | 调用历史 |

### 4.11 长任务 `/api/v1/jobs`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/jobs` | 任务列表（按 type 过滤） |
| GET | `/jobs/{id}` | 详情（含日志） |
| GET | `/jobs/{id}/logs` | 日志流（SSE） |
| POST | `/jobs/{id}/cancel` | 取消 |
| GET | `/jobs/{id}/result` | 获取产物 |

### 4.12 计费 `/api/v1/billing`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/billing/balance` | 当前余额 |
| GET | `/billing/quotas` | 各维度配额状态 |
| GET | `/billing/quotas/history` | 配额历史（按月） |
| GET | `/billing/transactions` | 余额流水 |
| GET | `/billing/bills` | 账单列表 |
| GET | `/billing/bills/{id}` | 账单详情（PDF 下载链接） |
| POST | `/billing/recharge` | 发起充值（返回支付二维码） |
| GET | `/billing/recharge/{id}/status` | 充值状态 |
| GET | `/billing/subscription` | 当前订阅 |
| POST | `/billing/subscription/change` | 切换套餐 |

### 4.13 用量 `/api/v1/usage`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/usage/overview` | 总览（按维度） |
| GET | `/usage/by-model` | 按模型分组 |
| GET | `/usage/by-apikey` | 按 API Key 分组 |
| GET | `/usage/by-day` | 按日时序 |
| GET | `/usage/export` | 导出 CSV |
| GET | `/usage/realtime` | 实时调用量（SSE） |

### 4.14 成员管理 `/api/v1/members`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/members` | 成员列表 |
| POST | `/members` | 邀请（发邮件） |
| GET | `/members/{id}` | 详情 |
| PUT | `/members/{id}` | 更新 |
| DELETE | `/members/{id}` | 移除 |
| POST | `/members/{id}/reset-password` | 重置密码 |
| GET | `/roles` | 角色列表 |
| POST | `/roles` | 创建角色 |
| PUT | `/roles/{id}` | 更新 |

### 4.15 租户设置 `/api/v1/tenant`

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/tenant` | 租户信息 |
| PUT | `/tenant` | 更新 |
| PUT | `/tenant/logo` | 上传 Logo |

---

## 5. WebSocket 实时流（**v1.2：MVP 只做通路 B**，详细架构见 [ARCHITECTURE.md §7.5](./ARCHITECTURE.md)）

### 5.1 通路 A：OpenAI Realtime API 兼容 — **延后到 V1.1**

> ⚠️ **MVP 不做**。外部租户 MVP 阶段用 SSE + HTTP Chunked 即可。

规划路径：`wss://aisaas.ykt.com/v1/realtime?api_key=sk-aisaas-xxx`
规划用途：第三方实时对话客户端
规划协议：完全兼容 [OpenAI Realtime API](https://platform.openai.com/docs/api-reference/realtime)
预计上线：V1.1（2026-Q4 或之后）

### 5.2 通路 B：xiaozhi 内部协议（`/internal/xiaozhi/v1/realtime`） — **MVP P0**

**用途**：保留 xiaozhi-server 现有设备协议（`hello`/`listen`/`iot`/`abort`/`goodbye`/`mcp` 消息类型），平台透明转发 AI 调用
**鉴权**：`X-Internal-Token` Header（内部超级租户专用密钥）

```
ws://localhost:8190/internal/xiaozhi/v1/realtime
Header: X-Internal-Token: {internalApiKey}
Header: X-Source: xiaozhi-server
```

> 同物理机部署时用 `localhost`，跨网络部署时用内网 IP。

#### 协议

完全兼容 xiaozhi 现有协议（拷贝自 `xiaozhi-common/communication/common/`），平台在内部把消息路由到 ai-realtime 流水线。

#### 消息类型

| type | 方向 | 说明 |
|---|---|---|
| `hello` | C→S | 握手（含 deviceId/audioParams） |
| `hello` | S→C | 握手响应（含 sessionId/transport） |
| `listen` | C→S | 监听指令（start/stop/detect） |
| `tts` | S→C | TTS 控制指令（start/stop sentence） |
| `iot` | C→S → S→C | IoT 状态变更（含工具调用结果） |
| `abort` | C→S | 中止当前响应（barge-in） |
| `goodbye` | C→S | 退出 |
| `mcp` | C→S → S→C | MCP 工具调用双向消息 |
| 二进制帧 | C→S | Opus 音频上行（20ms 一帧） |
| 二进制帧 | S→C | Opus 音频下行（TTS 产出） |

#### 平台扩展消息字段

```json
// hello 消息扩展
{
  "type": "hello",
  "deviceId": "device-abc-001",
  "audioParams": {...},
  
  // ⭐ v1.2 平台扩展
  "xDeviceSessionId": 99001,        // 平台 device_session.id（首次接入时分配）
  "xPersonaId": 88                  // 设备绑定的 Persona（覆盖默认）
}
```

#### 调用示例（xiaozhi-server 内部代码）

```java
// xiaozhi-server 改造后，连接平台的代码（参考）
@ClientEndpoint
public class PlatformRealtimeClient {
    @OnOpen
    public void onOpen(Session session) {
        session.getUserProperties().put("X-Internal-Token", internalToken);
        // 发送 hello
        session.getAsyncRemote().sendText(JsonUtils.toJson(
            new HelloMessage(deviceId, audioParams, deviceSessionId, personaId)
        ));
    }
    
    @OnMessage
    public void onBinary(byte[] audioFrame) {
        // 平台 TTS 输出的 Opus 音频帧
        devicePlayer.feed(audioFrame);
    }
    
    @OnMessage
    public void onText(String message) {
        // 控制消息（tts/iot/mcp 等）
        handleMessage(message);
    }
}
```

### 5.3 外部租户的"实时对话"方案（**v1.2 替代方案**）

MVP 阶段外部租户无 WebSocket 实时流（通路 A 延后），需要"类 ChatGPT"实时体验时：

| 场景 | 替代方案 |
|---|---|
| LLM 流式响应 | SSE `POST /v1/chat/completions` (stream=true) |
| TTS 音频流 | HTTP Chunked `POST /v1/audio/speech` (x-stream=true) |
| ASR 上传 | POST `POST /v1/audio/transcriptions`（短音频） |
| 长音频 ASR | 客户端分片上传（自定义协议，非流式） |
| 真正的"边说边识别" | ❌ 暂不支持，等 V1.1 通路 A |

**典型应用**：浏览器聊天框、App 文字/语音助手、客服机器人 → **SSE 已足够**。

---

## 6. 内部超级租户接口（`/internal/api/v1/*`，**v1.1 新增**）

> 仅供 xiaozhi-server 等内部系统使用，普通租户无法访问。
> 鉴权：`X-Internal-Token` Header（内部专用密钥）+ IP 白名单。

### 6.1 路由总览

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/internal/api/v1/device-sessions` | 注册设备-会话绑定（xiaozhi-server 在设备首次接入时调用） |
| GET | `/internal/api/v1/device-sessions/{deviceId}` | 查询设备当前会话 |
| DELETE | `/internal/api/v1/device-sessions/{deviceId}` | 解绑设备 |
| POST | `/internal/api/v1/device-sessions/{deviceId}/heartbeat` | 设备心跳（保持会话活跃） |
| GET | `/internal/api/v1/internal-usage/realtime` | 内部用量实时统计（SSE 流） |
| POST | `/internal/api/v1/personas/bulk-import` | 批量导入 Persona（数据迁移用） |
| POST | `/internal/api/v1/models/bulk-import` | 批量导入模型注册（数据迁移用） |

### 6.2 注册设备会话

```json
POST /internal/api/v1/device-sessions
Header: X-Internal-Token: {token}

{
  "deviceId": "device-abc-001",
  "deviceCode": "XZ-V2-001",
  "personaId": 88,                          // 设备绑定的 Persona（可选）
  "externalUserId": "user-123",
  "metadata": {"firmwareVersion": "2.2.6"}
}

→
{
  "code": 0,
  "data": {
    "deviceSessionId": 99001,
    "conversationId": 88001,                // 平台自动创建的会话
    "personaSnapshot": {...},               // 当前 Persona 快照（避免运行时变更）
    "quotaContext": {
      "isUnlimited": true,                  // 内部租户通常不限配额
      "rateLimits": {"llmQps": 500}
    }
  }
}
```

### 6.3 批量导入（数据迁移专用）

```json
POST /internal/api/v1/personas/bulk-import
Header: X-Internal-Token: {token}

{
  "source": "xiaozhi-sys-role",
  "items": [
    {
      "externalId": "xiaozhi-role-1",
      "code": "xiaozhi_role_1",
      "name": "温柔助手",
      "systemPrompt": "...",
      "greetingText": "...",
      "defaultEmotion": "warm",
      "temperature": 0.7
    }
    // ... 批量
  ]
}

→
{
  "code": 0,
  "data": {
    "imported": 15,
    "failed": 0,
    "mapping": [                            // externalId ↔ 平台 personaId 映射
      {"externalId": "xiaozhi-role-1", "personaId": 88},
      ...
    ]
  }
}
```

### 6.4 内部用量实时统计（SSE）

```
GET /internal/api/v1/internal-usage/realtime
Accept: text/event-stream

event: x-realtime-stats
data: {
  "timestamp": "2026-08-12T10:30:00+08:00",
  "activeDevices": 1245,
  "activeConversations": 832,
  "llmCallsPerSecond": 156,
  "ttsCharsPerSecond": 4200,
  "avgLatency": {
    "asrFirstToken": 420,
    "llmFirstToken": 1100,
    "ttsFirstAudio": 650,
    "total": 2170
  }
}
```

---

## 7. 运营后台接口（/admin/api/v1/*）

仅列出主要路由，详细字段见 Knife4j。

| 模块 | 路径 |
|---|---|
| 鉴权 | `/admin/api/v1/auth/*` |
| 仪表盘 | `/admin/api/v1/dashboard` |
| 租户管理 | `/admin/api/v1/tenants` |
| 套餐管理 | `/admin/api/v1/plans` |
| 模型管理 | `/admin/api/v1/models/registry` |
| 财务 | `/admin/api/v1/finance/bills`、`/admin/api/v1/finance/refunds` |
| 字典 | `/admin/api/v1/dict/*` |
| 系统用户 | `/admin/api/v1/admin-users` |
| 操作日志 | `/admin/api/v1/logs/operation` |
| 登录日志 | `/admin/api/v1/logs/login` |
| 系统设置 | `/admin/api/v1/system/*` |

---

## 8. 错误码规范

### 7.1 编码规则

`{4 位 HTTP 状态码}{2 位业务域}{2 位序号}`

| 业务域 | 编号 | 示例 |
|---|---|---|
| 通用 | 00 | `40001` 参数错误 |
| 鉴权 | 01 | `40101` API Key 无效 |
| 权限 | 02 | `40301` 租户已冻结 |
| 配额 | 03 | `40201` 配额不足 |
| 资源 | 04 | `40401` 模型不存在 |
| 上游 | 05 | `50201` Provider 异常 |
| 系统 | 09 | `50001` 上下文丢失 |

### 7.2 主要错误码清单

| code | HTTP | message | 含义 |
|---|---|---|---|
| 0 | 200 | success | 成功 |
| 40001 | 400 | 参数错误 | 字段校验失败 |
| 40002 | 400 | JSON 解析失败 | 请求体格式错 |
| 40101 | 401 | API Key 无效 | Key 不存在或哈希不匹配 |
| 40102 | 401 | API Key 已过期 | expiresAt 已过 |
| 40103 | 401 | IP 不在白名单 | - |
| 40104 | 401 | Token 无效 | Sa-Token 失效 |
| 40201 | 402 | 配额不足 | 月配额耗尽 |
| 40202 | 402 | 余额不足 | 充值账户 |
| 40203 | 429 | 请求过于频繁 | 限流 |
| 40301 | 403 | 租户已冻结 | - |
| 40302 | 403 | 权限不足 | 角色缺权限点 |
| 40401 | 404 | 模型不存在或未启用 | - |
| 40402 | 404 | 知识库不存在 | - |
| 40403 | 404 | Persona 不存在 | - |
| 40404 | 404 | 资源不存在 | 通用 |
| 40901 | 409 | 资源已存在 | 唯一约束冲突 |
| 40902 | 409 | 状态不允许操作 | 如已禁用的 Key 不能再次禁用 |
| 42201 | 422 | 文件格式不支持 | 上传文档 |
| 42202 | 422 | 文件大小超限 | - |
| 50001 | 500 | 租户上下文丢失 | **严重**，应触发告警 |
| 50002 | 500 | 系统内部错误 | 兜底 |
| 50201 | 502 | 上游服务异常 | Provider 返回错误 |
| 50202 | 504 | 上游服务超时 | Provider 不响应 |
| 50301 | 503 | 系统维护中 | - |

### 7.3 错误响应（OpenAI 兼容格式）

```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1723430460

{
  "error": {
    "message": "请求过于频繁，5 秒后重试",
    "type": "rate_limit_exceeded",
    "code": "40203",
    "requestId": "req-abc123",
    "retryAfterSeconds": 5
  }
}
```

---

## 9. SSE 流式协议细节

### 8.1 连接保持

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `X-Accel-Buffering: no`（强制 Nginx 不缓冲）

### 8.2 心跳

服务端每 15 秒发送：
```
: heartbeat

```
（注释行，不会触发客户端事件）

### 8.3 客户端断线重连

- Last-Event-ID header：服务端可识别并续传
- MVP 不支持续传，断线后需重新发起请求

### 8.4 错误处理

**流式中错误**（不中断流，单独发 `x-error` 事件）：
```
event: x-error
data: {"code":"50201","message":"上游模型超时，已自动切换","fatal":false}

data: {"choices":[{"delta":{"content":"抱歉，让我重新回答"},"finish_reason":null}]}
```

**致命错误**（流终止）：
```
event: x-error
data: {"code":"40201","message":"配额不足","fatal":true}

data: [DONE]
```

---

## 10. 限流策略详解

### 9.1 多级限流

```
请求 → 单 API Key QPM（Redisson RPermit）
       ├─ 超出 → 429
       │
       ▼ 单租户 QPM
       ├─ 超出 → 429
       │
       ▼ 单租户并发流式数
       ├─ 超出 → 429（提示等待）
       │
       ▼ 平台总 QPS（Bucket）
       ├─ 超出 → 429
       │
       ▼ 配额预扣（Redis Lua）
       ├─ 不足 → 402
       │
       ▼ 调用 Provider
```

### 9.2 限流维度配置

| 套餐 | API Key QPM | 租户 QPM | 并发流式 |
|---|---|---|---|
| 免费 | 10 | 30 | 5 |
| 标准 | 60 | 600 | 50 |
| 企业 | 600 | 6000 | 500 |
| 定制 | 可配 | 可配 | 可配 |

---

## 11. 幂等性

### 10.1 幂等 Key

非幂等的写操作（POST/DELETE）支持幂等：
```
POST /api/v1/apikeys
Idempotency-Key: client-uuid-xxx
```

- Redis 缓存 24 小时
- 同 Key 第二次请求直接返回首次结果

### 10.2 强制幂等的接口

- 充值 `/billing/recharge`
- 切换套餐 `/billing/subscription/change`
- 发起长任务 `/voices/clone`、`/jobs/*`

---

## 12. Webhook 回调

长任务（声音克隆、微调）和支付完成支持 Webhook 回调。

**回调签名**：
```
X-Aisaas-Signature: sha256=...
X-Aisaas-Event: voice_clone.completed
X-Aisaas-Delivery: {uuid}
```

**重试策略**：失败后 5 分钟、30 分钟、2 小时、6 小时、24 小时共 5 次。

**回调示例**：
```json
POST {client.webhookUrl}
{
  "event": "voice_clone.completed",
  "deliveryId": "dlv-xxx",
  "timestamp": "2026-08-12T10:30:00+08:00",
  "data": {
    "jobId": 9001,
    "status": "success",
    "voiceLibraryId": 2001,
    "voiceId": "cosyvoice-clone-xxx",
    "sampleUrl": "https://..."
  }
}
```

---

## 13. SDK 与示例

### 12.1 cURL

```bash
# 流式 Chat
curl -X POST https://aisaas.ykt.com/v1/chat/completions \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role":"user","content":"你好"}],
    "stream": true
  }'
```

### 12.2 Python（openai SDK）

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-aisaas-xxx",
    base_url="https://aisaas.ykt.com/v1"
)

stream = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "你好"}],
    stream=True,
    extra_body={"x-persona-id": 88, "x-knowledge-base-ids": [1, 2]}  # ⭐ 平台扩展
)

for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")
```

### 12.3 Java（调用方示例，任意 OpenAI SDK）

```java
// xiaozhi-server 接入：OpenAI SDK 换 baseUrl + 平台 Key 即可
OpenAIClient client = new OpenAIClient("sk-aisaas-xxx", "http://127.0.0.1:8190/v1");
```

### 12.4 JavaScript（浏览器 SSE）

```javascript
const resp = await fetch('https://aisaas.ykt.com/v1/chat/completions', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer ' + apiKey,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    model: 'gpt-4o-mini',
    messages: [{role: 'user', content: '你好'}],
    stream: true
  })
});

const reader = resp.body.getReader();
const decoder = new TextDecoder();
while (true) {
  const {done, value} = await reader.read();
  if (done) break;
  const text = decoder.decode(value);
  // 解析 SSE 行
}
```

---

## 14. 版本管理

- URL 路径含版本号 `/v1/`、`/api/v1/`、`/admin/api/v1/`
- 不兼容变更：升版本号 `/v2/`
- 兼容变更：扩展字段（前缀 `x-`）
- 废弃接口：保留 6 个月，响应头 `Deprecation: true` + `Sunset: <date>`

---

## 15. 待评审

- [ ] OpenAI 兼容接口的扩展字段是否真的用 `x-` 前缀（OpenAI 协议没明文规定，但社区惯例）
- [ ] ~~WebSocket 实时接口是否纳入 MVP（P1 阶段）~~ → **v1.1 已确定升 P0；v1.2 进一步收紧为只做通路 B**
- [ ] 文件上传是否需要分片上传（大音频文件 > 100MB）
- [ ] Webhook 签名算法（HMAC-SHA256 vs RSA）
- [ ] 错误码编码规则是否过长（5 位数）—— 可考虑 4 位数
- [ ] ~~v1.1：通路 A vs 通路 B MVP 是否都做~~ → **v1.2 已确定 MVP 只做 B，A 延后 V1.1**
- [ ] ~~v1.1：内部超级租户的鉴权方式~~ → **v1.2 已确定 X-Internal-Token Header**
