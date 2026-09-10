# ykt-aisaas V2 API 参考

> 版本：**v2.0（2026-09-02）** — 基于 OpenAPI 3.1 规范
> 状态：V2 新接口文档 · 待评审
> 关联：[API.md](./API.md)（V1） · [openapi/aisaas-v2.yaml](./openapi/aisaas-v2.yaml)

---

## 1.1 V2 vs V1 变化概览

| 变化维度 | V1 | V2 |
|---|---|---|
| **接口前缀** | `/v1/*`（OpenAI 兼容）<br>`/api/v1/*`（SaaS 管理）<br>`/internal/api/v1/*`（内部） | V1 全部保留<br>**新增 V2 专属模块** |
| **新增模块** | — | Memory（记忆）<br>Session（会话配额）<br>Persona（人设）<br>APIKey 生命周期<br>Audit（审计） |
| **配额机制** | 直接扣减 | **quota_snapshot 借记 → 结算退款** |
| **Key 轮换** | 手动 | **自动轮换 + 5min 宽限期** |
| **接口数量** | 约 20+ | **V2 新增 13 个** |

---

## 1.2 与 xiaozhi-server-go 的对接关系（Q6 决策）

```
┌─────────────────────────────────────────────────────────────────────┐
│                        xiaozhi-server-go                            │
│                                                                       │
│  启动时 ──→ /internal/api/v1/apikeys/rotate ──→ 获得长期 API Key    │
│              (X-Internal-Token 鉴权)                                   │
│                                                                       │
│  运行时 ──→ /api/v1/sessions/{deviceId}  ──→ 创建会话（含配额快照）   │
│              (API Key Bearer 鉴权)                                     │
│                                                                       │
│           ──→ /v1/chat/completions          ──→ LLM 对话             │
│              (API Key Bearer 鉴权，OpenAI 兼容)                         │
│                                                                       │
│           ──→ /api/v1/memories/{deviceId}/messages ──→ 写入记忆     │
│                                                                       │
│  结束时 ──→ /api/v1/sessions/{sessionId}/end ──→ 结算 + 退款        │
└─────────────────────────────────────────────────────────────────────┘
```

**Q6 核心决策**：xiaozhi-server-go 使用**长效 API Key**调用 ykt-aisaas，而非短期 Token。Key 轮换由运营后台触发，设备侧无感知。

---

## 1.3 13 个新接口模块分类图

```
ykt-aisaas V2 新增接口
│
├── Memory 模块（5 个）────────────────────────────────
│   POST   /api/v1/memories/{deviceId}/messages       ← 写入短期消息
│   GET    /api/v1/memories/{deviceId}/messages       ← 列出消息（分页）
│   GET    /api/v1/memories/{deviceId}                ← 拉取长期记忆图谱
│   POST   /api/v1/memories/{deviceId}/extract        ← 异步抽取实体
│   POST   /api/v1/memories/{deviceId}/summarize      ← 异步摘要聚合
│
├── Persona 模块（2 个）────────────────────────────────
│   GET    /api/v1/personas/{id}                      ← 按 ID 读取
│   GET    /api/v1/personas?deviceId=...               ← 按设备查默认
│
├── Session 模块（3 个）────────────────────────────────
│   POST   /api/v1/sessions/{deviceId}                 ← 创建会话（含借记）
│   GET    /api/v1/sessions/{deviceId}/history        ← 会话历史
│   POST   /api/v1/sessions/{sessionId}/end           ← 结算 + 退款
│
├── APIKey 模块（2 个，内部）──────────────────────────
│   POST   /internal/api/v1/apikeys/{keyId}/rotate     ← 轮换（5min 宽限）
│   POST   /internal/api/v1/apikeys/{keyId}/revoke     ← 紧急撤销
│
└── Audit 模块（1 个，内部）───────────────────────────
    GET    /internal/api/v1/audit-logs                 ← 审计日志查询
```

---

## 1.4 鉴权方式速查表

| 接口前缀 | 鉴权方式 | 适用场景 |
|---|---|---|
| `/v1/*` | API Key (Bearer `Bearer sk-aisaas-xxx`) | OpenAI 兼容（xiaozhi 调 chat/tts/asr） |
| `/api/v1/*` | API Key (Bearer `Bearer sk-aisaas-xxx`) | SaaS 自有管理（门户调 RAG/MCP CRUD） |
| `/internal/api/v1/*` | X-Internal-Token + loopback | 内部超级租户（运营后台 / xiaozhi-server-go 启动时） |
| `/internal/xiaozhi/v1/*` | X-Internal-Token + X-Device-Id | 设备即租户通道（xiaozhi 设备鉴权后） |
| `/portal/api/v1/*` | JWT Bearer (RS256) | 用户门户 |

> **loopback IP**：`127.0.0.0/8`, `::1`。外部请求即使携带正确 Token 也会被拒绝。

---

## 1.5 通用请求头

```
Authorization: Bearer {apiKey}          # API Key 鉴权
X-Internal-Token: {token}               # 内部接口鉴权（仅 loopback）
X-Request-Id: {client-uuid}             # 可选，链路追踪
Content-Type: application/json          # POST/PUT/PATCH 必须
Accept-Language: zh-CN                  # 国际化预留
```

---

## 1.6 统一响应格式

### 成功响应（SaaS 自有接口 `/api/v1/*`）

```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": { ... }
}
```

### 错误响应（SaaS 自有接口）

```json
{
  "code": 40201,
  "message": "配额不足",
  "requestId": "req_a1b2c3d4",
  "data": null
}
```

