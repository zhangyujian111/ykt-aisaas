-- 000023_oss_config.up.sql — OSS 对象存储配置（对齐 Java 版 OssConfigView）
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_oss_config (
  id           BIGINT       NOT NULL,
  tenantId     BIGINT       DEFAULT NULL COMMENT 'NULL=全局共享',
  provider     VARCHAR(32)  NOT NULL DEFAULT 'aliyun' COMMENT 'aliyun / aws / tencent / minio',
  configName   VARCHAR(64)  NOT NULL COMMENT '配置名（用户自定义标识）',
  configDesc   VARCHAR(256) DEFAULT NULL,
  endpoint     VARCHAR(256) NOT NULL COMMENT 'OSS endpoint，如 oss-cn-beijing.aliyuncs.com',
  bucket       VARCHAR(128) NOT NULL COMMENT 'bucket 名',
  accessKey    VARCHAR(512) NOT NULL COMMENT 'AccessKey ID（加密）',
  secretEnc    VARCHAR(1024) NOT NULL COMMENT 'AccessKey Secret（AES 加密）',
  region       VARCHAR(32)  DEFAULT NULL COMMENT 'region（如 cn-beijing）',
  pathPrefix   VARCHAR(128) DEFAULT NULL COMMENT '对象路径前缀',
  isDefault    TINYINT      NOT NULL DEFAULT 0 COMMENT '1=租户默认 0=否',
  status       TINYINT      NOT NULL DEFAULT 1 COMMENT '0停用 1启用',
  createTime   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted    TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_name (tenantId, configName, isDeleted),
  KEY idx_tenant_status (tenantId, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='OSS 对象存储配置';
