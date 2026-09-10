// Package anomaly AI 配额异常检测规则引擎。
package anomaly

import "errors"

// 预定义错误。
var (
	ErrRuleNotFound     = errors.New("anomaly rule not found")
	ErrRuleIDRequired   = errors.New("rule id is required")
	ErrInvalidRuleType  = errors.New("invalid rule type")
	ErrInvalidSeverity  = errors.New("invalid severity level")
	ErrEngineNotStarted = errors.New("anomaly engine not started")
	ErrRuleAlreadyExists = errors.New("rule already exists")
)

// RuleType 规则类型。
type RuleType string

const (
	QuotaBurst  RuleType = "quota_burst"
	TokenSpike  RuleType = "token_spike"
	ErrorRate   RuleType = "error_rate"
	CostAnomaly RuleType = "cost_anomaly"
)

// ValidRuleTypes 所有合法规则类型。
var ValidRuleTypes = map[RuleType]bool{
	QuotaBurst:  true,
	TokenSpike:  true,
	ErrorRate:   true,
	CostAnomaly: true,
}

// IsValidRuleType 校验规则类型。
func IsValidRuleType(rt RuleType) bool {
	return ValidRuleTypes[rt]
}

// Severity 严重度。
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// ValidSeverities 所有合法严重度。
var ValidSeverities = map[Severity]bool{
	SeverityInfo:     true,
	SeverityWarn:     true,
	SeverityCritical: true,
}

// IsValidSeverity 校验严重度。
func IsValidSeverity(s Severity) bool {
	return ValidSeverities[s]
}