### OpenAI 兼容格式（仅 `/v1/*`）

```json
{
  "error": {
    "message": "Invalid API key",
    "type": "aai_error",
    "code": "40101"
  },
  "requestId": "req_a1b2c3d4"
}
```

---

## 2. Memory 模块

> **概述**：Memory 模块管理设备的短期会话消息与长期记忆图谱。
>
> **设计思路**：
> - **短期记忆**（messages）：每轮对话的消息存储，支持按 sessionId 隔离
> - **长期图谱**（graph）：从短期记忆经 LLM 抽取生成的实体-关系网络
> - **异步任务**（extract/summarize）：实体抽取和摘要聚合为异步任务，返回 taskId 供轮询

### 2.1 `POST /api/v1/memories/{deviceId}/messages` — 写入短期会话消息

**用途**：将单条消息写入短期记忆存储。可选传入 sessionId，不传则系统自动创建新会话。

**何时调用**：
- xiaozhi-server-go 每完成一轮对话后写入
- Portal 用户发送消息时写入

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | deviceId | string | ✅ | 设备唯一标识（MAC 哈希或平台分配） |

#### 请求体

```json
{
  "sessionId": "sess_abc123",           // 可选，不传则创建新会话
  "role": "user",                       // user | assistant | system
  "content": "今天天气真不错",
  "metadata": {
    "model": "gpt-4o-mini",
    "tokens": 128,
    "latencyMs": 450
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| sessionId | string | 否 | 会话 ID，不传则自动创建 |
| role | string | ✅ | 消息角色：`user` / `assistant` / `system` |
| content | string | ✅ | 消息内容 |
| metadata | object | 否 | 元数据（模型名、token 数、延迟等） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "id": 123456789,
    "sessionId": "sess_abc123",
    "createdAt": "2026-09-02T12:34:56.789Z"
  }
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int64 | 消息全局唯一 ID |
| sessionId | string | 所属会话 ID |
| createdAt | datetime | 创建时间（ISO 8601） |

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/messages" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -H "Content-Type: application/json" \
  -d '{
    "role": "user",
    "content": "今天天气真不错",
    "metadata": {"model": "gpt-4o-mini", "tokens": 128}
  }'
```

#### 注意事项

- **幂等性**：写入操作天然幂等（同一消息 ID 不会重复写入）
- **限流**：单设备 60 QPM
- **配额**：消息存储占用 `llm_tokens_in` 配额（按 content 长度估算）
- **失败重试**：若返回 5xx，建议指数退避重试，最多 3 次

---

### 2.2 `GET /api/v1/memories/{deviceId}/messages` — 列出会话消息

**用途**：按 deviceId 分页拉取短期会话消息记录，按创建时间倒序。

**何时调用**：
- 设备重连后拉取历史上下文
- Portal 用户查看会话历史

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|---|
| path | deviceId | string | ✅ | — | 设备唯一标识 |
| query | limit | int | 否 | 50 | 返回条数上限（1-200） |
| query | cursor | string | 否 | — | 游标分页（base64 编码的上一页最后一条消息 ID） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "items": [
      {
        "id": 123456789,
        "sessionId": "sess_abc123",
        "role": "user",
        "content": "今天天气真不错",
        "metadata": {"model": "gpt-4o-mini", "tokens": 128},
        "createdAt": "2026-09-02T12:34:56.789Z"
      },
      {
        "id": 123456788,
        "sessionId": "sess_abc123",
        "role": "assistant",
        "content": "是的，今天阳光明媚，很适合外出。",
        "metadata": {"model": "gpt-4o-mini", "tokens": 64},
        "createdAt": "2026-09-02T12:34:57.123Z"
      }
    ],
    "hasMore": true,
    "nextCursor": "eyJpZCI6MTIzNDU2Nzg4fQ=="
  }
}
```

#### curl 示例

```bash
# 首次请求
curl -G "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/messages" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -d "limit=50"

# 翻页（使用 nextCursor）
curl -G "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/messages" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -d "limit=50" \
  -d "cursor=eyJpZCI6MTIzNDU2Nzg4fQ=="
```

#### 注意事项

- **排序**：按 `createdAt` 倒序，最新消息在前
- **游标格式**：`base64(json.stringify({id: lastMessageId}))`
- **limit 上限**：单次最多 200 条，建议深度翻页用 cursor

---

### 2.3 `GET /api/v1/memories/{deviceId}` — 拉取长期记忆图谱

**用途**：聚合设备短期记忆，生成并返回长期记忆图谱（实体-关系网络）。

**何时调用**：
- 设备启动时加载长期记忆注入上下文
- 用户主动查询"我记得什么"

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|---|
| path | deviceId | string | ✅ | — | 设备唯一标识 |
| query | limit | int | 否 | 20 | 返回实体数量上限（1-100） |
| query | dimension | string | 否 | 全部 | 图谱维度筛选：`entity` / `preference` / `event` / `skill` |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "deviceId": "device-abc-001",
    "entities": [
      {
        "id": "ent_001",
        "type": "person",
        "name": "王小明",
        "confidence": 0.92,
        "mentions": ["用户提到他儿子", "用户说他喜欢打篮球"],
        "attributes": {"age": "8岁", "interest": "篮球"}
      },
      {
        "id": "ent_002",
        "type": "place",
        "name": "北京市朝阳区",
        "confidence": 0.88,
        "mentions": ["用户说住在这个区"]
      }
    ],
    "preferences": [
      {
        "dimension": "music",
        "value": "喜欢古典音乐",
        "confidence": 0.85,
        "sourceMessages": [123456789, 123456780]
      }
    ],
    "events": [
      {
        "id": "evt_001",
        "eventType": "appointment",
        "eventTime": "2026-09-05T10:00:00Z",
        "description": "带孩子看牙医",
        "participants": ["王小明"]
      }
    ],
    "updatedAt": "2026-09-02T12:00:00Z"
  }
}
```

