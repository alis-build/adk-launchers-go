package metrics

import (
	"context"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// TrajectoryEvaluator compares tool call trajectories between actual and expected invocations.
type TrajectoryEvaluator struct {
	threshold float64
	matchType models.ToolTrajectoryMatchType
}

// newTrajectoryEvaluator builds a tool_trajectory_avg_score evaluator from metric config.
func newTrajectoryEvaluator(metric models.EvalMetric) (Evaluator, error) {
	ev := &TrajectoryEvaluator{
		threshold: metric.Threshold,
		matchType: models.ToolTrajectoryMatchExact,
	}
	// A ToolTrajectory criterion, when present, overrides the top-level
	// threshold and supplies the match type. Fall back to metric.Threshold /
	// exact-match when no criterion is provided.
	if c, ok := metric.Criterion.AsToolTrajectory(); ok {
		if c.Threshold != 0 {
			ev.threshold = c.Threshold
		}
		ev.matchType = c.MatchType
	}
	return ev, nil
}

// EvaluateInvocations compares tool call trajectories per invocation and averages scores.
func (e *TrajectoryEvaluator) EvaluateInvocations(
	_ context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	_ *models.ConversationScenario,
) (EvaluationResult, error) {
	if len(expected) == 0 {
		return EvaluationResult{}, fmt.Errorf("expected invocations are required for %s", models.MetricToolTrajectoryAvgScore)
	}

	var per []PerInvocationResult
	var total float64
	for i := range actual {
		if i >= len(expected) {
			break
		}
		score := e.scorePair(actual[i], expected[i])
		total += score
		s := score
		per = append(per, PerInvocationResult{
			ActualInvocation:   actual[i],
			ExpectedInvocation: &expected[i],
			Score:              &s,
			EvalStatus:         statusForScore(score, e.threshold),
		})
	}
	if len(per) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	overall := total / float64(len(per))
	return EvaluationResult{
		OverallScore:         &overall,
		OverallEvalStatus:    statusForScore(overall, e.threshold),
		PerInvocationResults: per,
	}, nil
}

// scorePair returns 1 when tool trajectories match under the configured match
// type, otherwise 0.
func (e *TrajectoryEvaluator) scorePair(actual, expected models.Invocation) float64 {
	actualCalls, err := models.GetAllToolCalls(actual.IntermediateData)
	if err != nil {
		return 0
	}
	expectedCalls, err := models.GetAllToolCalls(expected.IntermediateData)
	if err != nil {
		return 0
	}
	if trajectoryMatch(actualCalls, expectedCalls, e.matchType) {
		return 1
	}
	return 0
}

// trajectoryMatch delegates to the match-type-specific comparison function.
func trajectoryMatch(actual, expected []*genai.FunctionCall, match models.ToolTrajectoryMatchType) bool {
	switch match {
	case models.ToolTrajectoryMatchInOrder:
		return toolCallsInOrder(actual, expected)
	case models.ToolTrajectoryMatchAnyOrder:
		return toolCallsAnyOrder(actual, expected)
	default:
		return toolCallsExact(actual, expected)
	}
}

// toolCallsExact requires identical tool name and args at each index.
func toolCallsExact(actual, expected []*genai.FunctionCall) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if !functionCallEqual(actual[i], expected[i]) {
			return false
		}
	}
	return true
}

// toolCallsInOrder requires expected calls to appear as an ordered subsequence
// within actual (extra calls between matches are allowed).
func toolCallsInOrder(actual, expected []*genai.FunctionCall) bool {
	if len(expected) == 0 {
		return true
	}
	if len(actual) == 0 {
		return false
	}
	j := 0
	for _, a := range actual {
		if j < len(expected) && functionCallEqual(a, expected[j]) {
			j++
		}
	}
	return j == len(expected)
}

// toolCallsAnyOrder requires each expected call to match some remaining actual
// call regardless of order.
func toolCallsAnyOrder(actual, expected []*genai.FunctionCall) bool {
	if len(expected) == 0 {
		return true
	}
	if len(actual) == 0 {
		return false
	}
	remaining := append([]*genai.FunctionCall(nil), actual...)
	for _, exp := range expected {
		found := -1
		for i, act := range remaining {
			if functionCallEqual(act, exp) {
				found = i
				break
			}
		}
		if found < 0 {
			return false
		}
		remaining = append(remaining[:found], remaining[found+1:]...)
	}
	return true
}

// functionCallEqual compares tool name and serialized args.
func functionCallEqual(a, b *genai.FunctionCall) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name && argsEqual(a.Args, b.Args)
}

// argsEqual compares argument maps using fmt.Sprint for value equality.
func argsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || fmt.Sprint(v) != fmt.Sprint(bv) {
			return false
		}
	}
	return true
}
