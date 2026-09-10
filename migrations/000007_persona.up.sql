-- 000007_persona.up.sql — Persona 人设域：persona 主表 + persona_bind 绑定表
SET NAMES utf8mb4;

-- ============================================================
-- ykt_aisaas_persona — 人设/Agent 配置
-- Q1 决策：从 xiaozhi-server 角色下沉，与 xiaozhi-java 对齐
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_persona (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID（NULL=系统预置）',
  code                 VARCHAR(64)  NOT NULL COMMENT 'Persona 编码（租户内唯一）',
  name                 VARCHAR(128) NOT NULL COMMENT 'Persona 名称',
  description          VARCHAR(512) DEFAULT NULL COMMENT '描述',

  -- 系统提示词
  systemPrompt         TEXT         NOT NULL COMMENT '系统提示词（人设核心）',

  -- 性格特征（JSON格式：{emotion_baseline, speaking_style, ...}）
  personalityTraits    JSON        NULL COMMENT '性格特征 JSON',

  -- 音色偏好（引用 voice_library.id）
  voicePreference      VARCHAR(64)  DEFAULT NULL COMMENT '音色偏好（voice_library.id）',

  -- 关系阶段定义（JSON格式：{stage_name: {min_interactions, available_actions, ...}}）
  relationshipStages    JSON        NULL COMMENT '关系阶段定义',

  -- 默认模型
  defaultModelId       VARCHAR(64)  DEFAULT NULL COMMENT '默认模型ID（model_registry.modelId）',

  -- 默认知识库ID列表（JSON数组）
  defaultKnowledgeBaseIds      JSON     NULL COMMENT '默认知识库ID列表',

  -- 对话风格参数
  temperature          DECIMAL(3,2) NOT NULL DEFAULT 0.70 COMMENT '温度参数',
  topP                 DECIMAL(3,2) NOT NULL DEFAULT 0.90 COMMENT 'Top-P',
  maxTokens            INT          NOT NULL DEFAULT 4096 COMMENT '最大生成 token 数',
  presencePenalty      DECIMAL(3,2) NOT NULL DEFAULT 0.00 COMMENT '存在惩罚',
  frequencyPenalty     DECIMAL(3,2) NOT NULL DEFAULT 0.00 COMMENT '频率惩罚',

  -- 状态机：0禁用 1正常 2归档
  status               TINYINT      NOT NULL DEFAULT 1 COMMENT '0禁用 1正常 2归档',

  -- 标签（JSON数组）
  tags                         JSON     NULL COMMENT '标签 ["温柔","专业","儿童友好"]',

  createTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updateTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  isDeleted            TINYINT      NOT NULL DEFAULT 0 COMMENT '0未删 1已删',

  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_code (tenantId, code),
  KEY idx_tenant (tenantId, isDeleted),
  KEY idx_status (status, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Persona 人设配置';

-- ============================================================
-- ykt_aisaas_persona_bind — 设备/ persona 绑定
-- 设备租户（tenantType=DEVICE）绑定一个默认 Persona
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_persona_bind (
  id                   BIGINT       NOT NULL COMMENT '主键',
  tenantId             BIGINT       NOT NULL COMMENT '租户ID（设备所属租户）',
  deviceId             VARCHAR(128) NOT NULL COMMENT '设备ID（xiaozhi 设备标识）',
  personaId            BIGINT       NOT NULL COMMENT '绑定的 Persona ID',
  bindType             VARCHAR(16)  NOT NULL DEFAULT 'default' COMMENT '绑定类型：default默认/strict严格匹配',
  priority             INT          NOT NULL DEFAULT 0 COMMENT '优先级（多规则时取高）',

  -- 绑定条件（JSON，可选）
  bindCondition        JSON         DEFAULT NULL COMMENT '绑定条件 {externalUserId, timeRange, ...}',

  -- 状态：0解绑 1绑定
  status               TINYINT      NOT NULL DEFAULT 1 COMMENT '0解绑 1绑定',

  createTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updateTime           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  isDeleted            TINYINT      NOT NULL DEFAULT 0 COMMENT '0未删 1已删',

  PRIMARY KEY (id),
  UNIQUE KEY uk_device (deviceId),
  KEY idx_tenant_persona (tenantId, personaId, status),
  KEY idx_persona (personaId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='设备-Persona 绑定';
