package anomaly

import "time"

// BuiltinRules 内置规则集（4 类规则类型）。
// 这些规则在首次启动时通过 migration seed 写入数据库，
// 运行时由 engine.Start() 从 DB 加载到内存缓存。
var BuiltinRules = []Rule{
	{
		ID:          "builtin_quota_burst",
		Name:        "Quota Burst (1分钟内配额消耗 > 1000)",
		Description: "当 1 分钟内配额消耗超过 1000 时触发告警",
		RuleType:    QuotaBurst,
		Metric:      "quota_per_minute",
		Threshold:   1000,
		WindowSize:  1 * time.Minute,
		Severity:    SeverityWarn,
		Channels:    []string{"slack", "webhook"},
		Enabled:     true,
	},
	{
		ID:          "builtin_token_spike",
		Name:        "Token Spike (单次调用 token > 8000)",
		Description: "当单次 API 调用 token 数超过 8000 时触发告警",
		RuleType:    TokenSpike,
		Metric:      "tokens_per_call",
		Threshold:   8000,
		WindowSize:  5 * time.Minute,
		Severity:    SeverityWarn,
		Channels:    []string{"slack"},
		Enabled:     true,
	},
	{
		ID:          "builtin_error_rate",
		Name:        "Error Rate (5分钟内 5xx 错误率 > 5%)",
		Description: "当 5 分钟内 5xx 错误率超过 5% 时触发告警",
		RuleType:    ErrorRate,
		Metric:      "error_rate",
		Threshold:   0.05,
		WindowSize:  5 * time.Minute,
		Severity:    SeverityCritical,
		Channels:    []string{"slack", "email", "webhook"},
		Enabled:     true,
	},
	{
		ID:          "builtin_cost_anomaly",
		Name:        "Cost Anomaly (1小时成本 > ¥100)",
		Description: "当 1 小时内成本超过 ¥100 时触发告警",
		RuleType:    CostAnomaly,
		Metric:      "cost_per_hour",
		Threshold:   100,
		WindowSize:  1 * time.Hour,
		Severity:    SeverityCritical,
		Channels:    []string{"slack", "email"},
		Enabled:     true,
	},
}