package agenteval

import (
	"math"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

const (
	p = models.EvalStatusPassed
	f = models.EvalStatusFailed
	n = models.EvalStatusNotEvaluated
)

func near(got *float64, want float64) bool {
	return got != nil && math.Abs(*got-want) < 1e-9
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:149-184.
func TestPassRateCountsDecidedRuns(t *testing.T) {
	if got := PassRate([]models.EvalStatus{p, f, p}); !near(got, 2.0/3) {
		t.Errorf("PassRate(P F P) = %v, want 2/3", got)
	}
	if got := PassRate([]models.EvalStatus{p, n, f}); !near(got, 0.5) {
		t.Errorf("PassRate(P N F) = %v, want 0.5", got)
	}
	for _, statuses := range [][]models.EvalStatus{nil, {n, n}} {
		if got := PassRate(statuses); got != nil {
			t.Errorf("PassRate(%v) = %v, want nil", statuses, *got)
		}
	}
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:223.
func TestPassKLeavesOutUndecidedRuns(t *testing.T) {
	statuses := []models.EvalStatus{p, n, p, f}
	at, err := PassAtK(statuses, 2)
	if err != nil || !near(at, 1.0) {
		t.Errorf("PassAtK = %v, %v; want 1.0", at, err)
	}
	hat, err := PassHatK(statuses, 2)
	if err != nil || !near(hat, 1.0/3) {
		t.Errorf("PassHatK = %v, %v; want 1/3", hat, err)
	}
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:237.
func TestPassKIsNilWithFewerThanKDecided(t *testing.T) {
	statuses := []models.EvalStatus{p, n}
	if got, err := PassAtK(statuses, 2); got != nil || err != nil {
		t.Errorf("PassAtK = %v, %v; want nil", got, err)
	}
	if got, err := PassHatK(statuses, 2); got != nil || err != nil {
		t.Errorf("PassHatK = %v, %v; want nil", got, err)
	}
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:215.
func TestPassKAtOneEqualsPassRate(t *testing.T) {
	statuses := []models.EvalStatus{p, p, p, f}
	at, _ := PassAtK(statuses, 1)
	hat, _ := PassHatK(statuses, 1)
	if !near(at, 0.75) || !near(hat, 0.75) {
		t.Errorf("pass@1, pass^1 = %v, %v; want 0.75 both", at, hat)
	}
}

// The subset enumeration of adk-python test_multi_run_aggregation_methods.py:196
// for n 4, c 2, the values its aggregator test (:1776) asserts.
func TestPassKOfTwoInFour(t *testing.T) {
	statuses := []models.EvalStatus{p, f, f, p}
	at, _ := PassAtK(statuses, 2)
	hat, _ := PassHatK(statuses, 2)
	if !near(at, 5.0/6) || !near(hat, 1.0/6) {
		t.Errorf("pass@2, pass^2 = %v, %v; want 5/6, 1/6", at, hat)
	}
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:272.
func TestPassKRejectsKBelowOne(t *testing.T) {
	for _, k := range []int{0, -1} {
		if _, err := PassAtK([]models.EvalStatus{p}, k); err == nil {
			t.Errorf("PassAtK(k=%d) error = nil, want one", k)
		}
		if _, err := PassHatK([]models.EvalStatus{p}, k); err == nil {
			t.Errorf("PassHatK(k=%d) error = nil, want one", k)
		}
	}
}

// Mirrors adk-python's get_run_status docstring and aggregator test :355.
func TestRunStatusLeavesCrashedRunUndecided(t *testing.T) {
	metric := func(s models.EvalStatus) models.EvalMetricResult {
		return models.EvalMetricResult{MetricName: "m", EvalStatus: s}
	}
	tests := []struct {
		name string
		run  models.EvalCaseResult
		want models.EvalStatus
	}{
		{"crashed", models.EvalCaseResult{FinalEvalStatus: f}, n},
		{"one metric not evaluated", models.EvalCaseResult{FinalEvalStatus: p, OverallEvalMetricResults: []models.EvalMetricResult{metric(p), metric(n)}}, n},
		{"one metric failed", models.EvalCaseResult{FinalEvalStatus: f, OverallEvalMetricResults: []models.EvalMetricResult{metric(f)}}, f},
		{"passed", models.EvalCaseResult{FinalEvalStatus: p, OverallEvalMetricResults: []models.EvalMetricResult{metric(p)}}, p},
		{"only informational", models.EvalCaseResult{FinalEvalStatus: n, OverallEvalMetricResults: []models.EvalMetricResult{metric(models.EvalStatusInformational)}}, n},
		// Mirrors adk-python test_eval_case_result_multi_run_aggregator.py:780-813.
		{"failure comes first", models.EvalCaseResult{FinalEvalStatus: f, OverallEvalMetricResults: []models.EvalMetricResult{metric(f), metric(n)}}, f},
		{"passed with informational", models.EvalCaseResult{FinalEvalStatus: p, OverallEvalMetricResults: []models.EvalMetricResult{metric(p), metric(models.EvalStatusInformational)}}, p},
		{"failed with only per-invocation results", models.EvalCaseResult{FinalEvalStatus: f, EvalMetricResultPerInvocation: []models.EvalMetricResultPerInvocation{{EvalMetricResults: []models.EvalMetricResult{metric(f)}}}}, f},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RunStatus(tt.run); got != tt.want {
				t.Errorf("RunStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}