#### curl 示例

```bash
# 拉取完整图谱
curl -G "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b"

# 只看实体维度
curl -G "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -d "dimension=entity" \
  -d "limit=50"
```

#### 注意事项

- **图谱生成**：由异步任务定期从 messages 抽取，需先调用 `/extract` 触发
- **缓存**：建议本地缓存图谱，5 分钟内不重复请求
- **dimension 筛选**：`entity`（实体）/`preference`（偏好）/`event`（事件）/`skill`（技能）

---

### 2.4 `POST /api/v1/memories/{deviceId}/extract` — 异步抽取实体

**用途**：对指定消息列表异步执行实体抽取（命名实体识别、偏好挖掘、事件抽取）。

**Q3 决策**：抽取维度由 `extractTypes` 指定。

**何时调用**：
- 每日定时任务，从短期记忆抽取新实体入图谱
- 用户说"帮我记住 XXX"后触发

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | deviceId | string | ✅ | 设备唯一标识 |

#### 请求体

```json
{
  "sessionId": "sess_abc123",                    // 可选，指定会话
  "messages": [
    {"role": "user", "content": "我儿子王小明今年8岁，喜欢打篮球"},
    {"role": "assistant", "content": "知道了，小明喜欢篮球，很棒！"}
  ],
  "extractTypes": ["entity", "preference", "event"]  // 抽取维度
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| sessionId | string | 否 | 指定会话，不传则处理该设备全部消息 |
| messages | array | ✅ | 待抽取的消息（至少 1 条） |
| messages[].role | string | ✅ | `user` / `assistant` / `system` |
| messages[].content | string | ✅ | 消息内容 |
| extractTypes | array | ✅ | 抽取维度：`entity` / `preference` / `event` / `skill` |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "taskId": "550e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "estimatedEntities": 5,
    "createdAt": "2026-09-02T12:34:56.789Z"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/extract" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "我儿子王小明今年8岁，喜欢打篮球"}
    ],
    "extractTypes": ["entity", "preference"]
  }'
```

#### 注意事项

- **异步任务**：返回 `taskId`，需轮询或等待回调
- **配额**：触发 LLM 调用，消耗 `llm_tokens_in`
- **状态枚举**：`pending` → `running` → `completed` / `failed`

---

### 2.5 `POST /api/v1/memories/{deviceId}/summarize` — 异步摘要聚合

**用途**：对设备记忆异步生成摘要聚合（会话摘要、主题聚类、关键事件）。

**何时调用**：
- 每日定时生成摘要
- 长对话结束后自动摘要

#### 请求体

```json
{
  "sessionId": "sess_abc123",                      // 可选
  "summarizeTypes": ["conversation", "topic"],     // 默认 ["conversation", "topic"]
  "topicFocus": "孩子的兴趣"                        // 可选，聚焦特定话题
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| sessionId | string | 否 | 指定会话 |
| summarizeTypes | array | 否 | 摘要类型：`conversation` / `topic` / `key_event` / `personality` |
| topicFocus | string | 否 | 聚焦特定话题 |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "taskId": "550e8400-e29b-41d4-a716-446655440001",
    "status": "pending",
    "createdAt": "2026-09-02T12:34:56.789Z"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/summarize" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -H "Content-Type: application/json" \
  -d '{
    "summarizeTypes": ["conversation", "topic", "key_event"],
    "topicFocus": "孩子教育"
  }'
```

#### 注意事项

- **与 extract 的区别**：`extract` 抽取实体入图谱，`summarize` 生成文本摘要
- **耗时**：摘要任务通常比抽取慢，建议等待 30s 以上再轮询

---

## 3. Persona 模块

> **概述**：Persona 模块提供人设（角色）的读取与查询接口。

### 3.1 `GET /api/v1/personas/{id}` — 读取人设详情

**用途**：按 persona ID 读取完整人设配置（含系统提示词、技能列表、元数据）。

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | id | int64 | ✅ | Persona 全局唯一 ID |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "id": 88,
    "name": "小智助手",
    "version": "1.0",
    "systemPrompt": "你是一个温柔、有耐心的家庭助手，擅长回答问题和讲故事。",
    "skills": [
      {
        "id": "skill_001",
        "name": "天气查询",
        "type": "tool",
        "config": {"provider": "weather_api"}
      },
      {
        "id": "skill_002",
        "name": "儿童故事",
        "type": "knowledge",
        "config": {"category": "童话"}
      }
    ],
    "metadata": {"emotion": "warm", "tone": "gentle"},
    "createdAt": "2026-08-01T00:00:00Z",
    "updatedAt": "2026-08-15T10:30:00Z"
  }
}
```

#### curl 示例

```bash
curl "https://api.aisaas.ykt.cn/api/v1/personas/88" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b"
```

#### 注意事项

- **幂等**：GET 操作天然幂等
- **缓存**：建议缓存 persona 数据，更新频率低

---

### 3.2 `GET /api/v1/personas?deviceId=...` — 按设备查询默认 Persona

**用途**：查询指定设备对应的默认 Persona（含 persona_bind 绑定关系）。

**何时调用**：设备启动时查询默认人设

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|---|
| query | deviceId | string | ✅ | — | 设备 ID |
| query | limit | int | 否 | 10 | 返回条数上限（1-50） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "items": [
      {
        "id": 88,
        "name": "小智助手",
        "version": "1.0",
        "systemPrompt": "你是一个温柔、有耐心的家庭助手。",
        "skills": [...],
        "persona_bind": {
          "bindId": 1001,
          "personaId": 88,
          "deviceId": "device-abc-001",
          "isDefault": true,
          "boundAt": "2026-08-01T00:00:00Z"
        }
      }
    ],
    "hasMore": false
  }
}
```

