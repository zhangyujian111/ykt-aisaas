# memory 模块说明

## 概述

`internal/tenantm/memory` 实现 ykt-aisaas V2 的三层记忆模型：

| 层级 | 存储 | 生命周期 | 用途 |
|------|------|---------|------|
| **短期** | `ykt_aisaas_session_message` | 7 天 TTL | 最近 N 轮对话上下文，填入 LLM messages |
| **摘要** | `ykt_aisaas_memory` (entityType='SUMMARY') | 90 天 | 会话主题聚类、关键事件、用户偏好摘要 |
| **长期图谱** | `ykt_aisaas_memory` + `ykt_aisaas_memory_relation` | 永久（软删除 + 遗忘策略） | 实体-关系网络，支持图谱推理 |

## 架构决策

| 决策 | 方案 | 依据 |
|------|------|------|
| **异步任务** | Redis Stream + Consumer Group | 复用 metering 模式，持久化、消费者组、分布式协调 |
| **抽取触发** | aisaas 内部自动触发 | 调用方无感知，WriteMessage 写入后自动检查 |
| **图谱存储** | MySQL | P1 实体量小，MySQL JOIN 足够 |
| **LLM 调用** | 复用现有 `/v1/chat/completions` | 通过 llm.Registry 解析模型后调用 |

## 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/memories/{deviceId}/messages` | 写入短期会话消息 |
| `GET` | `/api/v1/memories/{deviceId}/messages?limit=N&cursor=xxx` | 列出会话消息（游标分页） |
| `GET` | `/api/v1/memories/{deviceId}?limit=20&dimension=entity` | 拉取长期记忆图谱 |
| `POST` | `/api/v1/memories/{deviceId}/extract` | 异步抽取实体（返回 taskId） |
| `POST` | `/api/v1/memories/{deviceId}/summarize` | 异步摘要聚合（返回 taskId） |

## 文件结构

```
internal/tenantm/memory/
├── memory.go           # Service 核心：CRUD 操作
├── message.go          # AppendMessage + ListMessages 业务逻辑
├── summary.go          # SummarizeWorker：LLM 摘要处理
├── extract.go          # ExtractWorker：LLM 实体抽取处理
├── graph.go            # GetGraph：图谱查询 + 实体转换
├── worker.go           # Redis Stream Consumer 循环
├── llm.go              # LLM 抽取/摘要 prompt 模板 + 调用
├── config.go           # MemoryConfig + Stream 常量
└── types.go            # DO/Schema/请求响应类型

internal/server/apiv1/
└── memory.go           # 5 个 HTTP handler
```

## 异步任务机制

```
xaizhi-server-go 调 WriteMessage
        │
        ▼
WriteMessage 写入 session_message
        │
        │  检查触发条件（累计 ≥10 条新消息 OR 距上次抽取 ≥30min）
        │
        ▼
XADD aisaas:memory:extract:tasks  ──→ Worker XREADGROUP → LLM 抽取 → 写入 memory + memory_relation
XADD aisaas:memory:summarize:tasks ──→ Worker XREADGROUP → LLM 摘要 → 写入 memory (SUMMARY)
```

## 审计埋点

| 动作 | action | 触发点 |
|------|--------|--------|
| 写入消息 | `memory.append` | WriteMessage |
| 读取消息/图谱 | `memory.read` | ListMessages / GetGraph |
| 抽取实体 | `memory.extract` | ExtractAsync |
| 摘要聚合 | `memory.summarize` | SummarizeAsync |

## 配额预扣

- `memory.append`：按 `len(content)/3` 估算 token 数预扣 `llm_tokens_in`
- `memory.extract`：按 `len(messages)*100` 估算 token 数预扣 `llm_tokens_in`
- `memory.summarize`：固定 2000 token 预扣 `llm_tokens_in`
- 所有预扣使用 `quota.Guard.PrecheckDim`，配额不足返回 402

## 依赖

- **P0 基础设施**：`audit.Record` + `quota.Guard` + `metering.Recorder`
- **LLM 调用**：`llm.Registry` → `openaiClient.Client.Complete`
- **Redis**：`redisx.Client` Stream 操作
- **MySQL**：`gorm.DB` CRUD（租户插件自动注入 tenantId）