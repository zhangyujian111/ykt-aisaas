-- 000005_billing.up.sql — 计费域补全
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_balance_transaction (
  id           BIGINT      NOT NULL,
  tenantId     BIGINT      NOT NULL,
  type         VARCHAR(32) NOT NULL COMMENT 'recharge/consume',
  amountCents  BIGINT      NOT NULL COMMENT '正=入账 负=出账',
  balanceAfter BIGINT      NOT NULL COMMENT '操作后余额',
  bizType      VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'llm/tts/asr/rag/mcp',
  bizRefId     VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'requestId',
  remark       VARCHAR(256) DEFAULT NULL,
  createTime   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_tenant_time (tenantId, createTime),
  KEY idx_biz (tenantId, bizType, bizRefId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='余额流水';

CREATE TABLE IF NOT EXISTS ykt_aisaas_plan (
  id           BIGINT       NOT NULL,
  code         VARCHAR(64)  NOT NULL,
  name         VARCHAR(64)  NOT NULL,
  priceMonthly DECIMAL(10,2) NOT NULL DEFAULT 0,
  quotas       VARCHAR(1024) NOT NULL DEFAULT '{}' COMMENT '{"llm_tokens_in":100000,...}',
  features     VARCHAR(512) NOT NULL DEFAULT '[]',
  status       TINYINT      NOT NULL DEFAULT 1,
  createTime   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  isDeleted    TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='套餐';

CREATE TABLE IF NOT EXISTS ykt_aisaas_subscription (
  id          BIGINT   NOT NULL,
  tenantId    BIGINT   NOT NULL,
  planId      BIGINT   NOT NULL,
  periodStart DATETIME NOT NULL,
  periodEnd   DATETIME NOT NULL,
  autoRenew   TINYINT  NOT NULL DEFAULT 0,
  status      TINYINT  NOT NULL DEFAULT 1 COMMENT '1生效 2过期',
  createTime  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted   TINYINT  NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_period (tenantId, periodEnd)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订阅';

CREATE TABLE IF NOT EXISTS ykt_aisaas_bill (
  id             BIGINT      NOT NULL,
  tenantId       BIGINT      NOT NULL,
  periodStart    DATE        NOT NULL,
  periodEnd      DATE        NOT NULL,
  usageFeeCents  BIGINT      NOT NULL DEFAULT 0,
  totalFeeCents  BIGINT      NOT NULL DEFAULT 0,
  paidCents      BIGINT      NOT NULL DEFAULT 0,
  usageBreakdown VARCHAR(2048) NOT NULL DEFAULT '{}',
  status         TINYINT     NOT NULL DEFAULT 0 COMMENT '0待付 1已付',
  payTime        DATETIME    DEFAULT NULL,
  createTime     DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime     DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_period (tenantId, periodStart)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='月度账单';

-- 余额字段补齐（1001 演示租户已初始化过，此处幂等）
INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId, balanceCents) VALUES (2, 2002, 0);
INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId, balanceCents) VALUES (3, 1, 0);

-- 套餐：免费 / 标准
INSERT IGNORE INTO ykt_aisaas_plan (id, code, name, priceMonthly, quotas, features)
VALUES
  (1, 'free', '免费版', 0,
   '{"llm_tokens_in":100000,"llm_tokens_out":100000,"tts_chars":10000,"asr_seconds":300}',
   '["llm","tts","asr","rag"]'),
  (2, 'standard', '标准版', 99.00,
   '{"llm_tokens_in":5000000,"llm_tokens_out":5000000,"tts_chars":500000,"asr_seconds":20000}',
   '["llm","tts","asr","rag","mcp"]');
