package audit

import "time"

// Config 审计日志配置。
type Config struct {
	// StreamName Redis Stream 名称，默认 "aisaas:audit:stream"
	StreamName string

	// BatchSize 批量落库大小，默认 100
	BatchSize int

	// FlushInterval 落库刷新间隔，默认 3s
	FlushInterval time.Duration

	// StreamMaxLen Stream 最大长度，默认 100000
	StreamMaxLen int64

	// DLQMaxRetries DLQ 最大重试次数，默认 3
	DLQMaxRetries int

	// Region 当前区域标识，默认 "cn-beijing"
	Region string
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		StreamName:    "aisaas:audit:stream",
		BatchSize:     100,
		FlushInterval: 3 * time.Second,
		StreamMaxLen:  100000,
		DLQMaxRetries: 3,
		Region:        "cn-beijing",
	}
}