#### curl 示例

```bash
curl -G "https://api.aisaas.ykt.cn/api/v1/personas" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -d "deviceId=device-abc-001" \
  -d "limit=10"
```

#### 注意事项

- **多绑定**：若设备有多个绑定，优先返回 `isDefault: true` 的记录
- **无绑定**：返回空 `items`（而非错误）

---

## 4. Session 模块（Q8 关键）

> **概述**：Session 模块管理设备会话的完整生命周期，包含配额快照借记与结算退款。
>
> **Q8 核心决策**：
> - **quota_snapshot 借记**：创建会话时从设备配额中预扣 `quotaInitial`
> - **结算退款**：会话结束时根据实际消耗 `actualCost` 退还剩余配额
> - **设备级 vs 会话级配额边界**：配额在设备维度扣减，会话记录快照值

### 4.1 `POST /api/v1/sessions/{deviceId}` — 创建会话（含配额快照借记）

**用途**：创建设备会话并预借配额（quota_snapshot）。

**为什么必须先创建 session 再调用 chat**：
1. 配额从设备维度借记，确保不会超额使用
2. 后续 `/sessions/{sessionId}/end` 依赖创建时生成的 `sessionId`
3. 审计链路完整（创建 → 使用 → 结算）

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | deviceId | string | ✅ | 设备唯一标识 |

#### 请求体

```json
{
  "dimension": "llm_tokens_in",
  "quotaInitial": 2048,
  "personaId": 88,
  "metadata": {
    "ip": "192.168.1.100",
    "userAgent": "xiaozhi-server-go/1.0"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| dimension | string | ✅ | 借记维度：`llm_tokens_in` / `llm_tokens_out` / `tts_chars` / `asr_seconds` |
| quotaInitial | int | ✅ | 申请借记量（最小 1） |
| personaId | int64 | 否 | 关联的 Persona ID |
| metadata | object | 否 | 客户端元数据（IP、User-Agent） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "sessionId": "550e8400-e29b-41d4-a716-446655440002",
    "deviceId": "device-abc-001",
    "dimension": "llm_tokens_in",
    "quotaInitial": 2048,
    "quotaRemaining": 998976,
    "quotaSnapshot": {
      "tenantId": 100,
      "deviceQuotaLimit": 1000000,
      "deviceQuotaUsed": 1024,
      "deviceQuotaRemaining": 998976,
      "snapshotTime": "2026-09-02T12:34:56.789Z"
    },
    "createdAt": "2026-09-02T12:34:56.789Z"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/device-abc-001" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -H "Content-Type: application/json" \
  -d '{
    "dimension": "llm_tokens_in",
    "quotaInitial": 2048,
    "personaId": 88,
    "metadata": {"ip": "192.168.1.100"}
  }'
```

#### 注意事项

- **quotaRemaining**：借记后设备剩余配额（用于判断是否可继续）
- **402 配额不足**：若 `quotaRemaining < quotaInitial`，返回 402
- **409 冲突**：若设备已有活跃会话（未 end），返回 409
- **幂等**：相同 `deviceId` + `dimension` 短期内重复创建返回 409

---

### 4.2 `GET /api/v1/sessions/{deviceId}/history` — 拉取会话历史

**用途**：按设备拉取最近 N 轮对话记录，返回完整消息列表（user + assistant）。

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|---|
| path | deviceId | string | ✅ | — | 设备唯一标识 |
| query | limit | int | 否 | 10 | 返回轮数上限（1-100，每轮含 user + assistant） |
| query | cursor | string | 否 | — | 游标分页 |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "sessionId": "550e8400-e29b-41d4-a716-446655440002",
    "items": [
      {
        "id": 123456789,
        "role": "user",
        "content": "今天天气真不错",
        "metadata": {"model": "gpt-4o-mini", "tokens": 128},
        "createdAt": "2026-09-02T12:34:56.789Z"
      },
      {
        "id": 123456790,
        "role": "assistant",
        "content": "是的，今天阳光明媚。",
        "metadata": {"model": "gpt-4o-mini", "tokens": 64},
        "createdAt": "2026-09-02T12:34:57.123Z"
      }
    ],
    "hasMore": false,
    "nextCursor": null
  }
}
```

#### curl 示例

```bash
curl -G "https://api.aisaas.ykt.cn/api/v1/sessions/device-abc-001/history" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -d "limit=10"
```

#### 注意事项

- **不传 sessionId**：返回该设备最新会话的历史
- **与 Memory 的区别**：Session history 是完整消息，Memory messages 是短期存储

---

### 4.3 `POST /api/v1/sessions/{sessionId}/end` — 结算会话实际消耗（含配额退款）

**用途**：结算会话实际消耗并退还剩余配额。

**Q8 决策**：
- `actualCost` 包含实际消耗字段
- `quotaRefunded` 为应退金额（`quotaInitial - quotaUsed`）
- `status` 为 `failed` 时标记会话异常结束，消耗按实际计入

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | sessionId | UUID | ✅ | 会话全局唯一 ID |

#### 请求体

```json
{
  "actualCost": {
    "llm_tokens_in": 1500,
    "llm_tokens_out": 800
  },
  "status": "success"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| actualCost | object | ✅ | 实际消耗量（多维度） |
| actualCost.llm_tokens_in | int | 否 | LLM 输入 token |
| actualCost.llm_tokens_out | int | 否 | LLM 输出 token |
| actualCost.tts_chars | int | 否 | TTS 字符数 |
| actualCost.asr_seconds | int | 否 | ASR 秒数 |
| status | string | ✅ | `success`（正常结束）/ `failed`（异常结束） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "sessionId": "550e8400-e29b-41d4-a716-446655440002",
    "quotaUsed": {
      "llm_tokens_in": 1500,
      "llm_tokens_out": 800
    },
    "quotaRefunded": {
      "llm_tokens_in": 548,
      "llm_tokens_out": 0
    },
    "endedAt": "2026-09-02T12:45:00.000Z"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/550e8400-e29b-41d4-a716-446655440002/end" \
  -H "Authorization: Bearer sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b" \
  -H "Content-Type: application/json" \
  -d '{
    "actualCost": {
      "llm_tokens_in": 1500,
      "llm_tokens_out": 800
    },
    "status": "success"
  }'
