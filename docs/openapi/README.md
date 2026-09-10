# ykt-aisaas V2 API 文档

> **OpenAPI 规范**：`openapi/aisaas-v2.yaml`（OpenAPI 3.1.0）

## 概述

本文档覆盖 ykt-aisaas V2 新增接口，共 **13 个**，按模块分组：

| 模块 | 接口数 | 路径前缀 |
|------|--------|----------|
| [Memory](#memory-模块) | 5 | `/api/v1/memories/{deviceId}` |
| [Persona](#persona-模块) | 2 | `/api/v1/personas` |
| [Session](#session-模块) | 3 | `/api/v1/sessions/{deviceId}` |
| [API Key](#apikey-模块) | 2 | `/internal/api/v1/apikeys/{keyId}` |
| [Audit](#audit-模块) | 1 | `/internal/api/v1/audit-logs` |

---

## 鉴权方式速查

| 路径前缀 | 鉴权方式 | 适用场景 |
|----------|----------|----------|
| `/api/v1/*` | `Authorization: Bearer <api_key>` | SaaS 自有接口，API Key + scope 校验 |
| `/internal/api/v1/*` | `X-Internal-Token: <token>` + loopback IP | 内部超级租户接口（API Key 生命周期、审计） |
| `/portal/api/v1/*` | `Authorization: Bearer <jwt>` | 用户门户（沿用现有） |

---

## 错误响应格式

### SaaS 自有接口（/api/v1/*）

```json
{
  "code": 40001,
  "message": "参数错误",
  "requestId": "req_a1b2c3d4",
  "data": {}
}
```

### OpenAI 兼容接口（/v1/*）

```json
{
  "error": {
    "message": "Invalid API key",
    "type": "aisaas_error",
    "code": "40101"
  },
  "requestId": "req_a1b2c3d4"
}
```

### HTTP 状态码

| 状态码 | 含义 |
|--------|------|
| 200 | 成功 |
| 400 | 参数错误 / 请求体解析失败 |
| 401 | 认证失败（API Key 无效或已过期） |
| 402 | 配额不足 / 余额不足 |
| 403 | 权限不足 / 租户已冻结 |
| 404 | 资源不存在 |
| 409 | 资源已存在或状态冲突 |
| 429 | 请求过于频繁 |
| 500 | 系统内部错误 |
| 502/504 | 上游服务异常/超时 |

---

## Memory 模块

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/memories/{deviceId}/messages` | 写入短期会话消息 |
| `GET` | `/api/v1/memories/{deviceId}/messages` | 列出会话消息（分页） |
| `GET` | `/api/v1/memories/{deviceId}` | 拉取长期记忆图谱 |
| `POST` | `/api/v1/memories/{deviceId}/extract` | 异步抽取实体（Q3 决策） |
| `POST` | `/api/v1/memories/{deviceId}/summarize` | 异步摘要聚合 |

### WriteMessage 请求示例

```json
{
  "sessionId": "sess_abc123",
  "role": "user",
  "content": "今天天气真不错",
  "metadata": {
    "model": "gpt-4o",
    "tokens": 128
  }
}
```

### Extract 请求示例

```json
{
  "sessionId": "sess_abc123",
  "messages": [
    { "role": "user", "content": "我喜欢吃川菜" },
    { "role": "assistant", "content": "好的，记录您的偏好" }
  ],
  "extractTypes": ["entity", "preference", "event"]
}
```

---

## Persona 模块

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/v1/personas/{id}` | 读取人设详情 |
| `GET` | `/api/v1/personas?deviceId=...` | 按设备查默认 Persona（含 persona_bind） |

---

## Session 模块（Q8 决策：配额借记快照）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/sessions/{deviceId}` | 创建会话（含 quota_snapshot 借记） |
| `GET` | `/api/v1/sessions/{deviceId}/history` | 拉取最近 N 轮对话 |
| `POST` | `/api/v1/sessions/{sessionId}/end` | 结算实际消耗（含 quota.refund） |

### CreateSession 请求示例

```json
{
  "dimension": "llm_tokens_in",
  "quotaInitial": 2048,
  "personaId": 123,
  "metadata": {
    "ip": "192.168.1.1"
  }
}
```

**响应**（含 quota_snapshot）：

```json
{
  "sessionId": "uuid-v7",
  "deviceId": "device-001",
  "dimension": "llm_tokens_in",
  "quotaInitial": 2048,
  "quotaRemaining": 2048,
  "quotaSnapshot": {
    "tenantId": 100,
    "deviceQuotaLimit": 100000,
    "deviceQuotaUsed": 50000,
    "deviceQuotaRemaining": 50000,
    "snapshotTime": "2026-09-02T12:34:56.789Z"
  },
  "createdAt": "2026-09-02T12:34:56.789Z"
}
```

### EndSession 请求示例

```json
{
  "actualCost": {
    "llm_tokens_in": 1500,
    "llm_tokens_out": 800
  },
  "status": "success"
}
```

**响应**（含 quota.refund）：

```json
{
  "sessionId": "uuid-v7",
  "quotaUsed": {
    "llm_tokens_in": 1500,
    "llm_tokens_out": 800
  },
  "quotaRefunded": {
    "llm_tokens_in": 548
  },
  "endedAt": "2026-09-02T12:34:56.789Z"
}
```

---

## APIKey 模块（Q6 决策：生命周期管理）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/internal/api/v1/apikeys/{keyId}/rotate` | API Key 轮换（5 分钟宽限期） |
| `POST` | `/internal/api/v1/apikeys/{keyId}/revoke` | API Key 紧急撤销 |

### Rotate 请求示例

```json
{
  "expireDays": 1,
  "rotateStrategy": "time_24h"
}
```

**响应**：

```json
{
  "oldKeyId": 100,
  "oldKeyExpiresAt": "2026-09-02T17:34:56Z",
  "newKeyId": 101,
  "newApiKey": "sk-aisaas-K5x9mN2pQr4tUv8wXy3zA1bCdEfGhJkL",
  "newKeyPrefix": "sk-aisaas-K5x9mN2p...",
  "newKeyExpiresAt": "2026-09-03T12:34:56Z"
}
```

---

## Audit 模块（Q6 决策：审计可查）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/internal/api/v1/audit-logs` | 审计日志查询（限内部超级租户） |

### Query 参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `actor_type` | string | 行为者类型：device / user / api_key / system |
| `tenant_id` | int64 | 租户 ID 筛选 |
| `event_time_start` | datetime | 事件时间范围起（ISO 8601） |
| `event_time_end` | datetime | 事件时间范围止（ISO 8601） |
| `action` | string | 事件动作筛选 |
| `result` | string | 事件结果：success / failure |
| `limit` | int | 每页条数（默认 100，最大 500） |
| `cursor` | string | 游标分页 |

### Query 响应示例

```json
{
  "items": [
    {
      "eventId": "uuid-v7",
      "actorType": "device",
      "actorId": "device-mac-a1b2c3d4",
      "actorIp": "192.168.1.100",
      "action": "auth.fail",
      "actionDetail": { "reason": "API Key 过期" },
      "result": "failure",
      "eventTime": "2026-09-02T12:34:56.789Z",
      "traceId": "trace-xyz",
      "region": "cn-east-1"
    }
  ],
  "hasMore": true,
  "nextCursor": "encoded-cursor"
}
```

---

## 字段命名规范

- **统一使用 camelCase**（sessionId, quotaSnapshot, personaId）
- **时间格式**：ISO 8601（`2026-09-02T12:34:56.789Z`）
- **ID 格式**：
  - 数字型：`id`（int64）
  - 字符串型：UUID v7（sessionId、eventId、taskId）
  - Key 前缀：`keyPrefix`（显示用）

---

## 配额维度枚举

| 维度 | 说明 |
|------|------|
| `llm_tokens_in` | LLM 输入 Token |
| `llm_tokens_out` | LLM 输出 Token |
| `tts_chars` | TTS 字符数 |
| `asr_seconds` | ASR 秒数 |

---

## 变更历史

| 版本 | 日期 | 变更说明 |
|------|------|----------|
| 2.0.0 | 2026-09-02 | 初始 V2 规范，新增 Memory/Persona/Session/APIKey/Audit 模块 |
