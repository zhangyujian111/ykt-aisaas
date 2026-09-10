-- ============================================================================
-- V4-A: AI Quota Anomaly Detection Rules Engine
-- ----------------------------------------------------------------------------
-- 对应需求：
--   ykt-aisaas/internal/platform/anomaly/
--   4 类规则类型 + 3 级严重度 + 3 种通知渠道
-- ============================================================================

CREATE TABLE IF NOT EXISTS ykt_aisaas_anomaly_rules (
    id VARCHAR(64) PRIMARY KEY,
    tenantId VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    ruleType VARCHAR(32) NOT NULL COMMENT 'quota_burst / token_spike / error_rate / cost_anomaly',
    metric VARCHAR(64) NOT NULL COMMENT 'quota_per_minute / tokens_per_call / error_rate / cost_per_hour',
    thresholdValue DECIMAL(15,6) NOT NULL,
    windowSizeMs INT NOT NULL COMMENT 'time window in milliseconds',
    severity VARCHAR(16) DEFAULT 'warn' COMMENT 'info / warn / critical',
    enabled BOOLEAN DEFAULT TRUE,
    notificationChannels JSON COMMENT '["slack","webhook","email"]',
    createdAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updatedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_rules_tenant (tenantId, enabled),
    INDEX idx_rules_type (ruleType, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='AI anomaly detection rules';

CREATE TABLE IF NOT EXISTS ykt_aisaas_anomaly_events (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    ruleId VARCHAR(64) NOT NULL,
    tenantId VARCHAR(64) NOT NULL,
    triggeredAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metricValue DECIMAL(15,6),
    severity VARCHAR(16),
    message TEXT,
    notified BOOLEAN DEFAULT FALSE,
    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledgedAt TIMESTAMP NULL,
    INDEX idx_events_rule_time (ruleId, triggeredAt DESC),
    INDEX idx_events_tenant_time (tenantId, triggeredAt DESC),
    INDEX idx_events_severity (severity, triggeredAt DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='AI anomaly detection events';

-- Seed builtin rules (4 类规则)
INSERT INTO ykt_aisaas_anomaly_rules (id, tenantId, name, description, ruleType, metric, thresholdValue, windowSizeMs, severity, notificationChannels, enabled) VALUES
    ('builtin_quota_burst', '', 'Quota Burst (1min quota > 1000)', 'Trigger when quota consumption exceeds 1000 in 1 minute', 'quota_burst', 'quota_per_minute', 1000, 60000, 'warn', '["slack","webhook"]', TRUE),
    ('builtin_token_spike', '', 'Token Spike (single call > 8000)', 'Trigger when a single API call uses more than 8000 tokens', 'token_spike', 'tokens_per_call', 8000, 300000, 'warn', '["slack"]', TRUE),
    ('builtin_error_rate', '', 'Error Rate (5min 5xx > 5%)', 'Trigger when 5xx error rate exceeds 5% in 5 minutes', 'error_rate', 'error_rate', 0.05, 300000, 'critical', '["slack","email","webhook"]', TRUE),
    ('builtin_cost_anomaly', '', 'Cost Anomaly (1h cost > ¥100)', 'Trigger when hourly cost exceeds ¥100', 'cost_anomaly', 'cost_per_hour', 100, 3600000, 'critical', '["slack","email"]', TRUE);