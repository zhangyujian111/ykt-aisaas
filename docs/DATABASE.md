# YKT AI SaaS 平台 · 数据库设计

> 版本：**v1.1（2026-08-12）** — 增加内部租户表 + xiaozhi 数据迁移章节
> 状态：MVP 基线 · 待评审
> 关联：[ARCHITECTURE.md](./ARCHITECTURE.md) · [MODULES.md](./MODULES.md) · [API.md](./API.md) · [CHANGELOG.md](./CHANGELOG.md)

---

## 0. 设计原则

### 0.1 命名规范（**与 ykt-admin / xiaozhi-server 一致**）

| 项 | 规范 | 示例 |
|---|---|---|
| 表前缀 | `ykt_aisaas_` | `ykt_aisaas_tenant` |
| 表名 | 小写下划线（业务域_实体） | `ykt_aisaas_async_job` |
| 字段名 | **camelCase**（不开下划线转驼峰） | `tenantId`, `createTime`, `apiKey` |
| 主键 | `id BIGINT`（雪花 ID） | - |
| 时间字段 | `DATETIME`，默认 `CURRENT_TIMESTAMP` | `createTime DATETIME` |
| 软删除 | `isDeleted TINYINT DEFAULT 0` | - |
| 审计字段 | `createTime` / `updateTime` / `createBy` / `updateBy` | - |
| 多租户字段 | `tenantId BIGINT NOT NULL`（系统表除外） | - |

### 0.2 通用字段约定（每张业务表必含）

```sql
id          BIGINT       NOT NULL  COMMENT '主键',
tenantId    BIGINT       NOT NULL  COMMENT '租户ID（系统表豁免）',
createTime  DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP              COMMENT '创建时间',
updateTime  DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
createBy    VARCHAR(64)            DEFAULT 'system'                       COMMENT '创建人',
updateBy    VARCHAR(64)            DEFAULT 'system'                       COMMENT '更新人',
isDeleted   TINYINT      NOT NULL  DEFAULT 0                              COMMENT '0未删 1已删',
PRIMARY KEY (id),
KEY idx_tenant (tenantId, isDeleted)
```

### 0.3 多租户豁免表清单

以下表**不带 tenantId**（系统表）：

| 表 | 说明 |
|---|---|
| `ykt_aisaas_tenant` | 租户主表 |
| `ykt_aisaas_plan` | 套餐定义 |
| `ykt_aisaas_dict_type` / `ykt_aisaas_dict_item` | 字典 |
| `ykt_aisaas_admin_user` / `ykt_aisaas_admin_role` | 平台运营人员 |
| `ykt_aisaas_model_registry`（部分行） | `tenantId IS NULL` 表示全局共享模型 |

---

## 1. DB 实例规划

### 1.1 实例清单（**同主机部署**）

| DB | 引擎 | 端口 | 数据库 | 用途 |
|---|---|---|---|---|
| MySQL 8.0 | InnoDB | 3306 | `ykt_aisaas` | 业务主库（租户/计费/任务/配置） |
| PostgreSQL 16 | +PgVector 扩展 | 5432 | `ykt_aisaas_rag` | 向量库（RAG） |
| Redis 7 | - | 6379 | db 0-15 | 缓存/配额/Token/分布式锁 |
| MinIO | - | 9000/9001 | `ykt-aisaas` bucket | 音频/文档/模型权重 |

### 1.2 字符集

```sql
-- MySQL
ALTER DATABASE ykt_aisaas CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- PostgreSQL（PgVector）
CREATE DATABASE ykt_aisaas_rag WITH ENCODING 'UTF8';
\c ykt_aisaas_rag
CREATE EXTENSION IF NOT EXISTS vector;
```

### 1.3 数据量预估（MVP，10 租户 × 1000 用户）

| 表 | 月增量 | 一年累计 | 备注 |
|---|---|---|---|
| `ykt_aisaas_message` | ~1000 万 | 1.2 亿 | 按月分表（P1） |
| `ykt_aisaas_usage_detail` | ~3000 万 | 3.6 亿 | 按月分表（P1） |
| `ykt_aisaas_async_job_log` | ~10 万 | 120 万 | 不分表 |
| 其他配置表 | <1 万 | <10 万 | 不分表 |

**MVP 不分表**，预留按 `createTime` 月度分表的能力（P1 实施）。

---

## 2. 表清单（按业务域）

| 域 | 表 | 说明 | 行数预估 |
|---|---|---|---|
| **租户** | `ykt_aisaas_tenant` | 租户主表 | 千级 |
| | `ykt_aisaas_tenant_member` | 租户成员 | 万级 |
| | `ykt_aisaas_tenant_role` | 租户内角色 | 千级 |
| | `ykt_aisaas_tenant_member_role` | 成员-角色关联 | 万级 |
| **鉴权** | `ykt_aisaas_apikey` | API Key | 万级 |
| **计费** | `ykt_aisaas_plan` | 套餐定义 | 十级 |
| | `ykt_aisaas_subscription` | 租户订阅 | 千级 |
| | `ykt_aisaas_balance` | 余额账户 | 千级 |
| | `ykt_aisaas_balance_transaction` | 余额流水 | 万级/月 |
| | `ykt_aisaas_quota` | 配额（月度） | 千级/月 |
| | `ykt_aisaas_usage_detail` | 计量明细 | 千万级/月 |
| | `ykt_aisaas_bill` | 月度账单 | 千级/月 |
| **模型** | `ykt_aisaas_model_registry` | 模型注册表 | 百级 |
| | `ykt_aisaas_tenant_model_config` | 租户模型配置 | 万级 |
| **对话** | `ykt_aisaas_conversation` | 会话 | 百万级/月 |
| | `ykt_aisaas_message` | 消息 | 千万级/月 |
| **RAG** | `ykt_aisaas_knowledge_base` | 知识库 | 万级 |
| | `ykt_aisaas_knowledge_document` | 文档 | 十万级 |
| | `ykt_aisaas_rag_chunk_tenant_{tid}_kb_{kbid}` | 切片+向量（PG） | 千万级 |
| **语音** | `ykt_aisaas_voice_library` | 声音库 | 千级 |
| | `ykt_aisaas_audio_task` | TTS/ASR 任务 | 百万级/月 |
| **长任务** | `ykt_aisaas_async_job` | 异步任务主表 | 万级/月 |
| | `ykt_aisaas_async_job_log` | 任务日志 | 十万级/月 |
| **MCP** | `ykt_aisaas_mcp_server` | MCP Server 注册 | 百级 |
| | `ykt_aisaas_mcp_tool` | MCP 工具 | 千级 |
| | `ykt_aisaas_tenant_mcp_binding` | 租户-MCP 绑定 | 万级 |
| **Persona** | `ykt_aisaas_persona` | 性格人设 | 千级 |
| | `ykt_aisaas_persona_voice_binding` | Persona-声音绑定 | 千级 |
| **系统** | `ykt_aisaas_admin_user` | 平台运营人员 | 十级 |
| | `ykt_aisaas_admin_role` | 平台角色 | 个级 |
| | `ykt_aisaas_dict_type` / `_item` | 字典 | 百级 |
| | `ykt_aisaas_operation_log` | 操作审计 | 万级/月 |
| | `ykt_aisaas_login_log` | 登录日志 | 千级/月 |

