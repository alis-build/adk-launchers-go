package metrics

import (
	"context"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// VertexMetric identifies a Vertex Gen AI Eval prebuilt metric.
type VertexMetric string

const (
	vertexMetricCoherence                  VertexMetric = "coherence"
	vertexMetricSafety                     VertexMetric = "safety"
	vertexMetricMultiTurnTaskSuccess       VertexMetric = "multi_turn_task_success"
	vertexMetricMultiTurnTrajectoryQuality VertexMetric = "multi_turn_trajectory_quality"
	vertexMetricMultiTurnToolUseQuality    VertexMetric = "multi_turn_tool_use_quality"
)

// VertexEvalClient evaluates invocations using Vertex Gen AI Eval SDK semantics.
type VertexEvalClient interface {
	EvaluateSingleTurn(ctx context.Context, req VertexSingleTurnRequest) (float64, error)
	EvaluateMultiTurn(ctx context.Context, req VertexMultiTurnRequest) (float64, error)
}

// VertexSingleTurnRequest is input for single-turn Vertex metrics.
type VertexSingleTurnRequest struct {
	Metric          VertexMetric
	Actual          models.Invocation
	Expected        models.Invocation
	RequireExpected bool
}

// VertexMultiTurnRequest is input for multi-turn Vertex metrics.
type VertexMultiTurnRequest struct {
	Metric   VertexMetric
	Actual   []models.Invocation
	Expected []models.Invocation
	Scenario *models.ConversationScenario
}

type vertexSingleTurnEvaluator struct {
	metric          models.EvalMetric
	vertexMetric    VertexMetric
	requireExpected bool
	client          VertexEvalClient
}

// newVertexSingleTurnEvaluator builds a per-invocation Vertex Gen AI Eval scorer.
func newVertexSingleTurnEvaluator(metric models.EvalMetric, vm VertexMetric, requireExpected bool, cfg *RegistryConfig) Evaluator {
	var client VertexEvalClient
	if cfg != nil {
		client = cfg.VertexClient
	}
	return &vertexSingleTurnEvaluator{
		metric:          metric,
		vertexMetric:    vm,
		requireExpected: requireExpected,
		client:          client,
	}
}

// EvaluateInvocations calls Vertex Gen AI Eval once per invocation and averages scores.
func (e *vertexSingleTurnEvaluator) EvaluateInvocations(
	ctx context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	_ *models.ConversationScenario,
) (EvaluationResult, error) {
	if e.client == nil {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	if e.requireExpected && len(expected) == 0 {
		return EvaluationResult{}, errExpectedRequired(e.metric.MetricName)
	}

	var per []PerInvocationResult
	var total float64
	for i := range actual {
		var exp models.Invocation
		if i < len(expected) {
			exp = expected[i]
		}
		score, err := e.client.EvaluateSingleTurn(ctx, VertexSingleTurnRequest{
			Metric:          e.vertexMetric,
			Actual:          actual[i],
			Expected:        exp,
			RequireExpected: e.requireExpected,
		})
		if err != nil {
			return EvaluationResult{}, err
		}
		total += score
		s := score
		var expPtr *models.Invocation
		if i < len(expected) {
			expPtr = &expected[i]
		}
		per = append(per, PerInvocationResult{
			ActualInvocation:   actual[i],
			ExpectedInvocation: expPtr,
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

type vertexMultiTurnEvaluator struct {
	metric       models.EvalMetric
	vertexMetric VertexMetric
	client       VertexEvalClient
}

// newVertexMultiTurnEvaluator builds a conversation-level Vertex Gen AI Eval scorer.
func newVertexMultiTurnEvaluator(metric models.EvalMetric, vm VertexMetric, cfg *RegistryConfig) Evaluator {
	var client VertexEvalClient
	if cfg != nil {
		client = cfg.VertexClient
	}
	return &vertexMultiTurnEvaluator{metric: metric, vertexMetric: vm, client: client}
}

// EvaluateInvocations scores the full conversation with a multi-turn Vertex metric.
func (e *vertexMultiTurnEvaluator) EvaluateInvocations(
	ctx context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	scenario *models.ConversationScenario,
) (EvaluationResult, error) {
	if e.client == nil {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	score, err := e.client.EvaluateMultiTurn(ctx, VertexMultiTurnRequest{
		Metric:   e.vertexMetric,
		Actual:   actual,
		Expected: expected,
		Scenario: scenario,
	})
	if err != nil {
		return EvaluationResult{}, err
	}
	return EvaluationResult{
		OverallScore:      &score,
		OverallEvalStatus: statusForScore(score, e.metric.Threshold),
	}, nil
}

// errExpectedRequired reports that golden invocations are required for metric.
func errExpectedRequired(metric string) error {
	return &expectedRequiredError{metric: metric}
}

// expectedRequiredError indicates a metric was invoked without required golden invocations.
type expectedRequiredError struct {
	metric string
}

func (e *expectedRequiredError) Error() string {
	return "expected invocations are required for " + e.metric
}