```

#### 失败时 Refund 行为

| 场景 | refund 行为 |
|---|---|
| 正常结束（success） | `quotaInitial - quotaUsed` 退还 |
| 异常结束（failed） | `quotaInitial - quotaUsed` 退还，消耗按实际计入审计 |
| 调用方未传 actualCost | 使用 quotaInitial 作为消耗（全额不退款） |
| 会话不存在 | 404 |

#### 注意事项

- **幂等**：重复调用 `end` 返回相同结果
- **顺序**：必须先 `createSession`，再 `endSession`
- **超时**：若设备异常断开，运营后台可代为调用 `endSession`（status=failed）

---

## 5. APIKey 管理模块（Q6 决策，内部接口）

> **概述**：APIKey 模块处理 API Key 的轮换与紧急撤销。
>
> **Q6 核心决策**：
> - xiaozhi-server-go 启动时调用 `/internal/api/v1/apikeys/rotate` 获取长期 Key
> - 运营后台可触发轮换，设备侧有 **5 分钟宽限期**
> - 安全事件时立即撤销，**Redis 缓存立即失效**

### 5.1 `POST /internal/api/v1/apikeys/{keyId}/rotate` — API Key 轮换

**用途**：轮换指定 API Key，生成新 Key，保留旧 Key 5 分钟宽限期。

**xiaozhi-server-go 启动时调用流程**：
```
1. xiaozhi-server-go 启动
2. 使用 X-Internal-Token 调用 rotate（旧 Key 可以是任意有效 KeyId 或新建）
3. 获得新 Key 明文（仅此一次）
4. 使用新 Key 调用 /api/v1/sessions 创建会话
5. 5 分钟内旧 Key 仍可用，切换无感
```

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | keyId | int64 | ✅ | 待轮换的 API Key ID |

#### 请求体

```json
{
  "expireDays": 30,
  "rotateStrategy": "time_24h"
}
```

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| expireDays | int | ✅ | — | 新 Key 有效期（天），最小 1 |
| rotateStrategy | string | 否 | `time_24h` | 轮换策略：`time_24h` / `on_use_count` / `manual` |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "oldKeyId": 5001,
    "oldKeyExpiresAt": "2026-09-02T12:39:56.789Z",
    "newKeyId": 5002,
    "newApiKey": "sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL",
    "newKeyPrefix": "sk-aisaas-K5x9mN2p...",
    "newKeyExpiresAt": "2026-10-02T12:34:56.789Z"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/internal/api/v1/apikeys/5001/rotate" \
  -H "X-Internal-Token: internal-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "expireDays": 30,
    "rotateStrategy": "time_24h"
  }'
```

#### 5 分钟宽限期机制

```
旧 Key (5001)              新 Key (5002)
     │                          │
     │ ←── 5 分钟宽限期 ──→     │
     │                          │
     ├─ 仍可调用 /api/v1/*      ├─ 立即可用
     │                          │
     └─ 12:39:56 到期           └─ 30 天后到期
```

**宽限期结束后**：
- 旧 Key 调用返回 `40101 API Key 无效`
- Redis 缓存自动失效，不额外处理

#### 注意事项

- **新 Key 明文仅此一次**：之后只能通过 `newKeyPrefix` 识别
- **rotateStrategy 说明**：
  - `time_24h`：24 小时后自动轮换（推荐）
  - `on_use_count`：使用 N 次后轮换
  - `manual`：手动轮换（运营后台触发）
- **409 冲突**：若 Key 正在轮换中（上次轮换未完成），返回 409

---

### 5.2 `POST /internal/api/v1/apikeys/{keyId}/revoke` — API Key 紧急撤销

**用途**：立即撤销指定 API Key，用于安全事件响应。

**撤销后行为**：
- Key 立即失效，无宽限期
- Redis 缓存立即失效
- 记录到审计日志（含 `reason`）

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 说明 |
|---|---|---|---|---|
| path | keyId | int64 | ✅ | 待撤销的 API Key ID |

#### 请求体

