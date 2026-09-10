-- ============================================================================
-- V6-A: A/B Testing Platform
-- ----------------------------------------------------------------------------
-- 对应需求：
--   ykt-aisaas/internal/platform/abtest/
--   4 张表：experiments + variants + assignments + events
--   粘性分配（哈希）+ Z 检验统计分析
-- ============================================================================

-- 1. 实验定义
CREATE TABLE IF NOT EXISTS ykt_aisaas_ab_experiments (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    status ENUM('draft', 'running', 'paused', 'completed') DEFAULT 'draft',
    hypothesis TEXT COMMENT '业务假设',
    primary_metric VARCHAR(100) COMMENT '主要指标',
    secondary_metrics JSON COMMENT '次要指标',
    min_sample_size INT DEFAULT 10000,
    expected_effect_size DECIMAL(5,4) COMMENT '期望效应大小',
    statistical_power DECIMAL(3,2) DEFAULT 0.80,
    significance_level DECIMAL(3,2) DEFAULT 0.05,
    created_by BIGINT,
    stopped_by BIGINT,
    started_at TIMESTAMP NULL,
    ended_at TIMESTAMP NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_experiment_name (name),
    INDEX idx_experiment_status (status, started_at),
    INDEX idx_experiment_created_by (created_by, created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='A/B testing experiments';

-- 2. 实验变体
CREATE TABLE IF NOT EXISTS ykt_aisaas_ab_variants (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    experiment_id BIGINT NOT NULL,
    name VARCHAR(50) NOT NULL COMMENT 'control / treatment_a / treatment_b',
    allocation_percent DECIMAL(5,2) NOT NULL COMMENT '流量分配百分比',
    config JSON COMMENT '变体配置',
    is_control BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (experiment_id) REFERENCES ykt_aisaas_ab_experiments(id) ON DELETE CASCADE,
    UNIQUE KEY uk_variant_exp_name (experiment_id, name),
    INDEX idx_variant_experiment (experiment_id, is_control)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='A/B testing variants';

-- 3. 实验分配记录（粘性）
CREATE TABLE IF NOT EXISTS ykt_aisaas_ab_assignments (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    experiment_id BIGINT NOT NULL,
    variant_id BIGINT NOT NULL,
    user_id VARCHAR(100) NOT NULL COMMENT '用户/设备标识',
    tenant_id BIGINT,
    assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_assignment (experiment_id, user_id),
    INDEX idx_assignment_tenant_time (tenant_id, assigned_at DESC),
    INDEX idx_assignment_variant (variant_id, assigned_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='A/B testing user assignments (sticky)';

-- 4. 实验事件
CREATE TABLE IF NOT EXISTS ykt_aisaas_ab_events (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    experiment_id BIGINT NOT NULL,
    variant_id BIGINT NOT NULL,
    user_id VARCHAR(100) NOT NULL,
    metric_name VARCHAR(100) COMMENT '指标名',
    metric_value DECIMAL(20,4),
    properties JSON COMMENT '上下文',
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_event_exp_metric (experiment_id, metric_name, recorded_at),
    INDEX idx_event_variant_time (variant_id, recorded_at DESC),
    INDEX idx_event_user (user_id, experiment_id, metric_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='A/B testing events';