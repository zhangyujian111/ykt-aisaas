-- ============================================================================
-- V2: Metering DLQ（Redis Stream 重试超限后死信落库表）
-- ----------------------------------------------------------------------------
-- 对应需求：
--   P0-fixes Fix 4（Redis Stream 改造）
--   内 internal/platform/metering/dlq.go
--
-- 触发场景：
--   Stream XREADGROUP 消费失败 → 内部重试 3 次仍失败 → 落库本表 + 告警
--   运营后台可查询本表决定是否人工 replay
-- ============================================================================

CREATE TABLE IF NOT EXISTS ykt_aisaas_metering_dlq (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    -- 原始 stream 消息 ID（Redis Stream ID，例："1700000000000-0"）
    stream_id       VARCHAR(64)  NOT NULL,
    -- 重试次数（首次 = 1，超过 3 次后落库）
    retry_count     SMALLINT     NOT NULL DEFAULT 0,
    -- 业务载荷（JSON 字符串，原始 Record bytes）
    payload         JSON         NOT NULL,
    -- 错误信息（最后一次失败原因）
    last_error      TEXT         NOT NULL,
    -- 重试状态：pending / resolved / abandoned
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    -- 首次失败时间
    first_failed_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 最后一次失败时间
    last_failed_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    -- 人工处置时间（resolved/abandoned 时填写）
    resolved_at     TIMESTAMP    NULL,
    -- 处置备注
    resolved_note   VARCHAR(512) NULL,
    PRIMARY KEY (id),
    -- 按状态 + 首次失败时间查询（运维查 pending 列表）
    KEY idx_status_first_failed (status, first_failed_at),
    -- 按 stream_id 查重
    UNIQUE KEY uk_stream_id (stream_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='metering Redis Stream 死信队列（重试超限后落库）';