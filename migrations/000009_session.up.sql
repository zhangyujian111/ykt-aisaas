-- 000009_session.up.sql — Session 会话域（含配额快照）
-- Q8 决策：会话创建时记录设备配额快照，流式更新已用值，结算时填入 actualCost
SET NAMES utf8mb4;

-- ============================================================
-- ykt_aisaas_session — 会话（含配额借记快照）
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_session (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID',
  deviceId             VARCHAR(128) NOT NULL COMMENT '设备ID',

  -- 会话标识
  sessionType          VARCHAR(16)  NOT NULL DEFAULT 'chat' COMMENT '会话类型：chat/voice/video/rag',
  title                VARCHAR(256) DEFAULT NULL COMMENT '会话标题',

  -- 关联的 Persona
  personaId            BIGINT       DEFAULT NULL COMMENT '使用的 Persona ID',

  -- 外部用户标识（设备租户下挂的最终用户）
  externalUserId       VARCHAR(64)  DEFAULT NULL COMMENT '调用方传入的终端用户ID',

  -- 使用的模型
  modelId              VARCHAR(64)  DEFAULT NULL COMMENT '模型 ID（model_registry.modelId）',

  -- ============================================================
  -- 配额快照（Q8 决策核心）
  -- 创建会话时从 Redis/DB 获取设备当前配额，持久化到此字段
  -- 流式对话中实时更新 quotaUsed，结算时填入 actualCost
  -- ============================================================

  -- 配额快照 JSON（创建时的设备配额完整快照）
  quotaSnapshot         JSON         NOT NULL COMMENT '创建时的配额快照 {llm_tokens_in:100000, llm_tokens_out:80000, tts_chars:5000, ...}',

  -- 借记维度（主维度，用于展示和告警）
  quotaDimension        VARCHAR(32)  NOT NULL DEFAULT 'llm_tokens_in' COMMENT '主借记维度：llm_tokens_in/llm_tokens_out/tts_chars/asr_seconds',

  -- 借记初始值（创建时 quotaSnapshot 中该维度的 limitValue）
  quotaInitial          BIGINT       NOT NULL DEFAULT 0 COMMENT '借记初始配额值',

  -- 已用值（流式更新，每次 token 计量后 +N）
  quotaUsed             BIGINT       NOT NULL DEFAULT 0 COMMENT '已借记用量',

  -- 结算实际消耗（会话结束后填入，包含各维度明细）
  actualCost            JSON         DEFAULT NULL COMMENT '结算实际消耗 {llm_tokens_in:1234, llm_tokens_out:567, tts_chars:89, cost_cents:234}',

  -- ============================================================
  -- 会话统计
  -- ============================================================
  messageCount          INT          NOT NULL DEFAULT 0 COMMENT '消息数',
  totalTokensIn         BIGINT       NOT NULL DEFAULT 0 COMMENT '累计输入 token',
  totalTokensOut        BIGINT       NOT NULL DEFAULT 0 COMMENT '累计输出 token',

  -- ============================================================
  -- 状态机
  -- 1=创建中（初始化）2=活跃（对话中）3=已结束（用户主动关闭）4=已结算（配额清算完成）
  -- ============================================================
  status                TINYINT      NOT NULL DEFAULT 1 COMMENT '1创建中 2活跃 3已结束 4已结算',

  -- 元数据（JSON，可扩展）
  metadata    JSON        NULL COMMENT '会话元数据',

  -- 时间戳
  startTime             DATETIME     DEFAULT NULL COMMENT '实际开始时间',
  endTime               DATETIME     DEFAULT NULL COMMENT '结束时间',
  lastMessageTime       DATETIME     DEFAULT NULL COMMENT '最后消息时间',

  createTime            DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updateTime            DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  isDeleted            TINYINT      NOT NULL DEFAULT 0 COMMENT '0未删 1已删',

  PRIMARY KEY (id),
  KEY idx_tenant_device (tenantId, deviceId, status),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_device_active (deviceId, status, lastMessageTime),
  KEY idx_persona (personaId),
  KEY idx_status (status, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话（含配额借记快照）';
