-- ============================================================================
-- V5-M: ML Anomaly Detection (Isolation Forest)
-- ----------------------------------------------------------------------------
-- 对应需求：
--   ykt-aisaas/internal/platform/anomaly/ml/
--   模型元数据表 + ML 规则表
-- ============================================================================

-- ML 模型元数据表
CREATE TABLE IF NOT EXISTS ykt_aisaas_ml_models (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenantId VARCHAR(64) NOT NULL COMMENT '租户ID',
    modelType VARCHAR(32) NOT NULL COMMENT '模型类型：isolation_forest',
    numTrees INT NOT NULL DEFAULT 100 COMMENT '树的数量',
    sampleSize INT NOT NULL DEFAULT 256 COMMENT '训练采样大小',
    numFeatures INT NOT NULL DEFAULT 8 COMMENT '特征维度',
    threshold FLOAT NOT NULL DEFAULT 0.6 COMMENT '异常分数阈值',
    trainedAt TIMESTAMP NOT NULL COMMENT '最后训练时间',
    sampleCount INT NOT NULL DEFAULT 0 COMMENT '训练样本数',
    createdAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updatedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_ml_models_tenant (tenantId),
    INDEX idx_ml_models_type (modelType)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='ML 模型元数据';

-- ML 异常检测规则表
CREATE TABLE IF NOT EXISTS ykt_aisaas_ml_anomaly_rules (
    id VARCHAR(64) PRIMARY KEY,
    tenantId VARCHAR(64) NOT NULL COMMENT '租户ID，空表示全局规则',
    name VARCHAR(255) NOT NULL COMMENT '规则名称',
    description TEXT COMMENT '规则描述',
    metric VARCHAR(64) NOT NULL DEFAULT 'ml_score' COMMENT '固定为 ml_score',
    thresholdValue FLOAT NOT NULL DEFAULT 0.6 COMMENT 'ML 分数阈值',
    windowSizeMs INT NOT NULL DEFAULT 300000 COMMENT '评估窗口（毫秒）',
    severity VARCHAR(16) DEFAULT 'critical' COMMENT '严重度：info / warn / critical',
    enabled BOOLEAN DEFAULT TRUE COMMENT '是否启用',
    notificationChannels JSON COMMENT '通知渠道：["slack","email","webhook"]',
    createdAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updatedAt TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_ml_rules_tenant (tenantId, enabled),
    INDEX idx_ml_rules_severity (severity, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='ML 异常检测规则';

-- Seed 内置 ML 规则
INSERT INTO ykt_aisaas_ml_anomaly_rules (id, tenantId, name, description, metric, thresholdValue, windowSizeMs, severity, notificationChannels, enabled) VALUES
    ('builtin_ml_anomaly', '', 'ML Anomaly Detection (Isolation Forest)', 'ML-based anomaly detection (Isolation Forest) for quota/token/latency/error rate', 'ml_score', 0.6, 300000, 'critical', '["slack","email","webhook"]', TRUE);
