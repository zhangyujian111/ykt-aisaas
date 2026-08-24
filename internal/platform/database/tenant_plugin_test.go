package database_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/tenant"
)

// itemDO 模拟租户业务表（含 tenantId，不在豁免名单）。
type itemDO struct {
	database.BaseDO
	Name string `gorm:"column:name"`
}

func (itemDO) TableName() string { return "biz_item" }

func mustDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&itemDO{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Use(&database.TenantPlugin{SkipTables: map[string]bool{"sys_global": true}}); err != nil {
		t.Fatalf("use plugin: %v", err)
	}
	return db
}

func seed(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []itemDO{
		{BaseDO: database.BaseDO{ID: 1, TenantID: 100}, Name: "a-100"},
		{BaseDO: database.BaseDO{ID: 2, TenantID: 100}, Name: "b-100"},
		{BaseDO: database.BaseDO{ID: 3, TenantID: 200}, Name: "c-200"},
	}
	for _, r := range rows {
		if err := db.Session(&gorm.Session{SkipHooks: true}).Create(&r).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func withT(ctx context.Context, tid int64) context.Context { return tenant.With(ctx, tid) }

// 租户 100 查询只能看到自己的 2 行。
func TestQueryIsolation(t *testing.T) {
	db := mustDB(t)
	seed(t, db)

	var got []itemDO
	if err := db.WithContext(withT(context.Background(), 100)).Find(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tenant 100 should see 2 rows, got %d", len(got))
	}
	for _, r := range got {
		if r.TenantID != 100 {
			t.Fatalf("leaked row: %+v", r)
		}
	}
}

// 无租户上下文的查询必须失败（fail-fast，绝不回退为全表查询）。
func TestQueryWithoutContextFails(t *testing.T) {
	db := mustDB(t)
	seed(t, db)

	var got []itemDO
	err := db.WithContext(context.Background()).Find(&got).Error
	if err == nil {
		t.Fatalf("expected TenantContextLost, got nil (rows=%d)", len(got))
	}
	e := errs.From(err)
	if e.Code != errs.TenantContextLost {
		t.Fatalf("expected TenantContextLost, got %v", e.Code)
	}
}

// Create 自动填充 tenantId，且无法伪造他人租户。
func TestCreateFillsTenant(t *testing.T) {
	db := mustDB(t)
	ctx := withT(context.Background(), 300)

	it := itemDO{BaseDO: database.BaseDO{ID: 9, TenantID: 999}, Name: "x"} // 伪造 999
	if err := db.WithContext(ctx).Create(&it).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if it.TenantID != 300 {
		t.Fatalf("tenantId should be overwritten to 300, got %d", it.TenantID)
	}
}

// Update/Delete 同样被租户过滤：改不到别人家的行。
func TestUpdateDeleteIsolation(t *testing.T) {
	db := mustDB(t)
	seed(t, db)
	ctx := withT(context.Background(), 200)

	res := db.WithContext(ctx).Model(&itemDO{}).Where("id = ?", 1). // id=1 属于租户 100
									Update("name", "hacked")
	if res.Error != nil {
		t.Fatalf("update: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Fatalf("cross-tenant update succeeded, affected=%d", res.RowsAffected)
	}

	res = db.WithContext(ctx).Where("id = ?", 2).Delete(&itemDO{})
	if res.Error != nil {
		t.Fatalf("delete: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Fatalf("cross-tenant delete succeeded, affected=%d", res.RowsAffected)
	}
}

// 豁免表不追加租户条件。
func TestSkipTable(t *testing.T) {
	db := mustDB(t)
	if err := db.Session(&gorm.Session{SkipHooks: true}).
		Exec("CREATE TABLE IF NOT EXISTS sys_global (id INTEGER PRIMARY KEY, v TEXT)").Error; err != nil {
		t.Fatalf("create skip table: %v", err)
	}
	if err := db.Session(&gorm.Session{SkipHooks: true}).
		Exec("INSERT INTO sys_global (id, v) VALUES (1, 'x')").Error; err != nil {
		t.Fatalf("insert skip table: %v", err)
	}
	// 无租户上下文也应放行（豁免表）
	var n int64
	if err := db.Table("sys_global").Count(&n).Error; err != nil {
		t.Fatalf("count skip table without ctx: %v", err)
	}
	if n != 1 {
		t.Fatalf("skip table count = %d, want 1", n)
	}
}
