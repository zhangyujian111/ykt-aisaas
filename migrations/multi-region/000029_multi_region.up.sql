-- ============================================================================
-- 000029_multi_region.up.sql — V10-M 多区域数据层
-- 数据库: CockroachDB（兼容 MySQL migration path）
-- 迁移路径: MySQL 单区域 → CockroachDB 多区域（expand-contract 模式）
-- 一致性: 本地强一致 + 跨区域最终一致（P99 < 30s）
-- ============================================================================

-- --------------------------------------------------------------------------
-- Phase 1: 多区域用户表（地理分区）
-- 替代单区域 MySQL ykt_aisaas_tenant 表
-- region 字段用于就近路由 + 数据本地化
-- --------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sys_user_global (
    user_id     BIGINT       NOT NULL,
    username    VARCHAR(100) NOT NULL,
    region      VARCHAR(32)  NOT NULL DEFAULT 'us-east-1',
    tenant_id   BIGINT       NOT NULL,
    email       VARCHAR(255) DEFAULT NULL,
    status      SMALLINT     NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, region),
    INDEX idx_user_region (region, user_id),
    INDEX idx_user_tenant (tenant_id, region),
    UNIQUE INDEX idx_user_email (email) WHERE email IS NOT NULL
) PARTITION BY LIST (region) (
    PARTITION us_east_1 VALUES IN ('us-east-1'),
    PARTITION eu_west_1 VALUES IN ('eu-west-1'),
    PARTITION ap_southeast_1 VALUES IN ('ap-southeast-1')
);

-- --------------------------------------------------------------------------
-- Phase 2: 多区域 Session 表
-- 替代单区域 MySQL session 表（迁移 000009）
-- 基于 region 的分区 + TTL 自动过期
-- --------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_session_global (
    session_id   VARCHAR(64)  NOT NULL,
    user_id      BIGINT       NOT NULL,
    region       VARCHAR(32)  NOT NULL,
    device_id    VARCHAR(128) DEFAULT NULL,
    push_token   VARCHAR(256) DEFAULT NULL,
    ip_address   VARCHAR(45)  DEFAULT NULL,
    expires_at   TIMESTAMPTZ  NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, region),
    INDEX idx_session_user (user_id, region),
    INDEX idx_session_expires (region, expires_at),
    INDEX idx_session_device (device_id) WHERE device_id IS NOT NULL
) PARTITION BY LIST (region) (
    PARTITION us_east_1 VALUES IN ('us-east-1'),
    PARTITION eu_west_1 VALUES IN ('eu-west-1'),
    PARTITION ap_southeast_1 VALUES IN ('ap-southeast-1')
) WITH (
    ttl = 'on',
    ttl_expire_after = '7 days',
    ttl_job_cron = '@daily'
);

-- --------------------------------------------------------------------------
-- Phase 3: 多区域 AI 推理上下文表
-- 用于跨区域推理状态恢复（region failover 场景）
-- 每个区域独立存储推理上下文，支持跨区域异步复制
-- --------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS inference_context_global (
    context_id    UUID          NOT NULL DEFAULT gen_random_uuid(),
    user_id       BIGINT        NOT NULL,
    session_id    VARCHAR(64)   NOT NULL,
    region        VARCHAR(32)   NOT NULL,
    model_type    VARCHAR(32)   NOT NULL,  -- 'transformer' | 'lstm' | 'xgb'
    context_data  JSONB         NOT NULL DEFAULT '{}',
    context_hash  VARCHAR(64)   NOT NULL,  -- SHA-256 of context_data
    version       BIGINT        NOT NULL DEFAULT 1,  -- 向量时钟版本
    last_used_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (region, context_id),
    INDEX idx_ctx_user (user_id, region),
    INDEX idx_ctx_session (session_id, region),
    INDEX idx_ctx_expires (last_used_at) WHERE last_used_at < now() - INTERVAL '24 hours',
    UNIQUE INDEX idx_ctx_hash (context_hash, region)
) PARTITION BY LIST (region) (
    PARTITION us_east_1 VALUES IN ('us-east-1'),
    PARTITION eu_west_1 VALUES IN ('eu-west-1'),
    PARTITION ap_southeast_1 VALUES IN ('ap-southeast-1')
) WITH (
    ttl = 'on',
    ttl_expire_after = '72 hours',
    ttl_job_cron = '@hourly'
);