---

## 3. 完整 DDL

### 3.1 租户域

```sql
-- 租户主表（系统表，无 tenantId）
CREATE TABLE ykt_aisaas_tenant (
  id              BIGINT       NOT NULL  COMMENT '租户ID',
  code            VARCHAR(64)  NOT NULL  COMMENT '租户编码（用于 URL/日志）',
  name            VARCHAR(128) NOT NULL  COMMENT '租户名称',
  contactName     VARCHAR(64)            COMMENT '联系人',
  contactPhone    VARCHAR(32)            COMMENT '联系电话',
  contactEmail    VARCHAR(128)           COMMENT '联系邮箱',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0禁用 1正常 2冻结',
  planId          BIGINT                COMMENT '当前套餐ID（冗余，便于查询）',
  expireTime      DATETIME              COMMENT '套餐到期时间',
  remark          VARCHAR(512)           COMMENT '备注',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code),
  KEY idx_status (status, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户主表';

-- 租户成员表
CREATE TABLE ykt_aisaas_tenant_member (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL  COMMENT '租户ID',
  username        VARCHAR(64)  NOT NULL  COMMENT '登录名（租户内唯一）',
  password        VARCHAR(128) NOT NULL  COMMENT 'BCrypt 加密',
  nickname        VARCHAR(64)            COMMENT '昵称',
  email           VARCHAR(128)           COMMENT '邮箱',
  phone           VARCHAR(32)            COMMENT '手机',
  avatar          VARCHAR(512)           COMMENT '头像 URL',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0禁用 1正常',
  lastLoginTime   DATETIME              COMMENT '最后登录',
  lastLoginIp     VARCHAR(64)            COMMENT '最后登录 IP',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_username (tenantId, username),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户成员';

-- 租户内角色（与平台运营角色 ykt_aisaas_admin_role 隔离）
CREATE TABLE ykt_aisaas_tenant_role (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  code            VARCHAR(64)  NOT NULL  COMMENT '角色编码（租户内唯一）',
  name            VARCHAR(64)  NOT NULL,
  permissions     JSON                   COMMENT '权限点列表 ["llm:call","tts:call"]',
  remark          VARCHAR(256),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_code (tenantId, code),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户角色';

-- 成员-角色关联
CREATE TABLE ykt_aisaas_tenant_member_role (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  memberId        BIGINT       NOT NULL,
  roleId          BIGINT       NOT NULL,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_member_role (memberId, roleId),
  KEY idx_tenant (tenantId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成员角色关联';

-- ⭐ v1.1 新增：内部超级租户配置（如 xiaozhi-server）
CREATE TABLE ykt_aisaas_internal_tenant_config (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL  COMMENT '关联 ykt_aisaas_tenant.id',
  tenantType      VARCHAR(32)  NOT NULL  COMMENT 'xiaozhi/iot_hub/third_party_internal',
  isUnlimited     TINYINT      NOT NULL  DEFAULT 0 COMMENT '是否不限配额（内部租户通常为 1）',
  rateLimitOverride JSON                 COMMENT '覆盖默认限流 {"llmQps":500,"ttsConcurrent":1000}',
  internalApiKey  VARCHAR(64)  NOT NULL  COMMENT '内部调用专用 Key（不走常规配额扣减）',
  webhookUrl      VARCHAR(512)           COMMENT '事件回调（如设备事件）',
  metadata        JSON                   COMMENT '扩展字段',
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant (tenantId),
  UNIQUE KEY uk_internal_key (internalApiKey),
  KEY idx_type (tenantType, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='内部超级租户配置（v1.1 新增）';

-- ⭐ v1.1 新增：设备-会话绑定（内部租户用，外部租户可选）
-- 用途：xiaozhi-server 把设备对话上下文绑定到平台 conversation，便于跨调用复用
CREATE TABLE ykt_aisaas_device_session (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL  COMMENT '内部租户 ID（如 xiaozhi=1）',
  deviceId        VARCHAR(128) NOT NULL  COMMENT '设备唯一标识（xiaozhi sys_device.id 或 deviceCode）',
  conversationId  BIGINT                 COMMENT '关联 ykt_aisaas_conversation.id',
  personaId       BIGINT                 COMMENT '设备绑定的 Persona（xiaozhi sys_role 对应）',
  externalUserId  VARCHAR(64)            COMMENT '终端用户标识（设备拥有者）',
  lastActiveTime  DATETIME,
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0已解绑 1正常',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_device (tenantId, deviceId),
  KEY idx_conv (conversationId),
  KEY idx_active (tenantId, lastActiveTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='设备-会话绑定（v1.1 新增，内部租户用）';
```

### 3.2 鉴权域

```sql
-- API Key（租户调用平台 API 的密钥）
CREATE TABLE ykt_aisaas_apikey (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  name            VARCHAR(64)  NOT NULL  COMMENT 'Key 名称（用户起的名）',
  apiKey          VARCHAR(64)  NOT NULL  COMMENT '完整 Key（sk-aisaas-{32位}）',
  apiKeyHash      VARCHAR(128) NOT NULL  COMMENT 'SHA-256(apiKey)，索引用',
  keyPrefix       VARCHAR(16)  NOT NULL  COMMENT '前 8 位，用于 UI 显示',
  scope           JSON                   COMMENT '权限范围 ["llm","tts","asr","rag","mcp"]',
  ipWhitelist     JSON                   COMMENT 'IP 白名单 ["1.2.3.4/24"]',
  expiresAt       DATETIME              COMMENT '过期时间，NULL=永久',
  lastUsedAt      DATETIME              COMMENT '最后使用',
  lastUsedIp      VARCHAR(64),
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0禁用 1正常',
  createdBy       BIGINT       NOT NULL  COMMENT '创建成员 ID',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_apikey_hash (apiKeyHash),
  KEY idx_tenant (tenantId, isDeleted),
  KEY idx_prefix (keyPrefix)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API Key';
```

### 3.3 计费域

