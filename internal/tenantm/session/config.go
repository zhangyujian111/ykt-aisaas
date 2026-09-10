package session

import "time"

// Config 会话模块配置。
type Config struct {
	// DefaultQuotaInitial 默认预估配额（token 数），默认 5000。
	DefaultQuotaInitial int64 `json:"defaultQuotaInitial"`

	// SessionTTL 会话超时时间，默认 24h。
	SessionTTL time.Duration `json:"sessionTTL"`

	// CleanupInterval 超时清理间隔，默认 1h。
	CleanupInterval time.Duration `json:"cleanupInterval"`

	// MaxHistoryLimit 历史查询最大返回条数，默认 100。
	MaxHistoryLimit int `json:"maxHistoryLimit"`
}

// DefaultConfig 返回生产默认配置。
func DefaultConfig() Config {
	return Config{
		DefaultQuotaInitial: 5000,
		SessionTTL:          24 * time.Hour,
		CleanupInterval:     time.Hour,
		MaxHistoryLimit:     100,
	}
}