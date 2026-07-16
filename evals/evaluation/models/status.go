package models

// EvalStatus is the outcome of evaluating a metric or case (matches Python EvalStatus).
type EvalStatus int

const (
	// EvalStatusPassed means the metric or case met its threshold.
	EvalStatusPassed EvalStatus = 1
	// EvalStatusFailed means the metric or case did not meet its threshold.
	EvalStatusFailed EvalStatus = 2
	// EvalStatusNotEvaluated means scoring did not run or could not produce a score.
	EvalStatusNotEvaluated EvalStatus = 3
)
