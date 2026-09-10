# ykt-aisaas V2 API 快速入门

> 本文档为 V2 新接口的快速导航，完整文档见 [API-V2.md](./API-V2.md)

---

## 快速链接

| 资源 | 链接 |
|---|---|
| **OpenAPI YAML** | [openapi/aisaas-v2.yaml](./openapi/aisaas-v2.yaml) |
| **完整 API 文档** | [API-V2.md](./API-V2.md)（V2 新增接口） |
| **V1 API 文档** | [API.md](./API.md)（V1 原有接口） |
| **Swagger UI** | `https://api.aisaas.ykt.cn/swagger-ui.html` |
| **架构文档** | [ARCHITECTURE.md](./ARCHITECTURE.md) |

---

## 模块索引

### 按用途分组

| 用途 | 模块 | 接口数量 | 主要场景 |
|---|---|---|---|
| **对话上下文存储** | Memory | 5 | xiaozhi 设备写入对话、查询历史、拉取图谱 |
| **人设加载** | Persona | 2 | 设备启动时加载默认人设 |
| **配额管控** | Session | 3 | 创建会话预借配额、结算退款 |
| **Key 生命周期** | APIKey | 2 | 启动时轮换、紧急撤销 |
| **安全审计** | Audit | 1 | 运营后台查询操作记录 |

### 按接口路径分组

| 前缀 | 模块 | 说明 |
|---|---|---|
| `/api/v1/memories/*` | Memory | 记忆存储与图谱 |
| `/api/v1/personas/*` | Persona | 人设查询 |
| `/api/v1/sessions/*` | Session | 会话与配额 |
| `/internal/api/v1/apikeys/*` | APIKey | Key 轮换与撤销 |
| `/internal/api/v1/audit-logs` | Audit | 审计日志 |

---

## "我应该用哪个接口"决策树

```
你是谁？
│
├─ xiaozhi-server-go（设备后端）
│   │
│   ├─ 启动时需要 API Key
│   │   └─ → POST /internal/api/v1/apikeys/{keyId}/rotate
│   │
│   ├─ 对话前需要创建会话
│   │   └─ → POST /api/v1/sessions/{deviceId}
│   │
│   ├─ 对话后写入记忆
│   │   └─ → POST /api/v1/memories/{deviceId}/messages
│   │
│   └─ 对话结束需要结算
│       └─ → POST /api/v1/sessions/{sessionId}/end
│
├─ Portal 用户（Web/App）
│   │
│   ├─ 查看会话历史
│   │   └─ → GET /api/v1/sessions/{deviceId}/history
│   │
│   ├─ 查看长期记忆图谱
│   │   └─ → GET /api/v1/memories/{deviceId}
│   │
│   ├─ 查询人设
│   │   └─ → GET /api/v1/personas?deviceId=...
│   │
│   └─ 管理 API Key（租户自管理）
│       └─ → 参见 API.md §4.4（/api/v1/apikeys）
│
└─ 运营后台（平台管理）
    │
    ├─ 轮换设备 Key
    │   └─ → POST /internal/api/v1/apikeys/{keyId}/rotate
    │
    ├─ 紧急撤销 Key
    │   └─ → POST /internal/api/v1/apikeys/{keyId}/revoke
    │
    └─ 查询审计日志
        └─ → GET /internal/api/v1/audit-logs
```

---

## 5 分钟快速上手

### 场景：xiaozhi-server-go 完整调用链

```bash
# 1. 轮换获得 API Key
curl -X POST "https://api.aisaas.ykt.cn/internal/api/v1/apikeys/5001/rotate" \
  -H "X-Internal-Token: internal-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"expireDays": 30}'

# 2. 创建会话（获得 sessionId）
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/device-abc-001" \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{"dimension": "llm_tokens_in", "quotaInitial": 2048}'

# 3. LLM 对话（OpenAI 兼容）
curl -X POST "https://api.aisaas.ykt.cn/v1/chat/completions" \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "你好"}]}'

# 4. 写入记忆
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/messages" \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "session-id-from-step-2", "role": "user", "content": "你好"}'

# 5. 结束会话（结算退款）
curl -X POST "https://api.aisaas.ykt.cn/api/v1/sessions/{sessionId}/end" \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{"actualCost": {"llm_tokens_in": 1500}, "status": "success"}'
```

---

## 鉴权速查

| 场景 | Header | 示例 |
|---|---|---|
| API Key 调 V2 接口 | `Authorization: Bearer sk-aisaas-xxx` | `Authorization: Bearer sk-aisaas-K5x9mN2p...` |
| 内部接口（运营后台） | `X-Internal-Token: xxx` | `X-Internal-Token: internal-secret-token` |
| Portal 用户（JWT） | `Authorization: Bearer eyJhbGc...` | 登录后获取 |

---

## 常见错误处理

| 错误码 | HTTP | 场景 | 处理方式 |
|---|---|---|---|
| 40101 | 401 | API Key 无效 | 检查 Key 是否正确，是否已过期 |
| 40201 | 402 | 配额不足 | 创建 session 时借记失败，降低 quotaInitial |
| 40901 | 409 | 资源已存在 | 设备已有活跃会话，先调用 endSession |
| 40203 | 429 | 请求过于频繁 | 等待 retryAfterSeconds 后重试 |

---

## 配额工作机制（Session 模块）

```
创建会话（借记）
  quotaRemaining = deviceQuotaRemaining - quotaInitial
       │
       ▼
   使用配额（LLM/TTS/ASR 调用）
       │
       ▼
   结束会话（退款）
  quotaRefunded = quotaInitial - actualCost
```

**示例**：
- 设备配额：100000 tokens
- 已使用：1024 tokens
- 创建会话借记：2048 tokens
- 实际消耗：1500 tokens
- 退还：2048 - 1500 = 548 tokens
- 会话结束后设备剩余：100000 - 1024 - 1500 = 97476 tokens

---

## 异步任务（Memory extract/summarize）

```bash
# 触发实体抽取
curl -X POST "https://api.aisaas.ykt.cn/api/v1/memories/device-abc-001/extract" \
  -H "Authorization: Bearer sk-aisaas-xxx" \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "user", "content": "我儿子叫小明"}], "extractTypes": ["entity"]}'
# 响应：{ "taskId": "xxx", "status": "pending" }

# 建议等待 10 秒后查询状态（实际任务状态需参考任务查询接口）
```

> 异步任务不返回即时结果，返回 `taskId` 供后续查询或回调。

---

## 变更日志

| 版本 | 日期 | 变更说明 |
|---|---|---|
| v2.0 | 2026-09-02 | 初始 V2 接口文档 |
