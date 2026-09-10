-- 000014_rag_log.up.sql — RAG 检索日志表（供 ykt-admin 运营后台查询召回率）
-- 对应需求：P2-D 第 3 数据源 AI 监控
-- 写入方：ykt-aisaas RAG 检索服务（每次检索后追加一条）
SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS ykt_aisaas_rag_log (
    id                  BIGINT       NOT NULL COMMENT '主键',
    tenant_id           BIGINT       NOT NULL COMMENT '租户ID',
    device_id           VARCHAR(128) DEFAULT NULL COMMENT '设备ID',
    knowledge_base_id   BIGINT       DEFAULT NULL COMMENT '知识库ID',
    query               TEXT         COMMENT '检索查询文本',
    retrieved_chunks    JSON         DEFAULT NULL COMMENT '检索到的 chunks（JSON 数组）',
    relevance_scores    JSON         DEFAULT NULL COMMENT '各 chunk 相关性分数',
    top_score           NUMERIC(5,4) DEFAULT NULL COMMENT '最高分 chunk 的相关性分数',
    latency_ms          INT          DEFAULT NULL COMMENT '检索耗时（毫秒）',
    created_at          TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (id),
    KEY idx_tenant_device_time (tenant_id, device_id, created_at),
    KEY idx_tenant_time (tenant_id, created_at),
    KEY idx_kb_time (knowledge_base_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='RAG 检索日志（供运营后台查询召回率）';