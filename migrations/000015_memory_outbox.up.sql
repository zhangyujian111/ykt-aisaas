-- 000015_memory_outbox.up.sql — Memory Outbox 异步可靠写入
-- Q8 决策：v2 引入 outbox 模式，保证 memory 写入 at-least-once
-- 策略：调用方事务内同步 INSERT outbox，Worker 异步消费
SET NAMES utf8mb4;

-- ============================================================
-- ykt_aisaas_memory_outbox — 记忆写入信箱
-- ============================================================
CREATE TABLE IF NOT EXISTS ykt_aisaas_memory_outbox (
    id              BIGINT       NOT NULL AUTO_INCREMENT  COMMENT '主键',
    tenant_id       VARCHAR(64)  NOT NULL                 COMMENT '租户ID',
    device_id       VARCHAR(64)  NOT NULL                 COMMENT '设备ID',
    payload         JSON         NOT NULL                 COMMENT '消息载荷（含请求体）',
    type            VARCHAR(32)  NOT NULL DEFAULT 'memory_write' COMMENT '消息类型：memory_write/memory_extract/memory_summarize',
    status          ENUM('pending','processing','done','failed') NOT NULL DEFAULT 'pending' COMMENT '状态',
    retry_count     INT          NOT NULL DEFAULT 0       COMMENT '重试次数',
    next_retry_at   TIMESTAMP    NULL                     COMMENT '下次重试时间',
    worker_id       VARCHAR(64)  NULL                     COMMENT '处理该消息的 worker 标识',
    error_message   TEXT         NULL                     COMMENT '最后一次错误信息',
    created_at      TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at      TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',

    PRIMARY KEY (id),
    -- 索引1：Worker 按 status + next_retry_at 拉取待处理消息（核心查询）
    INDEX idx_outbox_status_next_retry (status, next_retry_at),
    -- 索引2：按设备查询历史
    INDEX idx_outbox_tenant_device (tenant_id, device_id, created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='记忆写入信箱（at-least-once）';