```sql
-- 套餐（系统表）
CREATE TABLE ykt_aisaas_plan (
  id              BIGINT       NOT NULL,
  code            VARCHAR(64)  NOT NULL  COMMENT '套餐编码 free/standard/enterprise',
  name            VARCHAR(64)  NOT NULL,
  priceMonthly    DECIMAL(10,2) NOT NULL  DEFAULT 0 COMMENT '月费',
  priceYearly     DECIMAL(10,2) NOT NULL  DEFAULT 0 COMMENT '年费',
  quotas          JSON                   COMMENT '套餐配额 {"llmTokensMonthly":1000000,"ttsCharsMonthly":100000,...}',
  features        JSON                   COMMENT '功能白名单 ["voice_clone","finetune"]',
  status          TINYINT      NOT NULL  DEFAULT 1,
  remark          VARCHAR(512),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='套餐';

-- 租户订阅记录
CREATE TABLE ykt_aisaas_subscription (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  planId          BIGINT       NOT NULL,
  periodStart     DATETIME     NOT NULL  COMMENT '当前周期开始',
  periodEnd       DATETIME     NOT NULL  COMMENT '当前周期结束',
  autoRenew       TINYINT      NOT NULL  DEFAULT 0 COMMENT '自动续费',
  payMethod       VARCHAR(32)            COMMENT 'alipay/wechat/balance',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0已取消 1生效 2已过期',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_period (tenantId, periodEnd),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户订阅';

-- 余额账户（每租户一行）
CREATE TABLE ykt_aisaas_balance (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  balanceCents    BIGINT       NOT NULL  DEFAULT 0 COMMENT '余额（分），RMB',
  frozenCents     BIGINT       NOT NULL  DEFAULT 0 COMMENT '冻结（分）',
  totalRecharged  BIGINT       NOT NULL  DEFAULT 0 COMMENT '累计充值',
  totalConsumed   BIGINT       NOT NULL  DEFAULT 0 COMMENT '累计消费',
  version         INT          NOT NULL  DEFAULT 0  COMMENT '乐观锁',
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant (tenantId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='余额账户';

-- 余额流水
CREATE TABLE ykt_aisaas_balance_transaction (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  type            VARCHAR(32)  NOT NULL  COMMENT 'recharge/consume/refund/freeze',
  amountCents     BIGINT       NOT NULL  COMMENT '正数=入账 负数=出账',
  balanceAfter    BIGINT       NOT NULL  COMMENT '操作后余额',
  bizType         VARCHAR(32)            COMMENT 'llm/tts/asr/rag/voice_clone/finetune',
  bizRefId        VARCHAR(64)            COMMENT '业务流水号',
  remark          VARCHAR(256),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_biz (bizType, bizRefId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='余额流水';

-- 配额（按月）—— Redis 实时计数，月初从此表加载
CREATE TABLE ykt_aisaas_quota (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  periodStart     DATE         NOT NULL  COMMENT '周期起始日（按月）',
  periodEnd       DATE         NOT NULL,
  dimension       VARCHAR(32)  NOT NULL  COMMENT 'llm_tokens_in/llm_tokens_out/tts_chars/asr_seconds/rag_calls/voice_clone_minutes/finetune_gpu_hours',
  limitValue      BIGINT       NOT NULL  COMMENT '套餐+叠加包总量',
  usedValue       BIGINT       NOT NULL  DEFAULT 0 COMMENT '已用（每日从 Redis 同步）',
  overagePolicy   VARCHAR(16)  NOT NULL  DEFAULT 'reject' COMMENT 'reject=拒绝 overage=超额按量',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_period_dim (tenantId, periodStart, dimension)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='配额';

-- 计量明细（高写入表，按月分表预留）
CREATE TABLE ykt_aisaas_usage_detail (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  apiKeyId        BIGINT                  COMMENT '调用的 API Key',
  bizType         VARCHAR(32)  NOT NULL  COMMENT 'llm/tts/asr/rag/embedding/voice_clone/finetune',
  dimension       VARCHAR(32)  NOT NULL  COMMENT 'tokens_in/tokens_out/chars/seconds/calls/minutes/gpu_hours',
  amount          BIGINT       NOT NULL  COMMENT '本次用量',
  costCents       BIGINT       NOT NULL  COMMENT '本次成本（分）',
  modelId         VARCHAR(64)            COMMENT '使用的模型',
  requestId       VARCHAR(64)            COMMENT '请求 ID',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0失败 1成功',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_biz_time (tenantId, bizType, createTime),
  KEY idx_request (requestId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='计量明细';

-- 月度账单（定时任务月初生成）
CREATE TABLE ykt_aisaas_bill (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  periodStart     DATE         NOT NULL,
  periodEnd       DATE         NOT NULL,
  planFeeCents    BIGINT       NOT NULL  COMMENT '套餐费',
  usageFeeCents   BIGINT       NOT NULL  COMMENT '超额按量费',
  totalFeeCents   BIGINT       NOT NULL  COMMENT '应付总额',
  paidCents       BIGINT       NOT NULL  DEFAULT 0 COMMENT '已付',
  usageBreakdown  JSON                   COMMENT '{"llm":{...},"tts":{...}}',
  status          TINYINT      NOT NULL  DEFAULT 0  COMMENT '0待付 1已付 2部分付 3已逾期',
  payTime         DATETIME,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_period (tenantId, periodStart),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='月度账单';
```

### 3.4 模型域

```sql
-- 模型注册表（tenantId IS NULL = 全局模型，租户可订阅；非 NULL = 私有模型如微调产出）
CREATE TABLE ykt_aisaas_model_registry (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT 'NULL=全局共享，非NULL=租户私有',
  modelId         VARCHAR(64)  NOT NULL  COMMENT '业务模型ID（对外暴露）"gpt-4o-mini"',
  provider        VARCHAR(32)  NOT NULL  COMMENT 'openai/deepseek/zhipu/dashscope/aliyun-tts/edge-tts/aliyun-asr/self-hosted',
  baseUrl         VARCHAR(256) NOT NULL,
  apiKeyEnc       VARCHAR(512) NOT NULL  COMMENT '加密后的 API Key',
  upstreamModel   VARCHAR(64)  NOT NULL  COMMENT '上游真实模型名"gpt-4o-mini-2024-07-18"',
  modality        JSON         NOT NULL  COMMENT '["text","vision","function","audio-in"]',
  type            VARCHAR(16)  NOT NULL  COMMENT 'chat/embedding/tts/asr/rerank',
  contextLength   INT                    COMMENT '上下文长度',
  priceInputCents DECIMAL(10,4)          COMMENT '每千 token 输入价（分）',
  priceOutputCents DECIMAL(10,4)         COMMENT '每千 token 输出价（分）',
  isStream        TINYINT      NOT NULL  DEFAULT 1,
  isDefault       TINYINT      NOT NULL  DEFAULT 0 COMMENT '是否该 type 默认模型',
  capabilities    JSON                   COMMENT '额外能力 {"emotion":true,"voiceClone":true}',
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_modelid (tenantId, modelId),
  KEY idx_provider_type (provider, type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模型注册表';

-- 租户模型配置（哪些模型可用、各自限速）
CREATE TABLE ykt_aisaas_tenant_model_config (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  modelId         VARCHAR(64)  NOT NULL  COMMENT '引用 model_registry.modelId',
  alias           VARCHAR(64)            COMMENT '租户起的别名',
  rateLimitPerMin INT                    COMMENT '租户级 QPM 限速',
  enabled         TINYINT      NOT NULL  DEFAULT 1,
  customConfig    JSON                   COMMENT '租户自定义参数 {"temperature":0.7}',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_model (tenantId, modelId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户模型配置';
```

