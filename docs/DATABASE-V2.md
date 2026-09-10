# DATABASE V2 设计文档

> 版本：v2.0（2026-09-02）
> 状态：待评审
> 关联：[DATABASE.md](./DATABASE.md) · [ARCHITECTURE.md](./ARCHITECTURE.md)

---

## 1. 表清单与分组

| Migration | 表 | 说明 |
|---|---|---|
| 000007 | `ykt_aisaas_persona` | Persona 人设配置 |
| 000007 | `ykt_aisaas_persona_bind` | 设备-Persona 绑定 |
| 000008 | `ykt_aisaas_session_message` | 短期会话消息 |
| 000008 | `ykt_aisaas_memory` | 长期记忆实体 |
| 000008 | `ykt_aisaas_memory_relation` | 记忆实体关系图谱 |
| 000009 | `ykt_aisaas_session` | 会话（含配额快照） |
| 000010 | `ykt_aisaas_audit_log` | 审计日志（分区表） |

---

## 2. ER 关系图

```
tenant
  ├── apikey
  ├── device (tenantType=DEVICE)
  │     └── persona_bind → persona
  │           └── session → session_message
  │                         └── memory (实体)
  │                               └── memory_relation (关系图谱)
  └── audit_log (跨租户统一审计)

persona
  ├── voice_library (音色偏好)
  ├── knowledge_base (默认知识库)
  └── model_registry (默认模型)
```

---

## 3. 关键设计决策

### 3.1 Persona 表（000007）

**字段设计**：
- `systemPrompt TEXT` — 人设核心系统提示词
- `personalityTraits JSON` — 性格特征（emotion_baseline, speaking_style 等）
- `voicePreference VARCHAR(64)` — 音色偏好（引用 voice_library.id）
- `relationshipStages JSON` — 关系阶段定义（Q1 决策）
- `defaultModelId VARCHAR(64)` — 默认模型
- `defaultKnowledgeBaseIds JSON` — 默认知识库 ID 列表

**状态机**：
- `0` = 禁用
- `1` = 正常
- `2` = 归档

**多租户**：
- `tenantId NULL` = 系统预置（如 xiaozhi 迁移过来的角色）
- `tenantId NOT NULL` = 租户私有 Persona

**Persona_bind 设计**：
- `deviceId UNIQUE KEY` — 一台设备只能绑定一个默认 Persona
- `bindCondition JSON` — 可选绑定条件（externalUserId, timeRange 等）
- 支持 `strict` 严格匹配模式

### 3.2 Memory 三层模型（000008）

```
短期记忆 (session_message)
    ↓ (对话结束后可选提炼)
摘要记忆 (memory.content.summary)
    ↓ (高重要性实体化)
长期记忆 (memory + memory_relation)
```

**session_message（短期）**：
- 高写入，只追加不更新
- 按 `sessionId + createTime` 索引
- 建议 TTL 7 天后转储到 memory

**memory（长期实体）**：
- `entityType`：`PERSON/EVENT/PREFERENCE/OBJECT/LOCATION/KNOWLEDGE`
- `entityKey`：设备内唯一标识
- `importance 0-1 FLOAT`：用于摘要/遗忘策略
- `version INT`：乐观锁，支持最终一致性

**memory_relation（关系图谱）**：
- 三元组：`sourceEntityId + relationType + targetEntityId`
- `relationType`：knows/likes/participated_in/owns/is_a/part_of 等
- `weight 0-1 FLOAT`：关系强度

### 3.3 Session 表配额快照机制（000009）

**Q8 决策核心**：

```
会话创建 → quotaSnapshot 快照（设备当前配额完整记录）
         → quotaInitial = quotaSnapshot[quotaDimension]
         → quotaUsed = 0

流式对话 → 每次 token 计量 → quotaUsed += N

会话结束 → status=3 已结束
         → actualCost = {各维度实际消耗, cost_cents}

配额清算 → status=4 已结算
```

