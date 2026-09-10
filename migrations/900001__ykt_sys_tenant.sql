-- ykt_sys_tenant — 多租户表
--
-- 字段语义参考 ykt-admin-go/internal/repo/tenant.go 的 List/GetByID：
--   del_flag: '0' 有效 / '1' 删除（CHAR(1)）
--   status:   '0' 启用 / '1' 停用（CHAR(1)）
--
-- 命名风格沿用库内主流 camelCase（库/表都用了混合命名）。

CREATE TABLE IF NOT EXISTS ykt_sys_tenant (
    id          BIGINT       NOT NULL AUTO_INCREMENT COMMENT '租户ID',
    code        VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '租户编码',
    name        VARCHAR(128) NOT NULL DEFAULT '' COMMENT '租户名称',
    status      CHAR(1)      NOT NULL DEFAULT '0' COMMENT '状态：0=启用，1=停用',
    is_admin    TINYINT(1)   NOT NULL DEFAULT 0   COMMENT '是否超管租户：0=否，1=是',
    del_flag    CHAR(1)      NOT NULL DEFAULT '0' COMMENT '删除标记：0=有效，1=已删',
    create_time DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    update_time DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_code (code)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT ='系统租户表';

-- 默认租户：id=1（auth.go:64 在租户表为空时 fallback 到 1，必须存在）
INSERT IGNORE INTO ykt_sys_tenant (id, code, name, status, is_admin, del_flag)
VALUES (1, 'default', '默认租户', '0', 1, '0');