### 3.5 对话域

```sql
-- 会话（一次多轮对话）
CREATE TABLE ykt_aisaas_conversation (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  apiKeyId        BIGINT                  COMMENT '发起 API Key',
  externalUserId  VARCHAR(64)            COMMENT '调用方传入的最终用户ID',
  modelId         VARCHAR(64)  NOT NULL,
  title           VARCHAR(256)           COMMENT '会话标题',
  personaId       BIGINT                 COMMENT '使用的 Persona',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0已关闭 1活跃',
  messageCount    INT          NOT NULL  DEFAULT 0,
  totalTokensIn   BIGINT       NOT NULL  DEFAULT 0,
  totalTokensOut  BIGINT       NOT NULL  DEFAULT 0,
  lastMessageTime DATETIME,
  metadata        JSON                   COMMENT '客户端自定义',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_user_time (tenantId, externalUserId, lastMessageTime),
  KEY idx_tenant_time (tenantId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话';

-- 消息
CREATE TABLE ykt_aisaas_message (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  conversationId  BIGINT       NOT NULL,
  role            VARCHAR(16)  NOT NULL  COMMENT 'system/user/assistant/tool',
  content         MEDIUMTEXT             COMMENT '消息内容',
  toolCalls       JSON                   COMMENT '工具调用',
  toolCallId      VARCHAR(64)            COMMENT '工具响应对应的 call_id',
  modelId         VARCHAR(64)            COMMENT '生成此消息的模型',
  tokensIn        INT          NOT NULL  DEFAULT 0,
  tokensOut       INT          NOT NULL  DEFAULT 0,
  costCents       BIGINT       NOT NULL  DEFAULT 0,
  ttftMs          INT                    COMMENT 'Time to First Token（毫秒）',
  totalTimeMs     INT                    COMMENT '总响应时间',
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0失败 1成功 2中止',
  errorMsg        VARCHAR(512),
  requestId       VARCHAR(64)            COMMENT '请求ID（用于排障）',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_conv_time (conversationId, createTime),
  KEY idx_tenant_time (tenantId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息';
```

### 3.6 RAG 域

```sql
-- 知识库（MySQL）
CREATE TABLE ykt_aisaas_knowledge_base (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  name            VARCHAR(128) NOT NULL,
  description     VARCHAR(512),
  embeddingModel  VARCHAR(64)  NOT NULL  COMMENT '使用的 Embedding 模型ID',
  chunkSize       INT          NOT NULL  DEFAULT 800,
  chunkOverlap    INT          NOT NULL  DEFAULT 100,
  docCount        INT          NOT NULL  DEFAULT 0,
  chunkCount      INT          NOT NULL  DEFAULT 0,
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0构建中 1正常 2异常',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='知识库';

-- 文档
CREATE TABLE ykt_aisaas_knowledge_document (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  knowledgeBaseId BIGINT       NOT NULL,
  fileName        VARCHAR(256) NOT NULL,
  fileUrl         VARCHAR(512) NOT NULL  COMMENT 'MinIO 路径',
  fileType        VARCHAR(16)  NOT NULL  COMMENT 'txt/md/csv/pdf',
  fileSize        BIGINT       NOT NULL,
  chunkCount      INT          NOT NULL  DEFAULT 0,
  status          TINYINT      NOT NULL  DEFAULT 0  COMMENT '0待处理 1处理中 2就绪 3失败',
  errorMsg        VARCHAR(512),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_kb (tenantId, knowledgeBaseId, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='知识库文档';

-- ============================================================
-- PostgreSQL + PgVector 中的切片表（动态建表，命名规则）
-- 库名：ykt_aisaas_rag
-- 表名：rag_chunk_tenant_{tenantId}_kb_{knowledgeBaseId}
-- ============================================================
-- CREATE TABLE rag_chunk_tenant_1001_kb_5 (
--   id            BIGSERIAL PRIMARY KEY,
--   docId         BIGINT       NOT NULL,
--   chunkIndex    INT          NOT NULL,
--   content       TEXT         NOT NULL,
--   embedding     vector(1024) NOT NULL,
--   metadata      JSONB,
--   createTime    TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
--   FOREIGN KEY (docId) REFERENCES None  -- MySQL 主键，不建物理 FK
-- );
-- CREATE INDEX ON rag_chunk_tenant_1001_kb_5 USING ivfflat (embedding vector_cosine_ops) WITH (lists=100);
-- CREATE INDEX ON rag_chunk_tenant_1001_kb_5 (docId);
```

### 3.7 语音域

```sql
-- 声音库（系统预置 + 租户克隆产出）
CREATE TABLE ykt_aisaas_voice_library (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT 'NULL=系统预置',
  name            VARCHAR(64)  NOT NULL,
  provider        VARCHAR(32)  NOT NULL  COMMENT 'aliyun/edge/self-hosted',
  voiceId         VARCHAR(64)  NOT NULL  COMMENT '上游声音ID"cosyvoice-clone-v2-xxx"',
  language        VARCHAR(16)            COMMENT 'zh/en/ja',
  gender          VARCHAR(8)             COMMENT 'male/female',
  sampleUrl       VARCHAR(512)           COMMENT '试听音频',
  previewText     VARCHAR(256),
  cloneJobId      BIGINT                 COMMENT '来源克隆任务',
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_provider (tenantId, provider, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='声音库';

-- TTS/ASR 任务（每次调用记录）
CREATE TABLE ykt_aisaas_audio_task (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  type            VARCHAR(8)   NOT NULL  COMMENT 'tts/asr',
  modelId         VARCHAR(64)  NOT NULL,
  voiceId         VARCHAR(64)            COMMENT 'TTS 用',
  inputText       MEDIUMTEXT             COMMENT 'TTS 输入文本',
  inputAudioUrl   VARCHAR(512)           COMMENT 'ASR 输入音频',
  outputAudioUrl  VARCHAR(512)           COMMENT 'TTS 输出音频',
  outputText      MEDIUMTEXT             COMMENT 'ASR 输出文本',
  emotion         VARCHAR(32)            COMMENT 'happy/sad/neutral/angry',
  charsOrSeconds  INT          NOT NULL  DEFAULT 0 COMMENT 'TTS=字符数 ASR=秒数',
  costCents       BIGINT       NOT NULL  DEFAULT 0,
  latencyMs       INT,
  status          TINYINT      NOT NULL  DEFAULT 0  COMMENT '0处理中 1成功 2失败',
  errorMsg        VARCHAR(512),
  requestId       VARCHAR(64),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_type_time (tenantId, type, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='TTS/ASR 任务';
```

