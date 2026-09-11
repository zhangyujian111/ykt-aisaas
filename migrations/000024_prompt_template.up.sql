-- 000024_prompt_template.up.sql — Prompt 模板配置（对齐 Java 版 TemplateView）
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_prompt_template (
  id           BIGINT       NOT NULL,
  tenantId     BIGINT       DEFAULT NULL COMMENT 'NULL=全局共享',
  templateKey  VARCHAR(64)  NOT NULL COMMENT '模板 key（程序引用标识，如 memory_summary）',
  name         VARCHAR(128) NOT NULL COMMENT '模板中文名',
  category     VARCHAR(32)  NOT NULL DEFAULT 'custom' COMMENT 'memory_summary / persona / system / custom',
  description  VARCHAR(512) DEFAULT NULL,
  content      MEDIUMTEXT   NOT NULL COMMENT 'Go template / 纯文本',
  variables    JSON         DEFAULT NULL COMMENT 'JSON 数组：模板变量说明（仅元信息）',
  isDefault    TINYINT      NOT NULL DEFAULT 0 COMMENT '1=默认',
  status       TINYINT      NOT NULL DEFAULT 1 COMMENT '0停用 1启用',
  createTime   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted    TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_key (tenantId, templateKey, isDeleted),
  KEY idx_tenant_category (tenantId, category, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Prompt 模板配置';
