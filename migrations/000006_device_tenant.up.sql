-- 000006_device_tenant.up.sql — 设备即租户 + 用户门户
SET NAMES utf8mb4;

-- 租户扩展：类型（USER 普通租户 / DEVICE 设备租户 / INTERNAL 内部）+ 设备标识
ALTER TABLE ykt_aisaas_tenant
  ADD COLUMN tenantType VARCHAR(16) NOT NULL DEFAULT 'USER' COMMENT 'USER/DEVICE/INTERNAL' AFTER code,
  ADD COLUMN deviceId VARCHAR(128) DEFAULT NULL COMMENT '设备租户对应的 xiaozhi 设备ID' AFTER tenantType,
  ADD UNIQUE KEY uk_device (deviceId);

UPDATE ykt_aisaas_tenant SET tenantType = 'INTERNAL' WHERE id = 1;

-- 平台用户（设备拥有者门户账号）
CREATE TABLE IF NOT EXISTS ykt_aisaas_user (
  id         BIGINT       NOT NULL,
  username   VARCHAR(64)  NOT NULL,
  password   VARCHAR(128) NOT NULL COMMENT 'bcrypt',
  nickname   VARCHAR(64)  DEFAULT NULL,
  phone      VARCHAR(32)  DEFAULT NULL,
  status     TINYINT      NOT NULL DEFAULT 1,
  lastLoginTime DATETIME  DEFAULT NULL,
  createTime DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  isDeleted  TINYINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='门户用户';

-- 用户-设备绑定（一台设备可被一个主账号绑定；绑定码在开户时生成）
CREATE TABLE IF NOT EXISTS ykt_aisaas_user_device (
  id         BIGINT      NOT NULL,
  userId     BIGINT      NOT NULL,
  deviceId   VARCHAR(128) NOT NULL,
  tenantId   BIGINT      NOT NULL COMMENT '设备租户',
  bindName   VARCHAR(64) DEFAULT NULL COMMENT '用户给设备起的别名',
  createTime DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_user_device (userId, deviceId),
  UNIQUE KEY uk_device_owner (deviceId)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户设备绑定';

-- 充值订单（在线支付桩：状态机 0待付 1已付 2取消）
CREATE TABLE IF NOT EXISTS ykt_aisaas_recharge_order (
  id          BIGINT      NOT NULL,
  orderNo     VARCHAR(64) NOT NULL,
  tenantId    BIGINT      NOT NULL,
  userId      BIGINT      NOT NULL,
  amountCents BIGINT      NOT NULL,
  payMethod   VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT 'alipay/wechat/pending',
  status      TINYINT     NOT NULL DEFAULT 0 COMMENT '0待付 1已付 2取消',
  paidTime    DATETIME    DEFAULT NULL,
  createTime  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updateTime  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_order_no (orderNo),
  KEY idx_tenant_time (tenantId, createTime)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='充值订单';

-- 设备绑定码（开户时生成，用户扫码/输入绑定）
ALTER TABLE ykt_aisaas_tenant
  ADD COLUMN bindCode VARCHAR(16) DEFAULT NULL COMMENT '设备绑定码（6位数字）' AFTER deviceId;
