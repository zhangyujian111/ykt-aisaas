// Package portal 用户门户：注册登录(JWT)/设备绑定/用量/订阅/充值。
package portal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/billing"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
)

// UserDO ykt_aisaas_user（系统表）。
type UserDO struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	Username   string    `gorm:"column:username" json:"username"`
	Password   string    `gorm:"column:password" json:"-"`
	Nickname   string    `gorm:"column:nickname" json:"nickname"`
	Phone      string    `gorm:"column:phone" json:"phone"`
	Status     int8      `gorm:"column:status" json:"status"`
	LastLogin  *time.Time `gorm:"column:lastLoginTime" json:"lastLoginTime"`
	CreateTime time.Time `gorm:"column:createTime;autoCreateTime" json:"createTime"`
	IsDeleted  int8      `gorm:"column:isDeleted" json:"-"`
}

func (UserDO) TableName() string { return "ykt_aisaas_user" }

// BindDO ykt_aisaas_user_device。
type BindDO struct {
	ID         int64     `gorm:"column:id;primaryKey" json:"id"`
	UserID     int64     `gorm:"column:userId" json:"userId"`
	DeviceID   string    `gorm:"column:deviceId" json:"deviceId"`
	TenantID   int64     `gorm:"column:tenantId" json:"tenantId"`
	BindName   string    `gorm:"column:bindName" json:"bindName"`
	CreateTime time.Time `gorm:"column:createTime;autoCreateTime" json:"createTime"`
}

func (BindDO) TableName() string { return "ykt_aisaas_user_device" }

// OrderDO ykt_aisaas_recharge_order。
type OrderDO struct {
	ID          int64      `gorm:"column:id;primaryKey" json:"id"`
	OrderNo     string     `gorm:"column:orderNo" json:"orderNo"`
	TenantID    int64      `gorm:"column:tenantId" json:"tenantId"`
	UserID      int64      `gorm:"column:userId" json:"userId"`
	AmountCents int64      `gorm:"column:amountCents" json:"amountCents"`
	PayMethod   string     `gorm:"column:payMethod" json:"payMethod"`
	Status      int8       `gorm:"column:status" json:"status"` // 0待付 1已付 2取消
	PaidTime    *time.Time `gorm:"column:paidTime" json:"paidTime"`
	CreateTime  time.Time  `gorm:"column:createTime;autoCreateTime" json:"createTime"`
}

func (OrderDO) TableName() string { return "ykt_aisaas_recharge_order" }

// Service 门户业务。
type Service struct {
	db      *gorm.DB
	billing *billing.Service
	jwtKey  []byte
}

func NewService(db *gorm.DB, b *billing.Service, jwtKey string) *Service {
	return &Service{db: db, billing: b, jwtKey: []byte(jwtKey)}
}

// ---- 认证 ----

// Register 注册。
func (s *Service) Register(ctx context.Context, username, password, nickname string) (*UserDO, error) {
	if len(username) < 3 || len(password) < 6 {
		return nil, errs.New(errs.InvalidParam, "用户名≥3位，密码≥6位")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	u := &UserDO{ID: ids.Next(), Username: username, Password: string(hash),
		Nickname: nickname, Status: 1}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isDup(err) {
			return nil, errs.New(errs.Conflict, "用户名已存在")
		}
		return nil, errs.Wrap(errs.Internal, err)
	}
	return u, nil
}

// Login 登录 → JWT。
func (s *Service) Login(ctx context.Context, username, password string) (token string, u *UserDO, err error) {
	var do UserDO
	err = s.db.WithContext(ctx).Where("username = ? AND isDeleted = 0", username).Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, errs.New(errs.TokenInvalid, "用户名或密码错误")
	}
	if err != nil {
		return "", nil, errs.Wrap(errs.Internal, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(do.Password), []byte(password)) != nil {
		return "", nil, errs.New(errs.TokenInvalid, "用户名或密码错误")
	}
	_ = s.db.WithContext(ctx).Model(&UserDO{}).Where("id = ?", do.ID).
		Update("lastLoginTime", time.Now()).Error

	claims := jwt.MapClaims{
		"uid": do.ID, "username": do.Username,
		"exp": time.Now().Add(72 * time.Hour).Unix(),
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtKey)
	if err != nil {
		return "", nil, errs.Wrap(errs.Internal, err)
	}
	return t, &do, nil
}

