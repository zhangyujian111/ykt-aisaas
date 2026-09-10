-- 000008_memory.up.sql — Memory 记忆域：三层模型
-- Q3 决策：与 xiaozhi-java 对齐
--   - 短期：session_message（每次对话后追加）
--   - 摘要：memory_summary（实体 content 中可选携带 summary 字段）
--   - 长期：entity + entity_relation
SET NAMES utf8mb4;

-- ============================================================
-- ykt_aisaas_session_message — 短期会话上下文窗口
-- 高写入，按 session 分组，TTL 可选（建议 7 天后转储到 memory）
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_session_message (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID',
  sessionId            BIGINT       NOT NULL COMMENT '所属会话 ID（ykt_aisaas_session.id）',
  messageIndex         INT          NOT NULL DEFAULT 0 COMMENT '消息序号（会话内递增）',

  -- 角色：user/assistant/system/tool
  role                 VARCHAR(16)  NOT NULL COMMENT '角色：user/assistant/system/tool',

  -- 消息内容
  content              MEDIUMTEXT   COMMENT '消息内容',

  -- Token 统计
  tokensIn             INT          NOT NULL DEFAULT 0 COMMENT '输入 token 数',
  tokensOut            INT          NOT NULL DEFAULT 0 COMMENT '输出 token 数',

  -- 元数据
  metadata    JSON        NULL COMMENT '扩展元数据 {audioUrl, toolCalls, ...}',

  -- 来源标识（溯源）
  sourceType           VARCHAR(16)  DEFAULT NULL COMMENT '来源：user_input/llm_response/tool_call/rag_retrieval',
  sourceMessageId      BIGINT       DEFAULT NULL COMMENT '溯源消息 ID（tool_call 溯源到 trigger message）',

  createTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',

  PRIMARY KEY (id),
  KEY idx_session_time (sessionId, createTime),
  KEY idx_tenant_time (tenantId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='短期会话消息';

-- ============================================================
-- ykt_aisaas_memory — 长期记忆实体
-- 实体类型：人物(PERSON)/事件(EVENT)/偏好(PREFERENCE)/物品(OBJECT)/地点(LOCATION)/知识(KNOWLEDGE)
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_memory (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID',
  deviceId             VARCHAR(128) NOT NULL COMMENT '设备ID（溯源设备）',

  -- 实体标识
  entityType           VARCHAR(32)  NOT NULL COMMENT '实体类型：PERSON/EVENT/PREFERENCE/OBJECT/LOCATION/KNOWLEDGE',
  entityKey            VARCHAR(256) NOT NULL COMMENT '实体唯一标识（设备内唯一）',

  -- 实体内容（JSON）
  content              JSON         NOT NULL COMMENT '实体内容 {summary, facts: [], preferences: {}, ...}',

  -- 重要性分数（0-1 FLOAT，用于摘要/遗忘策略）
  importance           DECIMAL(3,2) NOT NULL DEFAULT 0.50 COMMENT '重要性 0~1',

  -- 溯源
  sourceMessageId      BIGINT       DEFAULT NULL COMMENT '来源消息 ID（可追溯原始输入）',

  -- 版本号（最终一致性）
  version              INT          NOT NULL DEFAULT 1 COMMENT '版本号（乐观锁）',

  -- 记忆状态：0潜在（待确认）1活跃 2已遗忘（软删除）
  status               TINYINT      NOT NULL DEFAULT 1 COMMENT '0潜在 1活跃 2已遗忘',

  -- 最后访问时间（用于 LRU/TTL 判断）
  lastAccessTime       DATETIME     DEFAULT NULL COMMENT '最后访问时间',

  createTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updateTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  isDeleted            TINYINT      NOT NULL DEFAULT 0 COMMENT '0未删 1已删',

  PRIMARY KEY (id),
  UNIQUE KEY uk_device_entity (deviceId, entityType, entityKey),
  KEY idx_tenant_device (tenantId, deviceId, status),
  KEY idx_importance (importance, status),
  KEY idx_source_msg (sourceMessageId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='长期记忆实体';

-- ============================================================
-- ykt_aisaas_memory_relation — 记忆实体关系图谱
-- 构建实体间的关联关系，支持知识图谱推理
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_memory_relation (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID',

  -- 关系三元组
  sourceEntityId       BIGINT       NOT NULL COMMENT '源实体 ID（ykt_aisaas_memory.id）',
  relationType         VARCHAR(64)  NOT NULL COMMENT '关系类型：knows/likes/participated_in/owns/is_a/part_of...',
  targetEntityId       BIGINT       NOT NULL COMMENT '目标实体 ID（ykt_aisaas_memory.id）',

  -- 关系权重（0-1 FLOAT）
  weight               DECIMAL(3,2) NOT NULL DEFAULT 0.50 COMMENT '关系权重 0~1',

  -- 关系属性（JSON）
  properties           COMMENT '关系属性 {since, frequency, context, ...}',

  -- 溯源
  sourceMessageId      BIGINT       DEFAULT NULL COMMENT '来源消息 ID',

  -- 版本号
  version              INT          NOT NULL DEFAULT 1 COMMENT '版本号',

  status               TINYINT      NOT NULL DEFAULT 1 COMMENT '0失效 1有效',

  createTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updateTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  isDeleted            TINYINT      NOT NULL DEFAULT 0 COMMENT '0未删 1已删',

  PRIMARY KEY (id),
  -- 去重：同一对实体同一种关系只保留一条
  UNIQUE KEY uk_relation (sourceEntityId, relationType, targetEntityId),
  KEY idx_tenant (tenantId),
  KEY idx_source_entity (sourceEntityId, status),
  KEY idx_target_entity (targetEntityId, status),
  KEY idx_relation_type (relationType, weight)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='记忆实体关系';
