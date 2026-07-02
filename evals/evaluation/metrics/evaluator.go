package metrics

import (
	"context"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// PerInvocationResult is the score for one invocation.
type PerInvocationResult struct {
	ActualInvocation   models.Invocation
	ExpectedInvocation *models.Invocation
	Score              *float64
	EvalStatus         models.EvalStatus
	RubricScores       []models.RubricScore
}

// EvaluationResult aggregates metric scores for an eval case.
type EvaluationResult struct {
	OverallScore         *float64
	OverallEvalStatus    models.EvalStatus
	PerInvocationResults []PerInvocationResult
	OverallRubricScores  []models.RubricScore
}

// Evaluator scores actual invocations against expected golden data.
type Evaluator interface {
	EvaluateInvocations(
		ctx context.Context,
		actual []models.Invocation,
		expected []models.Invocation,
		scenario *models.ConversationScenario,
	) (EvaluationResult, error)
}

// statusForScore maps a numeric score to Passed or Failed using the metric threshold.
func statusForScore(score, threshold float64) models.EvalStatus {
	if score >= threshold {
		return models.EvalStatusPassed
	}
	return models.EvalStatusFailed
}

// avgScore returns the arithmetic mean of non-nil per-invocation scores, or nil
// when results is empty or no scores are present.
func avgScore(results []PerInvocationResult) *float64 {
	if len(results) == 0 {
		return nil
	}
	var total float64
	var scored int
	for _, r := range results {
		if r.Score != nil {
			total += *r.Score
			scored++
		}
	}
	if scored == 0 {
		return nil
	}
	v := total / float64(scored)
	return &v
}

// overallStatus derives case-level status from per-invocation results: any
// failure fails the case; otherwise Passed when the mean meets threshold.
func overallStatus(results []PerInvocationResult, threshold float64) models.EvalStatus {
	if len(results) == 0 {
		return models.EvalStatusNotEvaluated
	}
	score := avgScore(results)
	if score == nil {
		return models.EvalStatusNotEvaluated
	}
	return statusForScore(*score, threshold)
}
