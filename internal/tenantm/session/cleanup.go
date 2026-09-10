package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ykt.dev/aisaas/internal/platform/audit"
)

// CleanupConfig 清理配置（从 Service.Config 读取）。
type CleanupConfig struct {
	SessionTTL      time.Duration // 24h
	CleanupInterval time.Duration // 1h
	BatchSize       int           // 100
}

// StartCleanupWorker 启动后台清理 worker。
//
// 装配位置（在 main.go 中）：
//
//	svc := session.NewService(...)
//	go svc.StartCleanupWorker(ctx)
func (s *Service) StartCleanupWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.cfg.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("session cleanup worker stopped")
				return
			case <-ticker.C:
				if err := s.cleanupStaleSessions(ctx); err != nil {
					slog.Error("session cleanup failed", "err", err)
				}
			}
		}
	}()
}

// cleanupStaleSessions 清理超时未结束的会话。
func (s *Service) cleanupStaleSessions(ctx context.Context) error {
	cutoff := time.Now().Add(-s.cfg.SessionTTL)

	// 1. 查询超时会话
	var staleSessions []SessionDO
	if err := s.repo.DB.WithContext(ctx).
		Where("status IN ? AND lastMessageTime < ?", []int{StatusCreating, StatusActive}, cutoff).
		Limit(100).
		Find(&staleSessions).Error; err != nil {
		return fmt.Errorf("query stale sessions: %w", err)
	}

	if len(staleSessions) == 0 {
		return nil
	}

	slog.Info("session cleanup found stale sessions", "count", len(staleSessions))

	// 2. 对每个会话调 QuotaSnapshotRefund
	for _, sess := range staleSessions {
		// 退 full quotaInitial（实际用量未知，按 0 处理，即全额退款）
		actualUsed := sess.QuotaUsed
		refunded := sess.QuotaInitial - actualUsed
		if refunded > 0 {
			sessionIDStr := formatSessionID(sess.ID)
			if err := s.rdb.QuotaSnapshotRefund(ctx, sess.TenantID, sess.QuotaDimension, sessionIDStr, actualUsed); err != nil {
				slog.Warn("stale session refund failed, will retry next cycle",
					"sessionID", sess.ID, "tenantID", sess.TenantID, "err", err)
				continue
			}
		}

		// 3. UPDATE status = StatusEnded
		now := time.Now()
		if err := s.repo.DB.WithContext(ctx).
			Model(&SessionDO{}).
			Where("id = ?", sess.ID).
			Updates(map[string]any{
				"status":      StatusEnded,
				"endTime":     now,
				"actualUsed":  actualUsed,
				"actualCost":  actualUsed,
			}).Error; err != nil {
			slog.Warn("stale session update failed", "sessionID", sess.ID, "err", err)
			continue
		}

		// 4. 写 audit_log
		sessionIDStr := formatSessionID(sess.ID)
		audit.Record(ctx, audit.AuditEntry{
			TenantID:     sess.TenantID,
			ActorType:    "system",
			Action:       "session.timeout_cleanup",
			ResourceType: "session",
			ResourceID:   sessionIDStr,
			Result:       "success",
			ActionDetail: map[string]any{
				"deviceId":      sess.DeviceID,
				"dimension":     sess.QuotaDimension,
				"quotaInitial":  sess.QuotaInitial,
				"actualUsed":    actualUsed,
				"quotaRefunded": refunded,
				"reason":        "timeout",
			},
		})
	}

	return nil
}
