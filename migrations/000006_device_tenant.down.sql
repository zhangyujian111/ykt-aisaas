ALTER TABLE ykt_aisaas_tenant DROP COLUMN bindCode;
DROP TABLE IF EXISTS ykt_aisaas_recharge_order;
DROP TABLE IF EXISTS ykt_aisaas_user_device;
DROP TABLE IF EXISTS ykt_aisaas_user;
ALTER TABLE ykt_aisaas_tenant DROP INDEX uk_device, DROP COLUMN deviceId, DROP COLUMN tenantType;