```json
{
  "reason": "疑似 Key 泄露，已被第三方使用"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| reason | string | 否 | 撤销原因（记录到审计日志） |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "keyId": 5001,
    "revokedAt": "2026-09-02T12:34:56.789Z",
    "reason": "疑似 Key 泄露，已被第三方使用"
  }
}
```

#### curl 示例

```bash
curl -X POST "https://api.aisaas.ykt.cn/internal/api/v1/apikeys/5001/revoke" \
  -H "X-Internal-Token: internal-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "疑似 Key 泄露"
  }'
```

#### 与 RevokeByInternal 的关系

> `RevokeByInternal` 是内部超级租户撤销（运营后台调用本接口）。
> 外部租户撤销自己的 Key 走 `/api/v1/apikeys/{id}` DELETE（软删）。

| 撤销方 | 接口 | 行为 |
|---|---|---|
| 运营后台（内部） | `POST /internal/api/v1/apikeys/{keyId}/revoke` | 立即失效，无宽限期 |
| 租户自己 | `DELETE /api/v1/apikeys/{id}` | 软删，有宽限期（24h） |

#### 注意事项

- **立即失效**：无宽限期，请确认后再调用
- **审计日志**：撤销原因记录到审计日志，供追溯
- **幂等**：重复撤销返回 404（Key 已不存在）

---

## 6. Audit 模块（Q6 决策，内部接口）

> **概述**：Audit 模块提供审计日志查询，仅限内部超级租户。

### 6.1 `GET /internal/api/v1/audit-logs` — 查询审计日志

**用途**：内部超级租户查询审计日志，支持多条件筛选与游标分页。

**配合 OpenTelemetry / Jaeger 使用**：
- `traceId` 字段对接分布式追踪
- 按 `traceId` 可查询完整调用链

#### 参数

| 位置 | 参数名 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|---|
| query | actor_type | string | 否 | — | 行为者类型：`device` / `user` / `api_key` / `system` |
| query | tenant_id | int64 | 否 | — | 租户 ID 筛选 |
| query | event_time_start | datetime | 否 | — | 事件时间范围起（ISO 8601） |
| query | event_time_end | datetime | 否 | — | 事件时间范围止（ISO 8601） |
| query | action | string | 否 | — | 事件动作筛选（如 `auth.fail`） |
| query | result | string | 否 | — | 事件结果：`success` / `failure` |
| query | limit | int | 否 | 100 | 每页条数（1-500） |
| query | cursor | string | 否 | — | 游标分页 |

#### 响应

**200 OK**
```json
{
  "code": 0,
  "message": "success",
  "requestId": "req_a1b2c3d4",
  "data": {
    "items": [
      {
        "eventId": "550e8400-e29b-41d4-a716-446655440003",
        "actorType": "api_key",
        "actorId": "device-mac-a1b2c3d4",
        "actorIp": "192.168.1.100",
        "action": "auth.fail",
        "actionDetail": {
          "reason": "Key 已过期"
        },
        "result": "failure",
        "eventTime": "2026-09-02T12:30:00.000Z",
        "traceId": "abc123def456",
        "region": "cn-east-1"
      },
      {
        "eventId": "550e8400-e29b-41d4-a716-446655440004",
        "actorType": "device",
        "actorId": "device-abc-001",
        "actorIp": "192.168.1.100",
        "action": "session.create",
        "actionDetail": {
          "dimension": "llm_tokens_in",
          "quotaInitial": 2048
        },
        "result": "success",
        "eventTime": "2026-09-02T12:34:56.789Z",
        "traceId": "def456ghi789",
        "region": "cn-east-1"
      }
    ],
    "hasMore": true,
    "nextCursor": "eyJldmVudElkIjogIjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMyJ9"
  }
}
```

#### curl 示例

```bash
# 查询某设备的所有事件
curl -G "https://api.aisaas.ykt.cn/internal/api/v1/audit-logs" \
  -H "X-Internal-Token: internal-secret-token" \
  -d "actor_type=device" \
  -d "event_time_start=2026-09-01T00:00:00Z" \
  -d "event_time_end=2026-09-03T00:00:00Z" \
  -d "limit=100"

# 查询认证失败事件
curl -G "https://api.aisaas.ykt.cn/internal/api/v1/audit-logs" \
  -H "X-Internal-Token: internal-secret-token" \
  -d "action=auth.fail" \
  -d "result=failure" \
  -d "limit=50"

# 按 traceId 追查完整调用链
curl -G "https://api.aisaas.ykt.cn/internal/api/v1/audit-logs" \
  -H "X-Internal-Token: internal-secret-token" \
  -d "traceId=abc123def456"
```

#### 溯源字段说明

| 字段 | 说明 |
|---|---|
| eventId | 事件 ID（UUID v7），全局唯一 |
| actorType | 行为者类型：`device`（设备）/ `user`（用户）/ `api_key`（API Key）/ `system`（系统） |
| actorId | 行为者 ID（如设备 MAC 哈希） |
| actorIp | 行为者 IP |
| action | 事件动作（如 `session.create`、`auth.fail`、`apikey.revoke`） |
| actionDetail | 动作详情（原因、参数等） |
| result | 结果：`success` / `failure` |
| traceId | 分布式追踪 ID（对接 OpenTelemetry/Jaeger） |
| region | 区域标识（如 `cn-east-1`） |

#### 注意事项

- **限内部超级租户**：仅 `X-Internal-Token` + loopback IP 可访问
- **数据保留**：审计日志默认保留 180 天
- **查询性能**：建议按时间范围 + actor_type 组合查询，避免全表扫描

