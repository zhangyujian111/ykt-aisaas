// Package billing 限额加载 + 余额扣减 + 流水 + 用量查询。
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/redisx"
)

// quotaRow ykt_aisaas_quota 最小读集（跨租户表，SkipHooks 读）。
type quotaRow struct {
	TenantID   int64     `gorm:"column:tenantId"`
	Dimension  string    `gorm:"column:dimension"`
	LimitValue int64     `gorm:"column:limitValue"`
	PeriodEnd  time.Time `gorm:"column:periodEnd"`
}

// QuotaLoader 把 DB 限额刷到 Redis（:limit 键）。
// 启动全量 + 每小时增量；quota 行变更后即时调用 ReloadTenant。
type QuotaLoader struct {
	db  *gorm.DB
	rdb *redisx.Client
}

func NewQuotaLoader(db *gorm.DB, rdb *redisx.Client) *QuotaLoader {
	return &QuotaLoader{db: db, rdb: rdb}
}

// LoadAll 全量加载当期生效配额。
func (l *QuotaLoader) LoadAll(ctx context.Context) (int, error) {
	var rows []quotaRow
	err := l.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_quota").
		Select("tenantId", "dimension", "limitValue", "periodEnd").
		Where("periodStart <= CURRENT_DATE AND periodEnd > CURRENT_DATE").
		Find(&rows).Error
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := l.rdb.QuotaSetLimit(ctx, r.TenantID, r.Dimension, r.LimitValue); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// Start 启动每小时刷新。
func (l *QuotaLoader) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := l.LoadAll(ctx)
				if err != nil {
					slog.Error("quota reload", "err", err)
				} else {
					slog.Info("quota reloaded", "rows", n)
				}
			}
		}
	}()
}

// ---- 余额 ----

// Balance 余额账户。
type Balance struct {
	TenantID       int64 `json:"tenantId"`
	BalanceCents   int64 `json:"balanceCents"`
	TotalRecharged int64 `json:"totalRecharged"`
	TotalConsumed  int64 `json:"totalConsumed"`
}

type balanceRow struct {
	TenantID       int64 `gorm:"column:tenantId"`
	BalanceCents   int64 `gorm:"column:balanceCents"`
	TotalRecharged int64 `gorm:"column:totalRecharged"`
	TotalConsumed  int64 `gorm:"column:totalConsumed"`
	Version        int   `gorm:"column:version"`
}

type TxnDO struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	TenantID     int64     `gorm:"column:tenantId"`
	Type         string    `gorm:"column:type"`
	AmountCents  int64     `gorm:"column:amountCents"`
	BalanceAfter int64     `gorm:"column:balanceAfter"`
	BizType      string    `gorm:"column:bizType"`
	BizRefID     string    `gorm:"column:bizRefId"`
	Remark       string    `gorm:"column:remark"`
	CreateTime   time.Time `gorm:"column:createTime;autoCreateTime"`
}

func (TxnDO) TableName() string { return "ykt_aisaas_balance_transaction" }

// Service 计费服务。
type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// GetBalance 查余额（无账户返回 0）。
func (s *Service) GetBalance(ctx context.Context, tenantID int64) (*Balance, error) {
	var r balanceRow
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_balance").
		Select("tenantId", "balanceCents", "totalRecharged", "totalConsumed", "version").
		Where("tenantId = ?", tenantID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &Balance{TenantID: tenantID}, nil
	}
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return &Balance{
		TenantID: r.TenantID, BalanceCents: r.BalanceCents,
		TotalRecharged: r.TotalRecharged, TotalConsumed: r.TotalConsumed,
	}, nil
}

