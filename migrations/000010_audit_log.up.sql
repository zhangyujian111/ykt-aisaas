-- 000010_audit_log.up.sql — Audit Log 审计日志
-- Q6 决策：UUID v7 + trace_id + 分区策略
-- 分区策略：按 event_time 月度分区（TO_DAYS），预留 2024-01 ~ 2026-01 + p_future
-- 保留期：热数据 365 天（之后归档到冷存储）
SET NAMES utf8mb4;

-- ============================================================
-- ykt_aisaas_audit_log — 审计日志
-- 高写入只插入不更新，按 event_time 分区
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_audit_log (
  -- UUID v7（时间有序唯一 ID，客户端生成或 DB 函数）
  eventId               VARCHAR(36)  NOT NULL COMMENT 'UUID v7（时间有序唯一标识）',

  -- 多租户
  tenantId              BIGINT       NOT NULL COMMENT '租户ID',

  -- 操作者
  actorType             VARCHAR(16)  NOT NULL COMMENT '操作者类型：apikey/device/user/internal/system',
  actorId               VARCHAR(128) NOT NULL COMMENT '操作者ID（apiKey hash / device_id / user_id / ...）',
  actorIp               VARCHAR(45)  DEFAULT NULL COMMENT '操作者 IP（IPv6 支持）',

  -- 动作
  action                VARCHAR(64)  NOT NULL COMMENT '动作：quota.check/apikey.create/auth.fail/session.start/...',
  actionDetail          JSON         NULL COMMENT '动作详情（请求参数/响应码等）',

  -- 资源
  resourceType          VARCHAR(32)  DEFAULT NULL COMMENT '资源类型：quota/apikey/session/persona/memory/...',
  resourceId            VARCHAR(128) DEFAULT NULL COMMENT '资源ID',

  -- 结果
  result                VARCHAR(16)  NOT NULL COMMENT '结果：success/failure/partial',

  -- 错误信息（脱敏处理，不记录敏感上下文）
  errorCode             VARCHAR(32)  DEFAULT NULL COMMENT '错误码',
  errorMessage          VARCHAR(512) DEFAULT NULL COMMENT '错误信息（已脱敏，不含用户数据）',

  -- 时间（微秒精度）
  eventTime             DATETIME(6)  NOT NULL COMMENT '事件发生时间（微秒）',

  -- 链路追踪
  traceId               VARCHAR(64)  DEFAULT NULL COMMENT '分布式追踪 ID',
  spanId                VARCHAR(32)  DEFAULT NULL COMMENT 'Span ID',
  parentSpanId          VARCHAR(32)  DEFAULT NULL COMMENT 'Parent Span ID',

  -- 多 region 支持
  region                VARCHAR(32)  DEFAULT NULL COMMENT '区域：cn-beijing/cn-shanghai/us-east-1/...',

  -- 保留字段（扩展用）
  extra                 JSON         NULL COMMENT '扩展字段',

  PRIMARY KEY (eventId, eventTime),
  -- 注意：联合主键包含 eventTime 以支持按时间分区裁剪
  -- 如果 MySQL 要求单独主键需调整，但 InooDB 允许复合主键

  KEY idx_tenant_time (tenantId, eventTime),
  KEY idx_actor_time (actorType, actorId, eventTime),
  KEY idx_action_time (action, eventTime),
  KEY idx_trace (traceId),
  KEY idx_resource (resourceType, resourceId, eventTime),
  KEY idx_result (result, eventTime)

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
  -- 分区策略：按 event_time 月度分区（生产启用，本地开发环境暂不分区）
  COMMENT='审计日志（生产环境建议启用按 event_time 月度分区）';
