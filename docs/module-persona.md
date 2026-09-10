# Persona 模块

> **归属**：ykt-aisaas V2（Q1 决策：角色/人设下沉至 aisaas）
> **状态**：P1 已实现

## 概述

Persona 模块管理 AI 助手的角色/人设配置，包括：

- **Persona 主表**：存储人设的核心配置（system prompt、性格特征、关系阶段、模型参数等）
- **Persona 绑定表**：设备与 Persona 的绑定关系
- **System Prompt 生成器**：将 Persona 配置转为可直接注入 LLM 的 system prompt

## 数据模型

### ykt_aisaas_persona（人设主表）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT | 主键（雪花 ID） |
| tenantId | BIGINT | 租户 ID（NULL=系统预置） |
| code | VARCHAR(64) | 租户内唯一编码 |
| name | VARCHAR(128) | 人设名称 |
| systemPrompt | TEXT | 系统提示词（核心） |
| personalityTraits | JSON | 性格特征 `{"emotion_baseline":"...", "speaking_style":"..."}` |
| voicePreference | VARCHAR(64) | 音色偏好 |
| relationshipStages | JSON | 关系阶段定义 |
| defaultModelId | VARCHAR(64) | 默认模型 |
| temperature | DECIMAL(3,2) | 温度参数（默认 0.70） |
| status | TINYINT | 0禁用 1正常 2归档 |

### ykt_aisaas_persona_bind（绑定表）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT | 主键 |
| tenantId | BIGINT | 设备所属租户 |
| deviceId | VARCHAR(128) | 设备 ID（唯一约束） |
| personaId | BIGINT | 绑定的 Persona ID |
| bindType | VARCHAR(16) | default / strict |
| priority | INT | 优先级 |
| status | TINYINT | 0解绑 1绑定 |

## API 接口

### 1. `GET /api/v1/personas/{id}`

按 ID 读取人设详情。

**鉴权**：API Key Bearer（scope: `persona.read`）

**响应**：`Persona` 对象（含 systemPrompt、personalityTraits、模型参数等）

### 2. `GET /api/v1/personas?deviceId=...`

按设备查询绑定的 Persona。

**鉴权**：API Key Bearer（scope: `persona.read`）

**响应**：`ListPersonasResponse`（含 `PersonaWithBind` 对象）

## 内部 Service 方法（admin 用，不暴露 API）

| 方法 | 说明 |
|------|------|
| `Create(ctx, req)` | 创建 Persona |
| `Update(ctx, req)` | 更新 Persona |
| `Delete(ctx, id)` | 软删 Persona |
| `List(ctx)` | 列出 Persona |
| `Bind(ctx, req)` | 绑定设备到 Persona |
| `Unbind(ctx, deviceID)` | 解绑设备 |
| `ToSystemPrompt(ctx, persona)` | 生成 system prompt 字符串 |

## System Prompt 生成

`ToSystemPrompt` 方法将 Persona 配置转为格式化字符串：

```
你是 {name}。

【角色描述】
{description}

【核心指令】
{systemPrompt}

【性格特征】
{personalityTraits 格式化}

【关系阶段】
{relationshipStages 格式化}
```

模板可通过 `PersonaConfig.DefaultPromptTemplate` 自定义（Go template 语法）。

## 审计埋点

| 事件 | Action | 触发场景 |
|------|--------|---------|
| persona.read | persona.read | 每次 API 读取 |
| persona.create | persona.create | 创建 Persona |
| persona.update | persona.update | 更新 Persona |
| persona.delete | persona.delete | 删除 Persona |
| persona.bind | persona.bind | 绑定设备 |
| persona.unbind | persona.unbind | 解绑设备 |

## 与 xiaozhi-server-go 的集成

```
xiaozhi-server-go 启动
  → GET /api/v1/personas?deviceId={deviceId}
  → 获取 Persona（含 systemPrompt）
  → 缓存到内存（TTL 1h）
  → 对话时注入 system prompt：
     messages = [system: persona.SystemPrompt] + memory + history + userInput
```

## 与 V1 Java 实现对齐

| V1 Java (`agent` + `role` 表) | V2 (`persona` 表) |
|-------------------------------|-------------------|
| agent.system_prompt | persona.systemPrompt |
| agent.model | persona.defaultModelId |
| agent.temperature | persona.temperature |
| agent.attributes JSON | persona.personalityTraits JSON |
| role.relationship_stages | persona.relationshipStages |
| — | persona.personalityTraits（新增，V1 为隐式） |

## 文件清单

| 文件 | 职责 |
|------|------|
| `internal/tenantm/persona/types.go` | DO + API 请求/响应类型 |
| `internal/tenantm/persona/config.go` | PersonaConfig |
| `internal/tenantm/persona/persona.go` | Service + Repo（CRUD） |
| `internal/tenantm/persona/bind.go` | 设备绑定/解绑逻辑 |
| `internal/tenantm/persona/prompt.go` | SystemPrompt 生成器 |
| `internal/server/apiv1/persona.go` | 2 个 HTTP handler |