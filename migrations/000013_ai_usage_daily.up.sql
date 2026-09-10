-- 000013_ai_usage_daily.up.sql — AI 用量日聚合表（供 ykt-admin 运营后台查询）
-- 对应需求：P2-D 第 3 数据源 AI 监控
-- 数据来源：ykt_aisaas_usage_detail 每日聚合（可由定时任务或 metering 落库）
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_usage_daily (
    tenant_id           BIGINT       NOT NULL COMMENT '租户ID',
    usage_date          DATE         NOT NULL COMMENT '统计日期',
    model_type          VARCHAR(20)  NOT NULL COMMENT '模型类型：llm/tts/asr/embedding',
    total_calls         BIGINT       NOT NULL DEFAULT 0 COMMENT '调用次数',
    total_tokens        BIGINT       NOT NULL DEFAULT 0 COMMENT 'LLM Token 数',
    total_chars         BIGINT       NOT NULL DEFAULT 0 COMMENT 'TTS 字符数',
    total_seconds       BIGINT       NOT NULL DEFAULT 0 COMMENT 'ASR 秒数',
    total_cost          NUMERIC(15,6) NOT NULL DEFAULT 0 COMMENT '总费用（分）',
    PRIMARY KEY (tenant_id, usage_date, model_type),
    KEY idx_usage_date_model (usage_date, model_type),
    KEY idx_tenant_date (tenant_id, usage_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='AI 用量日聚合表（供运营后台查询 LLM/TTS/ASR 用量）';