---

## 7. 错误响应格式

### 7.1 统一信封（SaaS 自有接口）

```json
{
  "code": 40201,
  "message": "配额不足",
  "requestId": "req_a1b2c3d4",
  "data": null
}
```

### 7.2 OpenAI 兼容错误（仅 `/v1/*`）

```json
{
  "error": {
    "message": "配额不足",
    "type": "insufficient_quota",
    "code": "40201"
  },
  "requestId": "req_a1b2c3d4"
}
```

### 7.3 常见错误码

| HTTP | code | message | 说明 |
|---|---|---|---|
| 400 | 40001 | 参数错误 | 字段校验失败 |
| 400 | 40002 | JSON 解析失败 | 请求体格式错 |
| 401 | 40101 | API Key 无效 | Key 不存在或哈希不匹配 |
| 401 | 40102 | API Key 已过期 | expiresAt 已过 |
| 401 | 40103 | IP 不在白名单 | — |
| 402 | 40201 | 配额不足 | 月配额耗尽 |
| 402 | 40203 | 请求过于频繁 | 限流 |
| 403 | 40301 | 租户已冻结 | — |
| 403 | 40302 | 权限不足 | 角色缺权限点 |
| 404 | 40404 | 资源不存在 | 通用 |
| 409 | 40901 | 资源已存在 | 唯一约束冲突 |
| 409 | 40902 | 状态不允许操作 | 如已存在活跃会话 |
| 500 | 50002 | 系统内部错误 | 兜底 |
| 502 | 50201 | 上游服务异常 | Provider 返回错误 |
| 502 | 50202 | 上游服务超时 | Provider 不响应 |

---

## 8. 限流 / 配额

### 8.1 API Key 维度限流

| 套餐 | 单 API Key QPM | 单租户 QPM | 并发流式 |
|---|---|---|---|
| 免费 | 10 | 30 | 5 |
| 标准 | 60 | 600 | 50 |
| 企业 | 600 | 6000 | 500 |

**429 响应**：
```json
{
  "code": 40203,
  "message": "请求过于频繁，请稍后再试",
  "requestId": "req_a1b2c3d4",
  "data": {"retryAfterSeconds": 5}
}
```

### 8.2 配额维度

| 维度 | 说明 | 超额返回 |
|---|---|---|
| `llm_tokens_in` | LLM 输入 Token | 402 |
| `llm_tokens_out` | LLM 输出 Token | 402 |
| `tts_chars` | TTS 字符数 | 402 |
| `asr_seconds` | ASR 秒数 | 402 |

**配额计算**：
- 创建会话时借记 `quotaInitial`
- 会话结束时按 `actualCost` 结算，剩余退还
- 超额时返回 `40201 配额不足`

### 8.3 配额相关 Header

| Header | 说明 |
|---|---|
| X-RateLimit-Limit | 限流上限 |
| X-RateLimit-Remaining | 剩余可用次数 |
| X-RateLimit-Reset | 重置时间戳 |
| X-Quota-Remaining | 剩余配额 |
| X-Quota-Limit | 配额上限 |

---

## 9. 完整 curl 示例

### 9.1 xiaozhi-server-go 启动 → 申请 API Key → 创建 Session → Chat → 退 Session

```bash
# ========== 步骤 1：API Key 轮换（xiaozhi-server-go 启动时）==========
curl -X POST "https://api.aisaas.ykt.cn/internal/api/v1/apikeys/5001/rotate" \
  -H "X-Internal-Token: internal-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"expireDays": 30, "rotateStrategy": "time_24h"}'
# 响应：{ "newApiKey": "sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL", ... }

# ========== 步骤 2：创建会话（含配额快照）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/device-abc-001" \
  -H "Authorization: Bearer sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL" \
  -H "Content-Type: application/json" \
  -d '{
    "dimension": "llm_tokens_in",
    "quotaInitial": 2048,
    "personaId": 88
  }'
# 响应：{ "sessionId": "550e8400-e29b-41d4-a716-446655440002", "quotaSnapshot": {...}, ... }

# ========== 步骤 3：LLM 对话（OpenAI 兼容）==========
curl -X POST "https://api.aisaas.ykt.cn/v1/chat/completions" \
  -H "Authorization: Bearer sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'

# ========== 步骤 4：写入记忆（可选）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/messages" \
  -H "Authorization: Bearer sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL" \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "550e8400-e29b-41d4-a716-446655440002",
    "role": "user",
    "content": "今天天气真不错"
  }'

# ========== 步骤 5：结束会话（结算 + 退款）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/550e8400-e29b-41d4-a716-446655440002/end" \
  -H "Authorization: Bearer sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL" \
  -H "Content-Type: application/json" \
  -d '{
    "actualCost": {"llm_tokens_in": 1500, "llm_tokens_out": 800},
    "status": "success"
  }'
# 响应：{ "quotaRefunded": {"llm_tokens_in": 548, "llm_tokens_out": 0}, ... }
```

### 9.2 Portal 用户注册 → 充值 → 订阅套餐 → 设备绑定

> 以下为 Portal 端操作流程（JWT 鉴权）

