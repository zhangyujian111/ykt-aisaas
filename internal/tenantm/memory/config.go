package memory

// Config 记忆模块配置。
type Config struct {
	// ExtractThreshold 触发自动抽取的阈值：累计新消息数
	ExtractThreshold int `json:"extractThreshold"`
	// ExtractIntervalMinutes 触发自动抽取的间隔（分钟）
	ExtractIntervalMinutes int `json:"extractIntervalMinutes"`
	// StreamMaxLen Redis Stream 最大长度
	StreamMaxLen int64 `json:"streamMaxLen"`
	// ConsumerGroup 消费者组名
	ConsumerGroup string `json:"consumerGroup"`
	// WorkerCount 消费者数量
	WorkerCount int `json:"workerCount"`
	// DLQMaxRetries DLQ 最大重试次数
	DLQMaxRetries int `json:"dlqMaxRetries"`
	// ShortTermTTLDays 短期消息 TTL（天）
	ShortTermTTLDays int `json:"shortTermTTLDays"`
	// SummaryTTLDays 摘要 TTL（天）
	SummaryTTLDays int `json:"summaryTTLDays"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		ExtractThreshold:       10,
		ExtractIntervalMinutes: 30,
		StreamMaxLen:           10000,
		ConsumerGroup:          "memory-workers",
		WorkerCount:            2,
		DLQMaxRetries:          3,
		ShortTermTTLDays:       7,
		SummaryTTLDays:         90,
	}
}

// Stream names
const (
	StreamExtract       = "aisaas:memory:extract:tasks"
	StreamSummarize     = "aisaas:memory:summarize:tasks"
	StreamExtractDLQ   = "aisaas:memory:extract:dlq"
	StreamSummarizeDLQ = "aisaas:memory:summarize:dlq"
)