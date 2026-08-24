-- 000003_knowledge.up.sql — RAG 知识库域
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_knowledge_base (
  id            BIGINT       NOT NULL,
  tenantId      BIGINT       NOT NULL,
  name          VARCHAR(128) NOT NULL,
  description   VARCHAR(512) DEFAULT NULL,
  embeddingModel VARCHAR(64) NOT NULL,
  embeddingDim  INT          NOT NULL DEFAULT 1024,
  chunkSize     INT          NOT NULL DEFAULT 800,
  chunkOverlap  INT          NOT NULL DEFAULT 100,
  docCount      INT          NOT NULL DEFAULT 0,
  chunkCount    INT          NOT NULL DEFAULT 0,
  status        TINYINT      NOT NULL DEFAULT 1 COMMENT '0构建中 1正常 2异常',
  createTime    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted     TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant (tenantId, isDeleted)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='知识库';

CREATE TABLE IF NOT EXISTS ykt_aisaas_knowledge_document (
  id              BIGINT       NOT NULL,
  tenantId        BIGINT       NOT NULL,
  knowledgeBaseId BIGINT       NOT NULL,
  fileName        VARCHAR(256) NOT NULL,
  fileType        VARCHAR(16)  NOT NULL DEFAULT 'txt',
  fileSize        BIGINT       NOT NULL DEFAULT 0,
  textContent     MEDIUMTEXT   COMMENT '提取后的纯文本（MVP 内联存储，MinIO 后续替换）',
  chunkCount      INT          NOT NULL DEFAULT 0,
  status          TINYINT      NOT NULL DEFAULT 0 COMMENT '0待处理 1处理中 2就绪 3失败',
  errorMsg        VARCHAR(512) DEFAULT NULL,
  createTime      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted       TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant_kb (tenantId, knowledgeBaseId, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='知识库文档';