// Recharge 充值（管理端手动记账版）。
func (s *Service) Recharge(ctx context.Context, tenantID, amountCents int64, remark string) (*Balance, error) {
	if amountCents <= 0 {
		return nil, errs.New(errs.InvalidParam, "充值金额必须为正")
	}
	if err := s.ensureAccount(ctx, tenantID); err != nil {
		return nil, err
	}
	var after int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE ykt_aisaas_balance
			SET balanceCents = balanceCents + ?, totalRecharged = totalRecharged + ?
			WHERE tenantId = ?`, amountCents, amountCents, tenantID)
		if res.Error != nil {
			return res.Error
		}
		var b balanceRow
		if err := tx.Session(&gorm.Session{SkipHooks: true}).
			Table("ykt_aisaas_balance").
			Select("balanceCents").Where("tenantId = ?", tenantID).Scan(&b.BalanceCents).Error; err != nil {
			return err
		}
		after = b.BalanceCents
		return tx.Create(&TxnDO{
			ID: ids.Next(), TenantID: tenantID, Type: "recharge",
			AmountCents: amountCents, BalanceAfter: after, Remark: remark,
		}).Error
	})
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return s.GetBalance(ctx, tenantID)
}

// Consume 扣费（计量回调）。余额不足也照扣（可负），由配额/风控决定拒止；
// 扣费失败仅记日志（不阻塞业务），对账任务兜底。
func (s *Service) Consume(ctx context.Context, tenantID int64, bizType string, costCents int64, refID string) {
	if costCents == 0 {
		return
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE ykt_aisaas_balance
			SET balanceCents = balanceCents - ?, totalConsumed = totalConsumed + ?
			WHERE tenantId = ?`, costCents, costCents, tenantID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 { // 无账户：自动建（0 起）
			if err := tx.Exec(`INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId, balanceCents, totalConsumed)
				VALUES (?, ?, 0, 0)`, ids.Next(), tenantID).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE ykt_aisaas_balance
				SET balanceCents = balanceCents - ?, totalConsumed = totalConsumed + ?
				WHERE tenantId = ?`, costCents, costCents, tenantID).Error; err != nil {
				return err
			}
		}
		var b int64
		if err := tx.Session(&gorm.Session{SkipHooks: true}).
			Table("ykt_aisaas_balance").Select("balanceCents").
			Where("tenantId = ?", tenantID).Scan(&b).Error; err != nil {
			return err
		}
		return tx.Create(&TxnDO{
			ID: ids.Next(), TenantID: tenantID, Type: "consume",
			AmountCents: -costCents, BalanceAfter: b, BizType: bizType, BizRefID: refID,
		}).Error
	})
	if err != nil {
		slog.Error("consume failed", "tenantId", tenantID, "costCents", costCents, "err", err)
	}
}

// ListTxns 最近流水。
func (s *Service) ListTxns(ctx context.Context, tenantID int64, limit int) ([]*TxnDO, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out []*TxnDO
	err := s.db.WithContext(ctx).
		Where("tenantId = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	return out, nil
}

// UsageOverview 本月各维度用量 + 余量 + 余额。
func (s *Service) UsageOverview(ctx context.Context, tenantID int64, dims []string) (*Overview, error) {
	ym := time.Now().Format("2006-01")
	type usageRow struct {
		Dimension string `gorm:"column:dimension"`
		Total     int64  `gorm:"column:total"`
		Cost      int64  `gorm:"column:cost"`
	}
	var usage []usageRow
	err := s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table("ykt_aisaas_usage_detail").
		Select("dimension, SUM(amount) AS total, SUM(costCents) AS cost").
		Where("tenantId = ? AND createTime >= ?", tenantID, ym+"-01").
		Group("dimension").Find(&usage).Error
	if err != nil {
		return nil, errs.Wrap(errs.Internal, err)
	}
	bal, err := s.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ov := &Overview{Period: ym, Balance: bal, Dimensions: map[string]DimStat{}}
	for _, u := range usage {
		ov.Dimensions[u.Dimension] = DimStat{Used: u.Total, CostCents: u.Cost}
	}
	return ov, nil
}

// Overview 用量总览响应。
type Overview struct {
	Period     string             `json:"period"`
	Balance    *Balance           `json:"balance"`
	Dimensions map[string]DimStat `json:"dimensions"`
}

// DimStat 单维度统计。
type DimStat struct {
	Used      int64 `json:"used"`
	CostCents int64 `json:"costCents"`
}

func (s *Service) ensureAccount(ctx context.Context, tenantID int64) error {
	return s.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Exec(fmt.Sprintf("INSERT IGNORE INTO ykt_aisaas_balance (id, tenantId) VALUES (%d, %d)", ids.Next(), tenantID)).Error
}
