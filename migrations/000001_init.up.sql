-- 000001_init.up.sql — ykt-aisaas 核心表（v0.1 可运行子集，DDL 对齐 docs/DATABASE.md）
SET NAMES utf8mb4;

-- 租户主表（系统表，无 tenantId）
CREATE TABLE IF NOT EXISTS ykt_aisaas_tenant (
  id           BIGINT      NOT NULL COMMENT '租户ID',
  code         VARCHAR(64) NOT NULL COMMENT '租户编码',
  name         VARCHAR(128) NOT NULL COMMENT '租户名称',
  contactName  VARCHAR(64)  DEFAULT NULL,
  contactPhone VARCHAR(32)  DEFAULT NULL,
  contactEmail VARCHAR(128) DEFAULT NULL,
  status       TINYINT     NOT NULL DEFAULT 1 COMMENT '0禁用 1正常 2冻结',
  planId       BIGINT      DEFAULT NULL,
  expireTime   DATETIME    DEFAULT NULL,
  remark       VARCHAR(512) DEFAULT NULL,
  createTime   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted    TINYINT     NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code),
  KEY idx_status (status, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户主表';

-- API Key
CREATE TABLE IF NOT EXISTS ykt_aisaas_apikey (
  id          BIGINT       NOT NULL,
  tenantId    BIGINT       NOT NULL,
  name        VARCHAR(64)  NOT NULL COMMENT 'Key 名称',
  apiKey      VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '占位（不再存明文）',
  apiKeyHash  VARCHAR(128) NOT NULL COMMENT 'SHA-256',
  keyPrefix   VARCHAR(16)  NOT NULL,
  scope       VARCHAR(512) NOT NULL DEFAULT '[]',
  ipWhitelist VARCHAR(1024) NOT NULL DEFAULT '[]',
  expiresAt   DATETIME     DEFAULT NULL,
  lastUsedAt  DATETIME     DEFAULT NULL,
  lastUsedIp  VARCHAR(64)  DEFAULT NULL,
  status      TINYINT      NOT NULL DEFAULT 1,
  createdBy   BIGINT       NOT NULL DEFAULT 0,
  createTime  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted   TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_apikey_hash (apiKeyHash),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API Key';

-- 模型注册表（tenantId IS NULL = 全局）
CREATE TABLE IF NOT EXISTS ykt_aisaas_model_registry (
  id               BIGINT        NOT NULL,
  tenantId         BIGINT        DEFAULT NULL,
  modelId          VARCHAR(64)   NOT NULL,
  provider         VARCHAR(32)   NOT NULL,
  baseUrl          VARCHAR(256)  NOT NULL,
  apiKeyEnc        VARCHAR(1024) NOT NULL COMMENT 'AES-GCM 加密',
  upstreamModel    VARCHAR(64)   NOT NULL,
  modality         VARCHAR(256)  NOT NULL DEFAULT '["text"]',
  type             VARCHAR(16)   NOT NULL COMMENT 'chat/embedding/tts/asr',
  contextLength    INT           NOT NULL DEFAULT 0,
  priceInputCents  DECIMAL(10,4) NOT NULL DEFAULT 0,
  priceOutputCents DECIMAL(10,4) NOT NULL DEFAULT 0,
  isStream         TINYINT       NOT NULL DEFAULT 1,
  isDefault        TINYINT       NOT NULL DEFAULT 0,
  capabilities     VARCHAR(512)  NOT NULL DEFAULT '{}',
  status           TINYINT       NOT NULL DEFAULT 1,
  createTime       DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime       DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted        TINYINT       NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_modelid (tenantId, modelId),
  KEY idx_provider_type (provider, type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模型注册表';

-- 配额（Redis 实时 + DB 快照）
CREATE TABLE IF NOT EXISTS ykt_aisaas_quota (
  id            BIGINT      NOT NULL,
  tenantId      BIGINT      NOT NULL,
  periodStart   DATE        NOT NULL,
  periodEnd     DATE        NOT NULL,
  dimension     VARCHAR(32) NOT NULL,
  limitValue    BIGINT      NOT NULL DEFAULT 0,
  usedValue     BIGINT      NOT NULL DEFAULT 0,
  overagePolicy VARCHAR(16) NOT NULL DEFAULT 'reject',
  createTime    DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime    DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_period_dim (tenantId, periodStart, dimension)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='配额';

-- 计量明细（高写入）
CREATE TABLE IF NOT EXISTS ykt_aisaas_usage_detail (
  id         BIGINT      NOT NULL,
  tenantId   BIGINT      NOT NULL,
  apiKeyId   BIGINT      NOT NULL DEFAULT 0,
  bizType    VARCHAR(32) NOT NULL,
  dimension  VARCHAR(32) NOT NULL,
  amount     BIGINT      NOT NULL,
  costCents  BIGINT      NOT NULL DEFAULT 0,
  modelId    VARCHAR(64) NOT NULL DEFAULT '',
  requestId  VARCHAR(64) NOT NULL DEFAULT '',
  status     TINYINT     NOT NULL DEFAULT 1,
  createTime DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_biz_time (tenantId, bizType, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='计量明细';

-- 余额
CREATE TABLE IF NOT EXISTS ykt_aisaas_balance (
  id             BIGINT   NOT NULL,
  tenantId       BIGINT   NOT NULL,
  balanceCents   BIGINT   NOT NULL DEFAULT 0,
  frozenCents    BIGINT   NOT NULL DEFAULT 0,
  totalRecharged BIGINT   NOT NULL DEFAULT 0,
  totalConsumed  BIGINT   NOT NULL DEFAULT 0,
  version        INT      NOT NULL DEFAULT 0,
  updateTime     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant (tenantId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='余额账户';

-- 内部超级租户配置
CREATE TABLE IF NOT EXISTS ykt_aisaas_internal_tenant_config (
  id                 BIGINT       NOT NULL,
  tenantId           BIGINT       NOT NULL,
  tenantType         VARCHAR(32)  NOT NULL,
  isUnlimited        TINYINT      NOT NULL DEFAULT 1,
  rateLimitOverride  VARCHAR(512) NOT NULL DEFAULT '{}',
  internalApiKey     VARCHAR(64)  NOT NULL,
  webhookUrl         VARCHAR(512) DEFAULT NULL,
  metadata           VARCHAR(1024) NOT NULL DEFAULT '{}',
  status             TINYINT      NOT NULL DEFAULT 1,
  createTime         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant (tenantId),
  KEY idx_type (tenantType, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='内部超级租户配置';