// ParseToken 校验 JWT → uid。
func (s *Service) ParseToken(tokenStr string) (int64, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("bad alg")
		}
		return s.jwtKey, nil
	})
	if err != nil || !tok.Valid {
		return 0, errs.New(errs.TokenInvalid)
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errs.New(errs.TokenInvalid)
	}
	uid, ok := claims["uid"].(float64)
	if !ok {
		return 0, errs.New(errs.TokenInvalid)
	}
	return int64(uid), nil
}

// ---- 设备绑定 ----

// BindDevice 通过绑定码绑定设备租户。
func (s *Service) BindDevice(ctx context.Context, userID int64, bindCode, bindName string) (*BindDO, error) {
	var tenant struct {
		ID       int64  `gorm:"column:id"`
		DeviceID string `gorm:"column:deviceId"`
	}
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_tenant").
		Select("id", "deviceId").
		Where("bindCode = ? AND tenantType = 'DEVICE' AND isDeleted = 0", bindCode).Scan(&tenant).Error
	if err != nil || tenant.ID == 0 {
		return nil, errs.New(errs.ResourceNotFound, "绑定码无效")
	}
	b := &BindDO{ID: ids.Next(), UserID: userID, DeviceID: tenant.DeviceID,
		TenantID: tenant.ID, BindName: bindName}
	if err := s.db.WithContext(ctx).Create(b).Error; err != nil {
		if isDup(err) {
			return nil, errs.New(errs.Conflict, "设备已被绑定（或已在你名下）")
		}
		return nil, errs.Wrap(errs.Internal, err)
	}
	return b, nil
}

// MyDevices 我的设备 + 各自用量/余额/套餐。
func (s *Service) MyDevices(ctx context.Context, userID int64) ([]map[string]any, error) {
	var binds []BindDO
	if err := s.db.WithContext(ctx).Where("userId = ?", userID).Find(&binds).Error; err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	out := make([]map[string]any, 0, len(binds))
	for _, b := range binds {
		item := map[string]any{
			"deviceId": b.DeviceID, "tenantId": b.TenantID, "bindName": b.BindName,
			"bindTime": b.CreateTime,
		}
		// 余额 + 用量 + 租户信息（billing 按租户查）
		bal, _ := s.billing.GetBalance(ctx, b.TenantID)
		ov, _ := s.billing.UsageOverview(ctx, b.TenantID, nil)
		item["balance"] = bal
		item["usage"] = ov
		var t struct {
			Name       string     `gorm:"column:name"`
			PlanID     int64      `gorm:"column:planId"`
			ExpireTime *time.Time `gorm:"column:expireTime"`
		}
		_ = s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
			Table("ykt_aisaas_tenant").
			Select("name", "planId", "expireTime").
			Where("id = ?", b.TenantID).Scan(&t)
		item["planId"] = t.PlanID
		item["planExpire"] = t.ExpireTime
		out = append(out, item)
	}
	return out, nil
}

// ---- 套餐订阅 ----

// Plans 套餐列表。
func (s *Service) Plans(ctx context.Context) ([]map[string]any, error) {
	var rows []map[string]any
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_plan").
		Where("status = 1 AND isDeleted = 0").Order("priceMonthly").
		Find(&rows).Error
	return rows, errs.Wrap(errs.Internal, err)
}

