package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
)

// Acceptance 2: Go scores the captured inference exactly as adk-python did.
func TestEvaluateMatchesPythonEfficiencyFixture(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "efficiency")
	raw, err := os.ReadFile(filepath.Join(dir, "inference_input.json"))
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	var input struct {
		EvalCase        models.EvalCase `json:"evalCase"`
		InferenceResult struct {
			AppName         string              `json:"appName"`
			EvalSetID       string              `json:"evalSetId"`
			EvalCaseID      string              `json:"evalCaseId"`
			SessionID       string              `json:"sessionId"`
			Inferences      []models.Invocation `json:"inferences"`
			RepetitionIndex *int                `json:"repetitionIndex"`
		} `json:"inferenceResult"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	ir := input.InferenceResult
	sets := storage.NewInMemoryEvalSetsManager()
	if _, err := sets.CreateEvalSet(ir.AppName, ir.EvalSetID); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	if err := sets.AddEvalCase(ir.AppName, ir.EvalSetID, input.EvalCase); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}
	svc := &service.LocalEvalService{Sets: sets, Registry: metrics.DefaultRegistry}

	results, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{
			AppName: ir.AppName, EvalSetID: ir.EvalSetID, EvalCaseID: ir.EvalCaseID, SessionID: ir.SessionID,
			Inferences: ir.Inferences, RepetitionIndex: ir.RepetitionIndex, Status: service.InferenceStatusSuccess,
		}},
		EvaluateConfig: service.EvaluateConfig{EvalMetrics: models.AppendDefaultEfficiencyMetrics(nil)},
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("Evaluate = %+v, %v", results, err)
	}

	goJSON, err := json.Marshal(results[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pyJSON, err := os.ReadFile(filepath.Join(dir, "python_eval_case_result.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	type metric struct {
		MetricName string   `json:"metricName"`
		Score      *float64 `json:"score"`
		EvalStatus int      `json:"evalStatus"`
		Details    struct {
			TokenUsageDetails map[string]float64 `json:"tokenUsageDetails"`
			DurationDetails   map[string]float64 `json:"durationDetails"`
		} `json:"details"`
	}
	type view struct {
		RepetitionIndex               *int     `json:"repetitionIndex"`
		OverallEvalMetricResults      []metric `json:"overallEvalMetricResults"`
		EvalMetricResultPerInvocation []struct {
			EvalMetricResults []metric `json:"evalMetricResults"`
		} `json:"evalMetricResultPerInvocation"`
	}
	var got, want view
	if err := json.Unmarshal(goJSON, &got); err != nil {
		t.Fatalf("decode Go: %v", err)
	}
	if err := json.Unmarshal(pyJSON, &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Go result differs from adk-python's (-python +go):\n%s", diff)
	}
}