### 3.8 长任务域

```sql
-- 异步任务主表（声音克隆 / 模型微调 / 批量推理）
CREATE TABLE ykt_aisaas_async_job (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  jobType         VARCHAR(32)  NOT NULL  COMMENT 'voice_clone/finetune/batch_inference',
  jobName         VARCHAR(128),
  status          VARCHAR(16)  NOT NULL  COMMENT 'queued/running/success/failed/cancelled/timeout',
  progress        INT          NOT NULL  DEFAULT 0  COMMENT '0-100',
  inputParams     JSON         NOT NULL  COMMENT '任务参数',
  outputResult    JSON                   COMMENT '产物 {"modelId":"ft-xxx","weightsUrl":"..."}',
  workerId        VARCHAR(64)            COMMENT '执行 Worker 实例',
  queuedAt        DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  startedAt       DATETIME,
  finishedAt      DATETIME,
  timeoutAt       DATETIME                COMMENT '超时点',
  costCents       BIGINT       NOT NULL  DEFAULT 0,
  errorMsg        VARCHAR(1024),
  retryCount      INT          NOT NULL  DEFAULT 0,
  webhookUrl      VARCHAR(512)           COMMENT '完成回调',
  requestId       VARCHAR(64),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_type_time (tenantId, jobType, createTime),
  KEY idx_status_queued (status, queuedAt),
  KEY idx_worker (workerId, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务';

-- 任务日志
CREATE TABLE ykt_aisaas_async_job_log (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  jobId           BIGINT       NOT NULL,
  level           VARCHAR(8)   NOT NULL  COMMENT 'INFO/WARN/ERROR',
  message         VARCHAR(1024) NOT NULL,
  progress        INT,
  metadata        JSON,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_job_time (jobId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='任务日志';
```

### 3.9 MCP 域

```sql
-- MCP Server 注册（系统级或租户私有）
CREATE TABLE ykt_aisaas_mcp_server (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT 'NULL=系统预置',
  name            VARCHAR(64)  NOT NULL,
  serverType      VARCHAR(16)  NOT NULL  COMMENT 'stdio/sse/http',
  endpoint        VARCHAR(512) NOT NULL  COMMENT 'URL 或命令',
  authTokenEnc    VARCHAR(512)           COMMENT '加密的访问令牌',
  description     VARCHAR(512),
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='MCP Server';

-- MCP 工具（每个 server 注册若干 tool）
CREATE TABLE ykt_aisaas_mcp_tool (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT 'NULL=系统级',
  mcpServerId     BIGINT       NOT NULL,
  toolName        VARCHAR(64)  NOT NULL  COMMENT '工具名"get_weather"',
  description     VARCHAR(512),
  inputSchema     JSON         NOT NULL  COMMENT 'JSON Schema 参数定义',
  outputSchema    JSON,
  exampleOutput   JSON,
  callCount       BIGINT       NOT NULL  DEFAULT 0,
  avgLatencyMs    INT,
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_server (mcpServerId, status),
  KEY idx_tenant (tenantId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='MCP 工具';

-- 租户-MCP 绑定（哪些 MCP 工具租户可用）
CREATE TABLE ykt_aisaas_tenant_mcp_binding (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  mcpToolId       BIGINT       NOT NULL,
  enabled         TINYINT      NOT NULL  DEFAULT 1,
  customConfig    JSON                   COMMENT '租户自定义参数',
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_tool (tenantId, mcpToolId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户 MCP 绑定';
```

### 3.10 Persona 域

```sql
-- 性格人设
CREATE TABLE ykt_aisaas_persona (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT 'NULL=系统预置',
  code            VARCHAR(64)  NOT NULL,
  name            VARCHAR(64)  NOT NULL,
  description     VARCHAR(512),
  systemPrompt    TEXT         NOT NULL  COMMENT '系统提示词',
  greetingText    VARCHAR(256)           COMMENT '开场白',
  defaultEmotion  VARCHAR(32)            COMMENT 'neutral/happy/warm',
  emotionBaseline JSON                   COMMENT '{"happy":0.6,"neutral":0.3,"sad":0.1}',
  temperature     DECIMAL(3,2) DEFAULT 0.70,
  topP            DECIMAL(3,2) DEFAULT 0.90,
  ttsSpeed        DECIMAL(3,2) DEFAULT 1.00,
  ttsPitch        DECIMAL(3,2) DEFAULT 1.00,
  tags            JSON                   COMMENT '["温柔","专业","儿童友好"]',
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_code (tenantId, code),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Persona 性格人设';

-- Persona-声音绑定（一个 Persona 可绑定多个声音，按场景选）
CREATE TABLE ykt_aisaas_persona_voice_binding (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  personaId       BIGINT       NOT NULL,
  voiceLibraryId  BIGINT       NOT NULL,
  scenario        VARCHAR(32)            COMMENT 'default/child/elder/business',
  priority        INT          NOT NULL  DEFAULT 0,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_persona (personaId),
  KEY idx_tenant (tenantId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Persona-声音绑定';
```

### 3.11 系统域

