// Package tenantm 租户域：设备即租户（auto-provisioning）+ 门户用户。
package tenantm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// TenantDO ykt_aisaas_tenant（系统表，查询走 SkipHooks 显式条件）。
type TenantDO struct {
	ID         int64      `gorm:"column:id;primaryKey" json:"id"`
	Code       string     `gorm:"column:code" json:"code"`
	TenantType string     `gorm:"column:tenantType" json:"tenantType"`
	DeviceID   string     `gorm:"column:deviceId" json:"deviceId"`
	BindCode   string     `gorm:"column:bindCode" json:"bindCode"`
	Name       string     `gorm:"column:name" json:"name"`
	Status     int8       `gorm:"column:status" json:"status"`
	PlanID     int64      `gorm:"column:planId" json:"planId"`
	ExpireTime *time.Time `gorm:"column:expireTime" json:"expireTime"`
	CreateTime time.Time  `gorm:"column:createTime" json:"createTime"`
	IsDeleted  int8       `gorm:"column:isDeleted" json:"-"`
}

func (TenantDO) TableName() string { return "ykt_aisaas_tenant" }

// DeviceTenantService 设备租户自动开户。
type DeviceTenantService struct {
	db  *gorm.DB
	rdb *redisx.Client
	mu  sync.Mutex // 序列化 upsert，防同设备并发开户
}

func NewDeviceTenantService(db *gorm.DB, rdb *redisx.Client) *DeviceTenantService {
	return &DeviceTenantService{db: db, rdb: rdb}
}

// EnsureDeviceTenant 设备上报 → 租户 upsert（幂等）。返回设备租户 ID。
// 事务：tenant(code=dev:{deviceId}, type=DEVICE, 绑定码) + balance + 免费套餐配额 + Redis 映射缓存。
func (s *DeviceTenantService) EnsureDeviceTenant(ctx context.Context, deviceID string) (int64, error) {
	if deviceID == "" || len(deviceID) > 128 {
		return 0, errs.New(errs.InvalidParam, "deviceId 无效")
	}

	// 1) Redis 快路径
	if tid, err := s.rdb.Get(ctx, deviceKey(deviceID)).Int64(); err == nil && tid > 0 {
		return tid, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 2) DB 查
	var do TenantDO
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(TenantDO{}.TableName()).
		Where("deviceId = ? AND isDeleted = 0", deviceID).Take(&do).Error
	if err == nil {
		s.cacheDevice(ctx, deviceID, do.ID)
		return do.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, errs.Wrap(errs.Internal, err)
	}

	// 3) 创建（免费配额模板）
	var planQuotas string
	_ = s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_plan").Select("quotas").
		Where("code = 'free'").Scan(&planQuotas)

	tenantID := ids.Next()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO ykt_aisaas_tenant
			(id, code, tenantType, deviceId, bindCode, name, status, planId)
			VALUES (?, ?, 'DEVICE', ?, ?, ?, 1, 1)
			ON DUPLICATE KEY UPDATE id = id`,
			tenantID, "dev:"+deviceID, deviceID, genBindCode(), "设备 "+deviceID).Error; err != nil {
			return err
		}
		// 唯一键冲突（并发已建）→ 重查
		var existing int64
		if err := tx.Session(&gorm.Session{SkipHooks: true}).
			Table(TenantDO{}.TableName()).Select("id").
			Where("deviceId = ?", deviceID).Scan(&existing).Error; err != nil {
			return err
		}
		if existing > 0 && existing != tenantID {
			tenantID = existing
			return nil
		}
		if err := tx.Exec(`INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId) VALUES (?, ?)`,
			ids.Next(), tenantID).Error; err != nil {
			return err
		}
		// 免费套餐配额（本月）
		for dim, limit := range parseQuotas(planQuotas) {
			if limit <= 0 {
				continue
			}
			if err := tx.Exec(`INSERT IGNORE INTO ykt_aisaas_quota
				(id, tenantId, periodStart, periodEnd, dimension, limitValue)
				VALUES (?, ?, CURRENT_DATE, LAST_DAY(CURRENT_DATE) + INTERVAL 1 DAY, ?, ?)`,
				ids.Next(), tenantID, dim, limit).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, errs.Wrap(errs.Internal, err)
	}
	s.cacheDevice(ctx, deviceID, tenantID)
	slog.Info("device tenant provisioned", "deviceId", deviceID, "tenantId", tenantID)
	return tenantID, nil
}

// GetByDevice 按设备查租户。
func (s *DeviceTenantService) GetByDevice(ctx context.Context, deviceID string) (*TenantDO, error) {
	var do TenantDO
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(TenantDO{}.TableName()).
		Where("deviceId = ? AND isDeleted = 0", deviceID).Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "设备未开户")
	}
	return &do, err
}

func (s *DeviceTenantService) cacheDevice(ctx context.Context, deviceID string, tid int64) {
	s.rdb.Set(ctx, deviceKey(deviceID), tid, 24*time.Hour)
}

func deviceKey(deviceID string) string {
	return fmt.Sprintf("aisaas:device:tenant:%s", deviceID)
}

func genBindCode() string {
	n := rand.Intn(1000000)
	return fmt.Sprintf("%06d", n)
}

// parseQuotas 简易 JSON 数字解析（{"dim":n,...}），无依赖。
func parseQuotas(s string) map[string]int64 {
	out := map[string]int64{}
	if s == "" {
		return out
	}
	depth := 0
	var key string
	var num string
	inKey := false
	flush := func() {
		if key != "" && num != "" {
			v := int64(0)
			p := 1
			for i := len(num) - 1; i >= 0; i-- {
				v += int64(num[i]-'0') * int64(p)
				p *= 10
			}
			out[key] = v
		}
		key, num = "", ""
	}
	for _, c := range s {
		switch {
		case c == '{':
			depth++
		case c == '}':
			flush()
			depth--
		case c == '"':
			inKey = !inKey
		case c >= '0' && c <= '9':
			num += string(c)
		case c == ',':
			flush()
			inKey = false
		default:
			if inKey {
				key += string(c)
			}
		}
	}
	return out
}

var _ = database.BaseDO{} // 保持 import（TenantDO 不嵌套 BaseDO，系统表手写全字段）
