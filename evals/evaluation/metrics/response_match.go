package metrics

import (
	"context"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// ResponseMatchEvaluator scores final responses with ROUGE-1 (response_match_score).
type ResponseMatchEvaluator struct {
	metric models.EvalMetric
}

// newResponseMatchEvaluator builds a response_match_score ROUGE-1 evaluator.
func newResponseMatchEvaluator(metric models.EvalMetric) Evaluator {
	return &ResponseMatchEvaluator{metric: metric}
}

// EvaluateInvocations scores each actual final response against golden text using ROUGE-1 F-measure.
func (e *ResponseMatchEvaluator) EvaluateInvocations(
	_ context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	_ *models.ConversationScenario,
) (EvaluationResult, error) {
	if len(expected) == 0 {
		return EvaluationResult{}, fmt.Errorf("expected invocations are required for %s", models.MetricResponseMatchScore)
	}
	var per []PerInvocationResult
	var total float64
	for i := range actual {
		if i >= len(expected) {
			break
		}
		rouge := CalculateRouge1Scores(
			textFromContent(actual[i].FinalResponse),
			textFromContent(expected[i].FinalResponse),
		)
		score := rouge.FMeasure
		total += score
		s := score
		per = append(per, PerInvocationResult{
			ActualInvocation:   actual[i],
			ExpectedInvocation: &expected[i],
			Score:              &s,
			EvalStatus:         statusForScore(score, e.metric.Threshold),
		})
	}
	if len(per) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	overall := total / float64(len(per))
	return EvaluationResult{
		OverallScore:         &overall,
		OverallEvalStatus:    statusForScore(overall, e.metric.Threshold),
		PerInvocationResults: per,
	}, nil
}