```sql
-- 平台运营人员（区别于 ykt_aisaas_tenant_member）
CREATE TABLE ykt_aisaas_admin_user (
  id              BIGINT       NOT NULL,
  username        VARCHAR(64)  NOT NULL,
  password        VARCHAR(128) NOT NULL,
  nickname        VARCHAR(64),
  email           VARCHAR(128),
  phone           VARCHAR(32),
  avatar          VARCHAR(512),
  roleId          BIGINT       NOT NULL  COMMENT '单一角色',
  status          TINYINT      NOT NULL  DEFAULT 1,
  lastLoginTime   DATETIME,
  lastLoginIp     VARCHAR(64),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='平台运营人员';

-- 平台角色
CREATE TABLE ykt_aisaas_admin_role (
  id              BIGINT       NOT NULL,
  code            VARCHAR(64)  NOT NULL,
  name            VARCHAR(64)  NOT NULL,
  permissions     JSON                   COMMENT '权限点',
  remark          VARCHAR(256),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='平台角色';

-- 字典
CREATE TABLE ykt_aisaas_dict_type (
  id              BIGINT       NOT NULL,
  code            VARCHAR(64)  NOT NULL,
  name            VARCHAR(64)  NOT NULL,
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='字典类型';

CREATE TABLE ykt_aisaas_dict_item (
  id              BIGINT       NOT NULL,
  dictTypeCode    VARCHAR(64)  NOT NULL,
  itemValue       VARCHAR(64)  NOT NULL,
  itemLabel       VARCHAR(128) NOT NULL,
  sortNo          INT          NOT NULL  DEFAULT 0,
  status          TINYINT      NOT NULL  DEFAULT 1,
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL  DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_type (dictTypeCode)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='字典项';

-- 操作审计
CREATE TABLE ykt_aisaas_operation_log (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT                COMMENT '租户操作员所在租户',
  operatorType    VARCHAR(16)  NOT NULL  COMMENT 'admin/member/system',
  operatorId      BIGINT       NOT NULL,
  operatorName    VARCHAR(64),
  module          VARCHAR(64)            COMMENT 'tenant/apikey/billing/...',
  operation       VARCHAR(128)           COMMENT 'create_tenant/disable_apikey/...',
  method          VARCHAR(8)             COMMENT 'GET/POST/...',
  requestUrl      VARCHAR(512),
  requestIp       VARCHAR(64),
  params          TEXT                   COMMENT '请求参数（脱敏）',
  result          TEXT                   COMMENT '响应（截断）',
  costMs          INT,
  success         TINYINT      NOT NULL  DEFAULT 1,
  errorMsg        VARCHAR(1024),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_operator (operatorType, operatorId),
  KEY idx_module_time (module, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='操作审计';

-- 登录日志
CREATE TABLE ykt_aisaas_login_log (
  id              BIGINT       NOT NULL,
  userType        VARCHAR(16)  NOT NULL  COMMENT 'admin/member',
  userId          BIGINT       NOT NULL,
  tenantId        BIGINT,
  username        VARCHAR(64),
  loginIp         VARCHAR(64),
  loginLocation   VARCHAR(128),
  browser         VARCHAR(64),
  os              VARCHAR(64),
  status          TINYINT      NOT NULL  DEFAULT 1  COMMENT '0失败 1成功',
  msg             VARCHAR(256),
  createTime      DATETIME     NOT NULL  DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_user_time (userType, userId, createTime),
  KEY idx_tenant_time (tenantId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='登录日志';
```

---

## 4. Redis Key 规范

```
aisaas:tenant:{tenantId}:*                      # 租户级命名空间

aisaas:tenant:{tid}:apikey:{hash}                # API Key 缓存（含 tenantId/scope/限额）
aisaas:tenant:{tid}:quota:{dimension}:{yyyymm}   # 月度配额计数
aisaas:tenant:{tid}:rate:{apiKeyId}              # 滑动窗口限流
aisaas:tenant:{tid}:model:cache:{modelId}        # 模型配置缓存
aisaas:tenant:{tid}:conv:{convId}                # 会话上下文缓存（最近 N 轮）
aisaas:tenant:{tid}:persona:cache:{personaId}    # Persona 缓存
aisaas:tenant:{tid}:tts:provider:{voiceId}       # TTS Provider 实例缓存
aisaas:tenant:{tid}:job:progress:{jobId}         # 长任务进度实时缓存

aisaas:global:model:registry                     # 全局模型注册表缓存
aisaas:global:plan:list                          # 套餐列表缓存
aisaas:lock:quota:deduct:{tid}:{dimension}       # 配额扣减分布式锁
```

**TTL 策略**：
- 配置类（apikey/model/persona/plan）：30 分钟，主动失效
- 配额类：按月持久（次月 1 号清零）
- 会话类：2 小时（活跃续期）
- 任务进度：任务结束后保留 1 小时

---

## 5. MinIO 路径规范

```
ykt-aisaas/                                      # bucket
├── tenant/{tenantId}/
│   ├── audio/tts/{yyyy}/{mm}/{dd}/{uuid}.mp3   # TTS 产出
│   ├── audio/asr/{yyyy}/{mm}/{dd}/{uuid}.wav   # ASR 输入
│   ├── doc/upload/{uuid}.{ext}                 # RAG 文档原文件
│   ├── voice/sample/{uuid}.wav                 # 声音克隆样本
│   ├── finetune/dataset/{uuid}.jsonl           # 微调数据集
│   └── finetune/weights/{jobId}/               # 微调产物
└── system/
    ├── voice/preset/                           # 系统预置声音
    └── avatar/                                 # 系统头像
```

---

## 6. 索引设计原则

### 6.1 高频查询模式与对应索引

| 查询模式 | 索引 |
|---|---|
| 按租户查所有数据 | `(tenantId, isDeleted)` |
| 按租户+时间范围 | `(tenantId, createTime)` |
| 按租户+业务+时间 | `(tenantId, bizType, createTime)` |
| API Key 反查（高并发） | `UNIQUE (apiKeyHash)` |
| 模型路由 | `UNIQUE (tenantId, modelId)` + `tenantId IS NULL` 全局模型 |
| 任务调度扫描 | `(status, queuedAt)` |
| 会话按用户 | `(tenantId, externalUserId, lastMessageTime)` |

### 6.2 PgVector 索引

```sql
-- 小数据量（<10万向量）：ivfflat，lists=100
CREATE INDEX ON rag_chunk_tenant_{tid}_kb_{kbid}
  USING ivfflat (embedding vector_cosine_ops) WITH (lists=100);

-- 大数据量（>10万向量）：hnsw（MVP 预留，量级上来后启用）
CREATE INDEX ON rag_chunk_tenant_{tid}_kb_{kbid}
  USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64);
```

---

## 7. 初始化数据（flyway V1__init.sql 末尾）

