package session

import (
	"context"
	"fmt"

	"ykt.dev/aisaas/internal/platform/redisx"
)

// DeductSnapshot 封装 redisx.QuotaSnapshotDeduct 调用，用于创建会话时的配额借记。
// 返回：(remaining, snapshot, error)
func DeductSnapshot(ctx context.Context, rdb *redisx.Client, tenantID int64, dim, sessionID string, amount int64) (int64, int64, error) {
	return rdb.QuotaSnapshotDeduct(ctx, tenantID, dim, sessionID, amount)
}

// RefundSnapshot 封装 redisx.QuotaSnapshotRefund 调用，用于结束会话时的配额退款。
func RefundSnapshot(ctx context.Context, rdb *redisx.Client, tenantID int64, dim, sessionID string, actualUsed int64) error {
	return rdb.QuotaSnapshotRefund(ctx, tenantID, dim, sessionID, actualUsed)
}

// GetQuotaRemaining 查询设备当前配额剩余。
func GetQuotaRemaining(ctx context.Context, rdb *redisx.Client, tenantID int64, dim string) (limit, used int64) {
	return rdb.QuotaRemaining(ctx, tenantID, dim)
}

// EstimateTokens 估算会话 token 用量（默认 5000）。
// 可根据 personaId、modelId 等参数调整预估。
func EstimateTokens(cfg Config, personaID *int64, modelID string) int64 {
	estimate := cfg.DefaultQuotaInitial
	if estimate <= 0 {
		estimate = 5000
	}
	// 后续可根据 persona/model 参数调整预估量
	_ = personaID
	_ = modelID
	return estimate
}

// ValidateQuotaInitial 校验配额初始值是否有效。
func ValidateQuotaInitial(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("quotaInitial must be positive, got %d", amount)
	}
	return nil
}

// ---- 维度映射 ----

// DimToLabel 将配额维度转为可读标签。
func DimToLabel(dim string) string {
	switch dim {
	case redisx.DimLLMTokensIn:
		return "输入 Token"
	case redisx.DimLLMTokensOut:
		return "输出 Token"
	case redisx.DimTTSChars:
		return "TTS 字符"
	case redisx.DimASRSeconds:
		return "ASR 秒数"
	default:
		return dim
	}
}