// Subscribe 订阅套餐：校验设备归属 → 写 subscription → 更新 tenant 套餐/到期
// → 按套餐 quotas 重设当月配额 → 刷 Redis 限额。
func (s *Service) Subscribe(ctx context.Context, userID, tenantID, planID int64) error {
	// 归属校验
	var cnt int64
	s.db.WithContext(ctx).Model(&BindDO{}).
		Where("userId = ? AND tenantId = ?", userID, tenantID).Count(&cnt)
	if cnt == 0 {
		return errs.New(errs.Forbidden, "设备不在你的名下")
	}

	var plan struct {
		Code   string `gorm:"column:code"`
		Name   string `gorm:"column:name"`
		Quotas string `gorm:"column:quotas"`
	}
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_plan").
		Select("code", "name", "quotas").
		Where("id = ? AND status = 1", planID).Scan(&plan).Error
	if err != nil || plan.Code == "" {
		return errs.New(errs.ResourceNotFound, "套餐不存在")
	}

	now := time.Now()
	end := now.AddDate(0, 1, 0)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO ykt_aisaas_subscription
			(id, tenantId, planId, periodStart, periodEnd, status)
			VALUES (?, ?, ?, ?, ?, 1)`,
			ids.Next(), tenantID, planID, now, end).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE ykt_aisaas_tenant SET planId = ?, expireTime = ? WHERE id = ?`,
			planID, end, tenantID).Error; err != nil {
			return err
		}
		// 套餐配额（覆盖式，当月）
		for dim, limit := range parseQuotaJSON(plan.Quotas) {
			if err := tx.Exec(`INSERT INTO ykt_aisaas_quota
				(id, tenantId, periodStart, periodEnd, dimension, limitValue)
				VALUES (?, ?, CURRENT_DATE, LAST_DAY(CURRENT_DATE) + INTERVAL 1 DAY, ?, ?)
				ON DUPLICATE KEY UPDATE limitValue = VALUES(limitValue)`,
				ids.Next(), tenantID, dim, limit).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- 充值 ----

// CreateOrder 创建充值订单（在线支付桩：返回订单号，支付渠道后续接）。
func (s *Service) CreateOrder(ctx context.Context, userID, tenantID, amountCents int64) (*OrderDO, error) {
	var cnt int64
	s.db.WithContext(ctx).Model(&BindDO{}).
		Where("userId = ? AND tenantId = ?", userID, tenantID).Count(&cnt)
	if cnt == 0 {
		return nil, errs.New(errs.Forbidden, "设备不在你的名下")
	}
	if amountCents < 100 || amountCents > 10_000_000 {
		return nil, errs.New(errs.InvalidParam, "充值金额 1 元 ~ 10 万元")
	}
	o := &OrderDO{
		ID: ids.Next(), OrderNo: fmt.Sprintf("RC%s%08d", time.Now().Format("20060102150405"), ids.Next()%100000000),
		TenantID: tenantID, UserID: userID, AmountCents: amountCents, Status: 0,
	}
	if err := s.db.WithContext(ctx).Create(o).Error; err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return o, nil
}

// PayOrderMock 支付回调（当前为模拟确认；接支付宝/微信后由回调替换）。
func (s *Service) PayOrderMock(ctx context.Context, userID, orderID int64) (*OrderDO, error) {
	var o OrderDO
	err := s.db.WithContext(ctx).Where("id = ? AND userId = ?", orderID, userID).Take(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "订单不存在")
	}
	if o.Status != 0 {
		return &o, nil // 幂等
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&OrderDO{}).
		Where("id = ? AND status = 0", orderID).
		Updates(map[string]any{"status": 1, "paidTime": now}).Error; err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	if _, err := s.billing.Recharge(ctx, o.TenantID, o.AmountCents, "order:"+o.OrderNo); err != nil {
		return nil, err
	}
	o.Status = 1
	o.PaidTime = &now
	return &o, nil
}

// MyOrders 我的充值订单。
func (s *Service) MyOrders(ctx context.Context, userID int64) ([]*OrderDO, error) {
	var out []*OrderDO
	err := s.db.WithContext(ctx).Where("userId = ?", userID).
		Order("id DESC").Limit(20).Find(&out).Error
	return out, errs.Wrap(errs.Internal, err)
}

func isDup(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) ||
		(len(err.Error()) > 0 && contains(err.Error(), "Duplicate entry")))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// parseQuotaJSON 与 tenantm 同款简易解析（避免跨业务包依赖）。
func parseQuotaJSON(s string) map[string]int64 {
	out := map[string]int64{}
	var key, num string
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
		case c == '"':
			inKey = !inKey
		case c >= '0' && c <= '9':
			num += string(c)
		case c == ',' || c == '}':
			flush()
		default:
			if inKey {
				key += string(c)
			}
		}
	}
	return out
}