```sql
-- 默认平台管理员（密码：admin123，BCrypt）
INSERT INTO ykt_aisaas_admin_user (id, username, password, nickname, roleId)
VALUES (1, 'admin', '$2a$10$xxx', '平台管理员', 1);

-- 默认平台角色
INSERT INTO ykt_aisaas_admin_role (id, code, name, permissions)
VALUES (1, 'SUPER_ADMIN', '超级管理员', '["*"]');

-- 默认套餐
INSERT INTO ykt_aisaas_plan (id, code, name, priceMonthly, priceYearly, quotas, features)
VALUES
  (1, 'free', '免费版', 0, 0,
   '{"llmTokensInMonthly":100000,"llmTokensOutMonthly":100000,"ttsCharsMonthly":10000,"asrSecondsMonthly":60}',
   '["llm","tts","asr","rag"]'),
  (2, 'standard', '标准版', 99.00, 999.00,
   '{"llmTokensInMonthly":2000000,"llmTokensOutMonthly":2000000,"ttsCharsMonthly":500000,"asrSecondsMonthly":3600}',
   '["llm","tts","asr","rag","mcp","voice_clone"]'),
  (3, 'enterprise', '企业版', 999.00, 9999.00,
   '{"llmTokensInMonthly":50000000,"llmTokensOutMonthly":50000000,"ttsCharsMonthly":10000000,"asrSecondsMonthly":72000}',
   '["llm","tts","asr","rag","mcp","voice_clone","finetune"]');

-- 默认全局模型
INSERT INTO ykt_aisaas_model_registry (id, tenantId, modelId, provider, baseUrl, apiKeyEnc, upstreamModel, modality, type, contextLength, priceInputCents, priceOutputCents, isDefault)
VALUES
  (1, NULL, 'gpt-4o-mini', 'openai', 'https://api.openai.com/v1', 'ENC[xxx]', 'gpt-4o-mini', '["text","vision","function"]', 'chat', 128000, 0.105, 0.42, 1),
  (2, NULL, 'deepseek-chat', 'deepseek', 'https://api.deepseek.com/v1', 'ENC[xxx]', 'deepseek-chat', '["text","function"]', 'chat', 64000, 0.1, 0.2, 0),
  (3, NULL, 'glm-4-flash', 'zhipu', 'https://open.bigmodel.cn/api/paas/v4', 'ENC[xxx]', 'glm-4-flash', '["text","function"]', 'chat', 128000, 0, 0, 0),
  (4, NULL, 'qwen-turbo', 'dashscope', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'ENC[xxx]', 'qwen-turbo', '["text","function"]', 'chat', 1000000, 0.03, 0.06, 0),
  (5, NULL, 'text-embedding-v3', 'dashscope', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'ENC[xxx]', 'text-embedding-v3', '[]', 'embedding', 8192, 0.007, 0, 1),
  (6, NULL, 'cosyvoice-v2', 'aliyun-tts', 'https://dashscope.aliyuncs.com/api/v1/services/audio/tts', 'ENC[xxx]', 'cosyvoice-v2', '["emotion"]', 'tts', 0, 0.1, 0, 1),
  (7, NULL, 'paraformer-v2', 'aliyun-asr', 'https://dashscope.aliyuncs.com/api/v1/services/audio/asr', 'ENC[xxx]', 'paraformer-v2', '["emotion"]', 'asr', 0, 0.4, 0, 1);
```

---

## 8. Flyway 迁移规划

| 版本 | 文件 | 内容 |
|---|---|---|
| V1 | `V1__init_schema.sql` | 所有表 DDL + 初始化数据 |
| V2 | `V2__add_voice_clone_tables.sql` | P1 阶段补全 |
| V3 | `V3__add_finetune_tables.sql` | P1 阶段补全 |
| V4 | `V4__partition_message_table.sql` | message 分表（P1） |
| V5 | `V5__partition_usage_detail.sql` | usage_detail 分表（P1） |

PostgreSQL 侧用独立 Flyway 实例管理（不同库）：
- `V1__enable_pgvector.sql`
- `V2__create_template_tables.sql`（模板表，实际 chunk 表由代码动态创建）

---

## 9. 待评审

- [ ] 表前缀 `ykt_aisaas_` 是否过长（影响 SQL 可读性）
- [ ] 配额表是否需要按"日 + 月"双周期（MVP 只月）
- [ ] message 表分表时机（建议 1000 万行触发）
- [ ] 是否需要给 `requestId` 单独建索引（用于排障）
- [ ] PgVector 是否需要按租户分库（MVP 同库不同表，够用）
- [ ] 声音克隆样本是否要单独存储生命周期管理
- [ ] **v1.1 新增：内部超级租户的计量策略**（不计费但需要统计？独立表还是用 usage_detail 加 isInternal 标记？）
- [ ] **v1.1 新增：device_session 表是否对等 xiaozhi sys_device**（避免数据冗余 vs 实时性）

---

## 10. xiaozhi-server 数据迁移（**v1.1 新增**）

### 10.1 迁移总览

xiaozhi-server 改造接入平台后，部分 AI 相关配置数据需要迁移到平台库：

| xiaozhi 源表 | 平台目标表 | 迁移策略 | 时机 |
|---|---|---|---|
| `sys_config`（type=llm/tts/stt/embedding） | `ykt_aisaas_model_registry`（tenantId=1，内部租户） | 转换 + 一次性导入 | Week 6 |
| `sys_role`（AI 角色） | `ykt_aisaas_persona`（系统预置） | 字段映射 + 一次性导入 | Week 6 |
| `sys_template`（提示词模板） | 合并到对应 `ykt_aisaas_persona.systemPrompt` | 关联查询合并 | Week 6 |
| `sys_knowledge_base` | `ykt_aisaas_knowledge_base` | 字段映射 + 一次性导入 | Week 7 |
| `sys_knowledge_document` | `ykt_aisaas_knowledge_document` | 字段映射 + 文件路径改写 | Week 7 |
| Chroma collection `kb_*` | PgVector 表 `rag_chunk_tenant_1_kb_{id}` | 重建向量（推荐）或直迁 | Week 7 |
| `sys_mcp_tool_exclude` | `ykt_aisaas_tenant_mcp_binding.enabled=0` | 转换 + 一次性导入 | Week 7 |

### 10.2 字段映射

#### 10.2.1 sys_config → ykt_aisaas_model_registry

