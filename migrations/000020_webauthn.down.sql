-- V20 Down: 回滚 WebAuthn / Passkeys

-- 删除 WebAuthn credentials 表
DROP TABLE IF EXISTS sys_user_webauthn;

-- 恢复 sys_user_mfa.mfa_type 为原有枚举（假设原为 ENUM）
-- 兼容处理：如果原来是 ENUM，重建为 ENUM；否则保持 VARCHAR
ALTER TABLE sys_user_mfa
    MODIFY COLUMN mfa_type ENUM('totp', 'sms', 'email') DEFAULT 'totp' COMMENT 'MFA 类型';
