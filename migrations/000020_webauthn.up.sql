-- V20: WebAuthn / Passkeys 支持
-- V6-P 阶段：替代 TOTP MFA，支持 platform authenticator + roaming authenticator + passkeys

-- sys_user_webauthn: WebAuthn credential 存储
CREATE TABLE IF NOT EXISTS sys_user_webauthn (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL,
    credential_id VARBINARY(100) NOT NULL UNIQUE COMMENT 'WebAuthn credential ID (unique)',
    public_key VARBINARY(512) NOT NULL COMMENT '公钥 (512 bytes 足够)',
    attestation_type VARCHAR(50) COMMENT 'attestation 类型: none/direct/indirect',
    transports VARCHAR(255) COMMENT 'USB/NFC/BLE/internal (逗号分隔)',
    sign_count BIGINT UNSIGNED DEFAULT 0 COMMENT 'authenticator sign counter (防重放)',
    aaguid VARBINARY(16) COMMENT 'Authenticator Attestation GUID (型号标识)',
    name VARCHAR(100) DEFAULT 'Default' COMMENT 'credential 名称 (用户自定义)',
    registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '注册时间',
    last_used_at TIMESTAMP NULL COMMENT '最后使用时间',
    INDEX idx_user_webauthn_user (user_id),
    INDEX idx_user_webauthn_credential (credential_id(64))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='WebAuthn credentials';

-- sys_user_mfa 增加 webauthn 类型（与 TOTP/SMS/Email 并存）
-- 注意: 假设 V17 已创建 sys_user_mfa 表，此处仅修改 mfa_type 枚举
-- 如果 sys_user_mfa 表不存在，先创建（兼容新部署）
CREATE TABLE IF NOT EXISTS sys_user_mfa (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    user_id BIGINT NOT NULL UNIQUE,
    mfa_enabled BOOLEAN DEFAULT FALSE COMMENT 'MFA 是否启用',
    mfa_type VARCHAR(20) DEFAULT 'totp' COMMENT 'MFA 类型: totp/sms/email/webauthn',
    totp_secret_encrypted TEXT COMMENT 'TOTP 密钥 (AES-256 加密)',
    sms_phone_encrypted VARCHAR(255) COMMENT '手机号 (AES-256 加密)',
    email_address VARCHAR(255) COMMENT '邮箱地址',
    backup_codes JSON COMMENT '8 个备用码 (bcrypt 加密)',
    last_used_at TIMESTAMP NULL COMMENT '最后使用时间',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_user_mfa_user (user_id),
    INDEX idx_user_mfa_type (mfa_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户 MFA 配置';

-- 如果 mfa_type 是 ENUM 类型（MySQL 8.0+），需要用 ALTER TABLE 修改
-- 对于旧表，可能需要重建表或使用 CHECK 约束
-- 兼容方案：如果是 ENUM，改为 VARCHAR(20)
ALTER TABLE sys_user_mfa
    MODIFY COLUMN mfa_type VARCHAR(20) DEFAULT 'totp' COMMENT 'MFA 类型: totp/sms/email/webauthn';