```sql
-- 假设 xiaozhi 库已配为第二数据源
INSERT INTO ykt_aisaas_model_registry (
  id, tenantId, modelId, provider, baseUrl, apiKeyEnc, upstreamModel,
  modality, type, contextLength, priceInputCents, priceOutputCents,
  isStream, isDefault, capabilities, status, createTime
)
SELECT
  NULL,                                              -- 自增雪花 ID
  1,                                                 -- 内部租户 ID
  c.modelName,                                       -- 业务模型ID
  CASE c.provider
    WHEN 'openai' THEN 'openai'
    WHEN 'edage' THEN 'edge-tts'                     -- 修正历史拼写
    WHEN 'aliyun' THEN CASE c.type
      WHEN 'tts' THEN 'aliyun-tts'
      WHEN 'stt' THEN 'aliyun-asr'
      ELSE 'dashscope'
    END
    WHEN 'xfyun' THEN CASE c.type WHEN 'tts' THEN 'xfyun-tts' WHEN 'stt' THEN 'xfyun-asr' END
    WHEN 'tencent' THEN CASE c.type WHEN 'tts' THEN 'tencent-tts' WHEN 'stt' THEN 'tencent-asr' END
    WHEN 'volcengine' THEN CASE c.type WHEN 'tts' THEN 'volcengine-tts' WHEN 'stt' THEN 'volcengine-asr' END
    ELSE c.provider
  END,
  JSON_EXTRACT(c.config, '$.baseUrl'),
  AES_ENCRYPT(JSON_EXTRACT(c.config, '$.apiKey'), ?),
  JSON_EXTRACT(c.config, '$.upstreamModel'),
  CASE c.type
    WHEN 'llm' THEN '["text","vision","function"]'
    WHEN 'embedding' THEN '[]'
    ELSE '["emotion"]'
  END,
  c.type,
  JSON_EXTRACT(c.config, '$.contextLength'),
  JSON_EXTRACT(c.config, '$.priceInputCents'),
  JSON_EXTRACT(c.config, '$.priceOutputCents'),
  1, 0, '{}', 1, NOW()
FROM xiaozhi.sys_config c
WHERE c.type IN ('llm','tts','stt','embedding')
  AND c.isDeleted = 0;
```

#### 10.2.2 sys_role → ykt_aisaas_persona

```sql
INSERT INTO ykt_aisaas_persona (
  tenantId, code, name, description, systemPrompt, greetingText,
  defaultEmotion, temperature, topP, ttsSpeed, ttsPitch,
  tags, status
)
SELECT
  1,                                            -- 内部租户（系统预置）
  CONCAT('xiaozhi_role_', r.id),
  r.roleName,
  r.description,
  r.prompt,                                     -- systemPrompt
  r.greeting,
  'neutral',
  r.temperature,
  r.topP,
  1.0,                                          -- TTS 默认参数（sys_role 没有）
  1.0,
  JSON_ARRAY(),
  1
FROM xiaozhi.sys_role r
WHERE r.isDeleted = 0;

-- 然后建立 xiaozhi sys_role.id ↔ ykt_aisaas_persona.id 映射表（应急回滚用）
CREATE TABLE ykt_aisaas_xiaozhi_role_map (
  xiaozhiRoleId   BIGINT PRIMARY KEY,
  platformPersonaId BIGINT NOT NULL,
  migratedAt      DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

#### 10.2.3 sys_knowledge_base / _document → 平台 RAG 表

```sql
-- 知识库元数据
INSERT INTO ykt_aisaas_knowledge_base (
  tenantId, name, description, embeddingModel, chunkSize, chunkOverlap, docCount, chunkCount, status
)
SELECT 1, kb.name, kb.description, 'text-embedding-v3', 800, 100, kb.docCount, kb.chunkCount, 1
FROM xiaozhi.sys_knowledge_base kb
WHERE kb.isDeleted = 0;

-- 文档元数据
INSERT INTO ykt_aisaas_knowledge_document (
  tenantId, knowledgeBaseId, fileName, fileUrl, fileType, fileSize, chunkCount, status
)
SELECT 1, kbid_mapping.new_id, d.fileName, d.filePath, d.fileType, d.fileSize, d.chunkCount,
  CASE d.status WHEN 2 THEN 2 ELSE 0 END  -- 重新索引
FROM xiaozhi.sys_knowledge_document d;

-- 向量数据：建议重新跑 embedding（embeddingModel 可能不同）
-- 如果模型完全一致可直迁：
-- INSERT INTO rag_chunk_tenant_1_kb_{id} (docId, chunkIndex, content, embedding, metadata)
-- SELECT docId, chunkIndex, content, embedding::vector, metadata
-- FROM xiaozhi_chroma.kb_{old_id};
```

### 10.3 迁移脚本规划（Flyway on 平台库）

| 版本 | 文件 | 内容 |
|---|---|---|
| V1 | `V1__init_schema.sql` | 平台所有表 DDL + 初始化数据 |
| V2 | `V2__xiaozhi_migration.sql` | **v1.1 新增**：xiaozhi 数据迁移脚本 |
| V3 | `V3__xiaozhi_role_mapping.sql` | **v1.1 新增**：sys_role ↔ persona 映射表 |
| V4 | `V4__add_voice_clone_tables.sql` | P1 阶段补全 |
| V5 | `V5__add_finetune_tables.sql` | P1 阶段补全 |
| V6 | `V6__partition_message_table.sql` | message 分表（P1） |
| V7 | `V7__partition_usage_detail.sql` | usage_detail 分表（P1） |
| V8 | `V8__cleanup_xiaozhi_legacy.sql` | **v1.1 新增**：1 个月稳定期后，清理 xiaozhi 库的已迁移表 |

### 10.4 迁移流程

```
═══ Week 6（开发期）═══
   1. 在灰度环境执行 V2/V3 迁移脚本
   2. 对比源 xiaozhi 库 vs 平台库：行数、字段值抽样
   3. 用 100 台测试设备调平台，验证 AI 行为一致

═══ Week 8（生产上线前）═══
   1. 生产环境备份 xiaozhi 库
   2. 业务低谷期（凌晨）执行迁移
   3. 切换 xiaozhi-server 配置：feature flag `xiaozhi.ai.usePlatform=true`
   4. 监控 1 小时，无异常 → 完成

═══ Week 8-12（双写观察期）═══
   1. xiaozhi-server 保留只读 sys_config（应急回滚用）
   2. 每日对账：平台调用量 vs 设备活跃数（异常告警）

═══ Week 12+（清理）═══
   1. 执行 V8 清理 xiaozhi 库的 AI 配置表
   2. 删除 xiaozhi-ai 模块代码
   3. xiaozhi-server 彻底瘦身
```

### 10.5 应急回滚

若平台故障导致设备不可用，立即回滚：

```bash
# 1. 切回 xiaozhi 自研 AI
xiaozhi.ai.usePlatform=false
# 重启 xiaozhi-server

# 2. 数据未丢失（sys_config 仍在 xiaozhi 库，仅 7 天观察期内未删）

# 3. 平台故障修复后，重新启用
xiaozhi.ai.usePlatform=true
```

**关键保障**：xiaozhi 库的 AI 配置表保留 7 天只读副本，确保应急回滚可行。