```bash
# ========== 步骤 1：用户登录（获取 JWT）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username": "henry", "password": "xxx", "tenantCode": "acme"}'
# 响应：{ "data": {"tokenValue": "eyJhbGc...", "memberId": 1001, "tenantId": 100} }

# ========== 步骤 2：充值（幂等）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/billing/recharge" \
  -H "Authorization: Bearer eyJhbGc..." \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: client-uuid-xxx" \
  -d '{"amount": 1000, "paymentMethod": "wechat"}'

# ========== 步骤 3：订阅套餐（幂等）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/billing/subscription/change" \
  -H "Authorization: Bearer eyJhbGc..." \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: client-uuid-yyy" \
  -d '{"planCode": "standard"}'

# ========== 步骤 4：创建 API Key（供设备使用）==========
curl -X POST "https://api.aisaas.ykt.cn/api/v1/apikeys" \
  -H "Authorization: Bearer eyJhbGc..." \
  -H "Content-Type: application/json" \
  -d '{"name": "设备 Key", "scope": ["llm", "tts", "asr"]}'
# 响应：{ "apiKey": "sk-aisaas-9f3c5e2a8b1d4f6e0c2a4b6d8e0f2a4b", ... }

# ========== 步骤 5：查询设备绑定的 Persona==========
curl -G "https://api.aisaas.ykt.cn/api/v1/personas" \
  -H "Authorization: Bearer eyJhbGc..." \
  -d "deviceId=device-abc-001"
```

### 9.3 运营后台查询审计日志（追查某次失败）

```bash
# ========== 步骤 1：查询认证失败事件==========
curl -G "https://api.aisaas.ykt.cn/internal/api/v1/audit-logs" \
  -H "X-Internal-Token: internal-secret-token" \
  -d "action=auth.fail" \
  -d "result=failure" \
  -d "event_time_start=2026-09-01T00:00:00Z" \
  -d "event_time_end=2026-09-03T00:00:00Z" \
  -d "limit=50"
# 发现：Key 5001 在 12:30:00 出现 auth.fail

# ========== 步骤 2：按 traceId 追查完整调用链==========
curl -G "https://api.aisaas.ykt.cn/internal/api/v1/audit-logs" \
  -H "X-Internal-Token: internal-secret-token" \
  -d "traceId=abc123def456"
# 发现：auth.fail → apikey.revoke（运营手动撤销）

# ========== 步骤 3：紧急撤销该 Key（若未撤销）==========
curl -X POST "https://api.aisaas.ykt.cn/internal/api/v1/apikeys/5001/revoke" \
  -H "X-Internal-Token: internal-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"reason": "安全事件：Key 泄露"}'
```

---

## 10. 接口索引

| 模块 | 方法 | 路径 | 说明 |
|---|---|---|---|
| **Memory** | POST | `/api/v1/memories/{deviceId}/messages` | 写入短期会话消息 |
| **Memory** | GET | `/api/v1/memories/{deviceId}/messages` | 列出消息（分页） |
| **Memory** | GET | `/api/v1/memories/{deviceId}` | 拉取长期记忆图谱 |
| **Memory** | POST | `/api/v1/memories/{deviceId}/extract` | 异步抽取实体 |
| **Memory** | POST | `/api/v1/memories/{deviceId}/summarize` | 异步摘要聚合 |
| **Persona** | GET | `/api/v1/personas/{id}` | 按 ID 读取人设 |
| **Persona** | GET | `/api/v1/personas?deviceId=...` | 按设备查默认人设 |
| **Session** | POST | `/api/v1/sessions/{deviceId}` | 创建会话（含配额借记） |
| **Session** | GET | `/api/v1/sessions/{deviceId}/history` | 拉取会话历史 |
| **Session** | POST | `/api/v1/sessions/{sessionId}/end` | 结算 + 退款 |
| **APIKey** | POST | `/internal/api/v1/apikeys/{keyId}/rotate` | 轮换（5min 宽限） |
| **APIKey** | POST | `/internal/api/v1/apikeys/{keyId}/revoke` | 紧急撤销 |
| **Audit** | GET | `/internal/api/v1/audit-logs` | 审计日志查询 |

---

## 11. 待评审

- [ ] Memory extract/summarize 异步任务的状态轮询机制（推荐轮询间隔？）
- [ ] Session create 时 quotaSnapshot 的 tenantId 是否需要返回给调用方
- [ ] Audit 日志的 retention period 是否符合合规要求
- [ ] APIKey rotate 的 rotateStrategy 枚举值是否完整

---

## 附录：OpenAPI YAML 对应关系

| OpenAPI operationId | 本文档章节 | 路径 |
|---|---|---|
| writeMemoryMessage | §2.1 | `/api/v1/memories/{deviceId}/messages` (POST) |
| listMemoryMessages | §2.2 | `/api/v1/memories/{deviceId}/messages` (GET) |
| getMemoryGraph | §2.3 | `/api/v1/memories/{deviceId}` |
| extractMemoryEntities | §2.4 | `/api/v1/memories/{deviceId}/extract` |
| summarizeMemory | §2.5 | `/api/v1/memories/{deviceId}/summarize` |
| getPersona | §3.1 | `/api/v1/personas/{id}` |
| listPersonas | §3.2 | `/api/v1/personas` |
| createSession | §4.1 | `/api/v1/sessions/{deviceId}` |
| getSessionHistory | §4.2 | `/api/v1/sessions/{deviceId}/history` |
| endSession | §4.3 | `/api/v1/sessions/{sessionId}/end` |
| rotateApiKey | §5.1 | `/internal/api/v1/apikeys/{keyId}/rotate` |
| revokeApiKey | §5.2 | `/internal/api/v1/apikeys/{keyId}/revoke` |
| queryAuditLogs | §6.1 | `/internal/api/v1/audit-logs` |
