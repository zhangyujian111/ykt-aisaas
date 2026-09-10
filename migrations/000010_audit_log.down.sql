-- 000010_audit_log.down.sql — 回滚 Audit Log 审计日志
-- 注意：分区表需要先移除分区再删除表
SET NAMES utf8mb4;

-- 使用 REORGANIZE PARTITION 将所有分区合并后删除（MySQL 8.0 兼容）
-- 但更安全的做法是直接 DROP TABLE（分区会一并删除）
DROP TABLE IF EXISTS ykt_aisaas_audit_log;
