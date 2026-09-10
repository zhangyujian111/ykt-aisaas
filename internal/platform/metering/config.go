package metering

// StreamConfig Redis Stream 相关配置（扩展 Metering config）。
type StreamConfig struct {
	// UseStream 是否使用 Redis Stream（false = 回退 channel 模式）
	UseStream bool

	// StreamMaxLen Stream 最大长度，默认 100000
	StreamMaxLen int64

	// ConsumerGroup consumer group 名称，默认 "aisaas-metering-consumers"
	ConsumerGroup string

	// DLQMaxRetries DLQ 最大重试次数，默认 3
	DLQMaxRetries int
}

// DefaultStreamConfig 返回默认 Stream 配置。
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		UseStream:     true,
		StreamMaxLen:  100000,
		ConsumerGroup: "aisaas-metering-consumers",
		DLQMaxRetries: 3,
	}
}