-- --------------------------------------------------------------------------
-- Phase 4: 多区域 AI 使用量聚合表
-- 替代单区域 MySQL 000013_ai_usage_daily
-- 按 region + tenant 双重聚合
-- --------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai_usage_global (
    usage_date   DATE          NOT NULL,
    tenant_id    BIGINT        NOT NULL,
    region       VARCHAR(32)   NOT NULL,
    model_type   VARCHAR(32)   NOT NULL,
    request_count BIGINT       NOT NULL DEFAULT 0,
    token_input  BIGINT        NOT NULL DEFAULT 0,
    token_output BIGINT        NOT NULL DEFAULT 0,
    latency_p50_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    latency_p99_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_count  BIGINT        NOT NULL DEFAULT 0,
    cost_cents   BIGINT        NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (usage_date, tenant_id, region, model_type),
    INDEX idx_usage_region_date (region, usage_date DESC)
);

-- --------------------------------------------------------------------------
-- Phase 5: 跨区域复制策略（CockroachDB 特有）
-- 每个分区 3 副本，跨 3 个区域分布
-- --------------------------------------------------------------------------

-- us-east-1 分区：主副本在 us-east-1，其他副本跨区域
ALTER PARTITION us_east_1 OF TABLE sys_user_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=us-east-1]]',
        range_min_bytes = 134217728,
        range_max_bytes = 536870912,
        gc.ttlseconds = 86400;

ALTER PARTITION us_east_1 OF TABLE user_session_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=us-east-1]]',
        gc.ttlseconds = 3600;

-- eu-west-1 分区：主副本在 eu-west-1
ALTER PARTITION eu_west_1 OF TABLE sys_user_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=eu-west-1]]',
        range_min_bytes = 134217728,
        range_max_bytes = 536870912,
        gc.ttlseconds = 86400;

ALTER PARTITION eu_west_1 OF TABLE user_session_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=eu-west-1]]',
        gc.ttlseconds = 3600;

-- ap-southeast-1 分区：主副本在 ap-southeast-1
ALTER PARTITION ap_southeast_1 OF TABLE sys_user_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=ap-southeast-1]]',
        range_min_bytes = 134217728,
        range_max_bytes = 536870912,
        gc.ttlseconds = 86400;

ALTER PARTITION ap_southeast_1 OF TABLE user_session_global
    CONFIGURE ZONE USING
        num_replicas = 3,
        constraints = '{"+region=us-east-1": 1, "+region=eu-west-1": 1, "+region=ap-southeast-1": 1}',
        lease_preferences = '[[+region=ap-southeast-1]]',
        gc.ttlseconds = 3600;

-- --------------------------------------------------------------------------
-- Phase 6: 跨区域变更数据捕获（CDC）
-- 启用 changefeed 用于跨区域实时同步到 OTel 可观测性
-- --------------------------------------------------------------------------
-- CREATE CHANGEFEED FOR TABLE sys_user_global, user_session_global
--     INTO 'kafka://kafka-cross-region.production:9092'
--     WITH updated, resolved='10s',
--          min_checkpoint_frequency='30s';

-- --------------------------------------------------------------------------
-- MySQL → CockroachDB 迁移注意事项
-- ============================================================================
-- 1. 语法差异:
--    - TINYINT → SMALLINT（CRDB 不支持 TINYINT）
--    - DATETIME → TIMESTAMPTZ（CRDB 推荐时区感知类型）
--    - ENGINE=InnoDB → 移除（CRDB 不需要）
--    - CHARSET=utf8mb4 → 移除（CRDB 默认 UTF-8）
--
-- 2. 事务差异:
--    - CRDB 使用 SERIALIZABLE 隔离级别（MySQL 默认 REPEATABLE READ）
--    - 需要审查所有 @Transactional 注解的传播行为
--    - 重试逻辑: CRDB 自动重试可序列化冲突（40001 错误码）
--
-- 3. 性能差异:
--    - 单行写入: CRDB ~2-5ms（MySQL ~1ms），跨区域写入: CRDB ~50-100ms
--    - 读取: CRDB follower reads 可降低延迟（AS OF SYSTEM TIME）
--    - 建议: 使用 us-east-1 作为 lease holder region 减少写放大
--
-- 4. 迁移步骤（expand-contract 模式）:
--    Phase 1 (Week 1-2): Shadow write - MySQL 主写，CRDB 双写不读取
--    Phase 2 (Week 3-4): Read replica promotion - CRDB follower reads
--    Phase 3 (Week 5-6): Write leader migration - 切换 CRDB 为主
--    Phase 4 (Post-launch): Decommission MySQL
-- ============================================================================