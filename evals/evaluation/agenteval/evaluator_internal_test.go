package agenteval

import (
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// TestMeanKeepsInformationalWithNotEvaluatedRun: a run that was not evaluated
// gives no verdict, so it must not turn an informational value into one.
func TestMeanKeepsInformationalWithNotEvaluatedRun(t *testing.T) {
	three := 3.0
	runs := []models.EvalCaseResult{
		{EvalID: "c", OverallEvalMetricResults: []models.EvalMetricResult{{MetricName: "tool_call_count_v1", Score: &three, EvalStatus: models.EvalStatusInformational}}},
		{EvalID: "c", OverallEvalMetricResults: []models.EvalMetricResult{{MetricName: "tool_call_count_v1", EvalStatus: models.EvalStatusNotEvaluated}}},
	}
	got := meanMetricResults(runs)
	if len(got) != 1 || got[0].Score == nil {
		t.Fatalf("meanMetricResults = %+v, want one scored metric", got)
	}
	if *got[0].Score != 3 || got[0].EvalStatus != models.EvalStatusInformational {
		t.Errorf("mean = %v, status %d; want 3, %d", *got[0].Score, got[0].EvalStatus, models.EvalStatusInformational)
	}
}
