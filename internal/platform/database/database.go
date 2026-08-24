// Package database 初始化 GORM 并注册多租户插件。
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"ykt.dev/aisaas/internal/platform/config"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// BaseDO 所有含租户业务表的公共字段。列名 camelCase 与 DATABASE.md 对齐。
type BaseDO struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	TenantID   int64     `gorm:"column:tenantId" json:"tenantId"`
	CreateTime time.Time `gorm:"column:createTime;autoCreateTime" json:"createTime"`
	UpdateTime time.Time `gorm:"column:updateTime;autoUpdateTime" json:"updateTime"`
}

// IsDeleted 字段在各 DO 自行内嵌（soft delete 用 gorm.DeletedAt 太重，用 int8 手动过滤即可）。

// Open 打开 MySQL 连接并注册租户插件。
func Open(cfg *config.MySQL, skipTables []string, debug bool) (*gorm.DB, error) {
	opts := &gorm.Config{
		// 关闭 snake_case/复数转换：表名列名以 struct tag 显式声明为准（camelCase）
		NamingStrategy: schema.NamingStrategy{NoLowerCase: true, SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Warn),
		NowFunc:        time.Now,
	}
	if debug {
		opts.Logger = logger.Default.LogMode(logger.Info)
	}
	db, err := gorm.Open(mysql.Open(cfg.DSN), opts)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifeSec) * time.Second)

	if err := db.Use(&TenantPlugin{SkipTables: toSet(skipTables)}); err != nil {
		return nil, fmt.Errorf("register tenant plugin: %w", err)
	}
	slog.Info("mysql connected, tenant plugin registered", "skipTables", len(skipTables))
	return db, nil
}

func toSet(in []string) map[string]bool {
	m := make(map[string]bool, len(in))
	for _, s := range in {
		m[s] = true
	}
	return m
}

// TenantPlugin GORM 多租户插件：
// - Query/Update/Delete 自动追加 WHERE tenantId = ?
// - Create 自动填充 tenantId
// 上下文缺失时返回 TenantContextLost 错误（fail-fast），绝不含 tenantId=0 查询。
type TenantPlugin struct {
	SkipTables map[string]bool
}

func (p *TenantPlugin) Name() string { return "ykt_tenant" }

func (p *TenantPlugin) Initialize(db *gorm.DB) error {
	db.Callback().Query().Before("gorm:query").Register("tenant:query", p.scope)
	db.Callback().Update().Before("gorm:update").Register("tenant:update", p.scope)
	db.Callback().Delete().Before("gorm:delete").Register("tenant:delete", p.scope)
	db.Callback().Create().Before("gorm:create").Register("tenant:create", p.fill)
	return nil
}

func (p *TenantPlugin) scope(tx *gorm.DB) {
	if p.skip(tx) || tx.Statement.SkipHooks {
		return
	}
	tid, ok := tenant.FromSafe(tx.Statement.Context)
	if !ok {
		tx.AddError(errs.New(errs.TenantContextLost))
		return
	}
	tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{
		clause.Eq{
			Column: clause.Column{Table: clause.CurrentTable, Name: "tenantId"},
			Value:  tid,
		},
	}})
}

func (p *TenantPlugin) fill(tx *gorm.DB) {
	if p.skip(tx) || tx.Statement.SkipHooks {
		return
	}
	tid, ok := tenant.FromSafe(tx.Statement.Context)
	if !ok {
		tx.AddError(errs.New(errs.TenantContextLost))
		return
	}
	tx.Statement.SetColumn("TenantID", tid)
}

func (p *TenantPlugin) skip(tx *gorm.DB) bool {
	return p.SkipTables[tx.Statement.Table]
}

// WithTenant 以指定租户执行（内部 API / 任务场景）。例：
//
//	database.WithTenant(ctx, tid, func(c context.Context) error { return repo.X(c, ...) })
func WithTenant(ctx context.Context, tenantID int64, fn func(ctx context.Context) error) error {
	return fn(tenant.With(ctx, tenantID))
}
