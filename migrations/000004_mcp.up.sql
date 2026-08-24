-- 000004_mcp.up.sql — MCP 工具域
-- 简化说明：mcp_server 表延后（MVP 工具直接带 endpoint，server 维度在接入外部 MCP server 时补）
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_mcp_tool (
  id            BIGINT       NOT NULL,
  tenantId      BIGINT       DEFAULT NULL COMMENT 'NULL=全局工具（需租户绑定），非NULL=租户私有',
  toolName      VARCHAR(64)  NOT NULL,
  description   VARCHAR(512) NOT NULL DEFAULT '',
  toolType      VARCHAR(16)  NOT NULL DEFAULT 'http' COMMENT 'http/builtin',
  endpoint      VARCHAR(512) DEFAULT NULL COMMENT 'http 工具的调用地址（POST JSON）',
  method        VARCHAR(8)   NOT NULL DEFAULT 'POST',
  authToken     VARCHAR(256) DEFAULT NULL COMMENT 'Bearer token（可空）',
  inputSchema   TEXT         NOT NULL COMMENT 'JSON Schema 参数定义',
  timeoutMs     INT          NOT NULL DEFAULT 10000,
  callCount     BIGINT       NOT NULL DEFAULT 0,
  status        TINYINT      NOT NULL DEFAULT 1,
  createTime    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted     TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_tenant (tenantId, isDeleted),
  KEY idx_name (toolName, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='MCP 工具';

CREATE TABLE IF NOT EXISTS ykt_aisaas_tenant_mcp_binding (
  id         BIGINT   NOT NULL,
  tenantId   BIGINT   NOT NULL,
  mcpToolId  BIGINT   NOT NULL,
  enabled    TINYINT  NOT NULL DEFAULT 1,
  createTime DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenant_tool (tenantId, mcpToolId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户-MCP 工具绑定';