**字段说明**：
- `quotaSnapshot JSON` — 创建时的设备配额完整快照
- `quotaDimension VARCHAR` — 主借记维度（展示/告警用）
- `quotaInitial INT64` — 创建时该维度的配额上限
- `quotaUsed INT64` — 流式更新的已用量
- `actualCost JSON` — 结算时的各维度实际消耗

### 3.4 Audit Log 分区策略（000010）

**Q6 决策核心**：

**UUID v7 优势**：
- 时间有序（适合分区裁剪 + 日志时序查询）
- 唯一性保证（时间戳 + 随机数）
- 可在客户端生成（减少 DB 序列依赖）

**分区设计**：
```sql
PARTITION BY RANGE (TO_DAYS(eventTime)) (
  p_2024_01 ... p_2026_12,
  p_future VALUES LESS THAN MAXVALUE
)
```

**索引设计**：
| 索引 | 用途 |
|---|---|
| `idx_tenant_time` | 按租户查审计记录 |
| `idx_actor_time` | 按操作者查行为轨迹 |
| `idx_action_time` | 按动作类型统计分析 |
| `idx_trace` | 分布式链路追踪 |
| `idx_resource` | 按资源查关联操作 |

**保留策略**：
- 热数据：365 天（InnoDB 分区本地管理）
- 冷数据：归档到 MinIO/OSS（通过分区交换 `EXCHANGE PARTITION`）

**脱敏处理**：
- `errorMessage` 不记录用户数据/上下文
- `actionDetail` 脱敏后存储（如密码字段置空）

---

## 4. 与现有 Schema 的兼容性

### 4.1 ALTER 现有表

**无需 ALTER 现有表**，新表通过外键引用现有表：

- `ykt_aisaas_session.personaId` → `ykt_aisaas_persona.id`（逻辑外键，无 FK 约束）
- `ykt_aisaas_session.deviceId` → `ykt_aisaas_tenant.deviceId`（设备租户）
- `ykt_aisaas_memory.deviceId` → `ykt_aisaas_tenant.deviceId`

### 4.2 新建初始化数据

**无需 seed migration**，系统预置 Persona 由 xiaozhi 迁移脚本处理（见 DATABASE.md 10.2.2 节）。

---

## 5. 索引设计理由

| 表 | 索引 | 理由 |
|---|---|---|
| `persona` | `uk_tenant_code` | 租户内编码唯一 |
| `persona` | `idx_tenant` | 按租户查询 persona 列表 |
| `persona_bind` | `uk_device` | 设备只能绑定一个默认 persona |
| `session_message` | `idx_session_time` | 按会话查消息历史（高频） |
| `memory` | `uk_device_entity` | 设备内实体唯一 |
| `memory` | `idx_importance` | 重要性排序（摘要候选） |
| `memory_relation` | `uk_relation` | 同一三元组不重复 |
| `session` | `idx_device_active` | 设备活跃会话查询 |
| `audit_log` | `idx_tenant_time` | 审计查询最常见模式 |

---

## 6. 迁移顺序

```sql
-- 执行顺序（依赖关系）
000001_init          -- 核心表（已有）
000002_seed          -- 初始化数据（已有）
000003_knowledge     -- 知识库（已有）
000004_mcp           -- MCP（已有）
000005_billing       -- 计费（已有）
000006_device_tenant -- 设备租户（已有）
000007_persona       -- Persona（新增）
000008_memory        -- Memory（新增，依赖 persona）
000009_session       -- Session（新增，依赖 persona）
000010_audit_log     -- Audit Log（新增，独立）
```

---

## 7. 待确认事项

- [ ] `ykt_aisaas_audit_log.eventId` 是否需要生成函数（UUID v7）？
- [ ] Session 的 `actualCost` 触发时机（状态机 3→4）？
- [ ] Memory 的 TTL 策略（7 天/30 天/永久）？
- [ ] Audit Log 分区交换归档策略（定时任务 vs 手动）？

---

## 8. 参考文档

- [DATABASE.md](./DATABASE.md) — v1.1 数据库设计
- xiaozhi-java `MemoryService` / `SessionService` 接口（待对齐）
- xiaozhi-server `sys_role` → `ykt_aisaas_persona` 迁移脚本（见 DATABASE.md 10.2.2）
