-- 000022_ai_traces.up.sql
-- V6-T: AI 推理 Trace 元数据表
-- 存储 AI 推理请求的 trace 元数据，用于离线分析和成本归因

CREATE TABLE IF NOT EXISTS ykt_aisaas_ai_traces (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    trace_id        VARCHAR(64)     NOT NULL                COMMENT 'OTel TraceID（hex）',
    span_id         VARCHAR(32)     NOT NULL                COMMENT 'OTel SpanID（hex）',
    parent_span_id  VARCHAR(32)     DEFAULT ''              COMMENT '父 SpanID（hex）',
    tenant_id       BIGINT          DEFAULT 0               COMMENT '租户 ID',
    model_id        VARCHAR(128)    NOT NULL                COMMENT '模型 ID',
    provider        VARCHAR(64)     NOT NULL                COMMENT 'AI Provider: openai/anthropic/qwen',
    operation       VARCHAR(64)     NOT NULL                COMMENT '操作类型: chat/embedding/vector_search/rag',
    input_tokens    INT             DEFAULT 0               COMMENT '输入 Token 数',
    output_tokens   INT             DEFAULT 0               COMMENT '输出 Token 数',
    total_tokens    INT             DEFAULT 0               COMMENT '总 Token 数',
    duration_ms     DOUBLE          DEFAULT 0               COMMENT '耗时（毫秒）',
    first_token_latency_ms DOUBLE   DEFAULT 0               COMMENT '首 Token 延迟（毫秒），仅流式',
    status_code     TINYINT         DEFAULT 0               COMMENT '状态码: 0=OK, 1=Error',
    error_message   TEXT                                    COMMENT '错误信息',
    metadata        JSON                                    COMMENT '扩展元数据（tags/custom attributes）',
    start_time      DATETIME(3)     NOT NULL                COMMENT 'Span 开始时间',
    end_time        DATETIME(3)                             COMMENT 'Span 结束时间',
    create_time     DATETIME(3)     DEFAULT CURRENT_TIMESTAMP(3),

    -- 索引
    INDEX idx_trace_id (trace_id),
    INDEX idx_tenant_id (tenant_id),
    INDEX idx_model_id (model_id),
    INDEX idx_provider (provider),
    INDEX idx_operation (operation),
    INDEX idx_start_time (start_time),
    INDEX idx_status_code (status_code),
    INDEX idx_tenant_start (tenant_id, start_time)          COMMENT '租户时间范围查询'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI 推理 Trace 元数据表（V6-T）';