// Package abtest A/B testing 实验平台。
package abtest

import "errors"

// 预定义错误。
var (
	ErrExperimentNotFound    = errors.New("experiment not found")
	ErrExperimentNotRunning  = errors.New("experiment is not running")
	ErrExperimentAlreadyEnded = errors.New("experiment already ended")
	ErrVariantNotFound       = errors.New("variant not found")
	ErrInvalidAllocation     = errors.New("variant allocation percentages must sum to 100")
	ErrNoControlVariant      = errors.New("experiment must have at least one control variant")
	ErrDuplicateAssignment   = errors.New("user already assigned to this experiment")
	ErrSampleSizeInsufficient = errors.New("sample size insufficient for statistical analysis")
)

// ExperimentStatus 实验状态。
type ExperimentStatus string

const (
	StatusDraft     ExperimentStatus = "draft"
	StatusRunning   ExperimentStatus = "running"
	StatusPaused    ExperimentStatus = "paused"
	StatusCompleted ExperimentStatus = "completed"
)

// ValidStatuses 所有合法实验状态。
var ValidStatuses = map[ExperimentStatus]bool{
	StatusDraft:     true,
	StatusRunning:   true,
	StatusPaused:    true,
	StatusCompleted: true,
}

// IsValidStatus 校验实验状态。
func IsValidStatus(s ExperimentStatus) bool {
	return ValidStatuses[s]
}

// ValidTransitions 状态转移规则。
var ValidTransitions = map[ExperimentStatus][]ExperimentStatus{
	StatusDraft:     {StatusRunning},
	StatusRunning:   {StatusPaused, StatusCompleted},
	StatusPaused:    {StatusRunning, StatusCompleted},
	StatusCompleted: {},
}

// CanTransition 校验状态转移是否合法。
func CanTransition(from, to ExperimentStatus) bool {
	targets, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}