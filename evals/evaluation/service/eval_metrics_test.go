package service_test

import (
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
)

func metricNames(ms []models.EvalMetric) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.MetricName)
	}
	return out
}

func TestPrepareEvalMetricsAddsEfficiencyTheRegistryHas(t *testing.T) {
	requested := []models.EvalMetric{{MetricName: "response_match_score", Threshold: 0.5}}
	got, err := service.PrepareEvalMetrics(metrics.NewDefaultRegistry(), requested)
	if err != nil || strings.Join(metricNames(got), ",") != "response_match_score,tool_call_count_v1,inference_call_count_v1,token_usage_v1,invocation_duration_v1" {
		t.Errorf("default registry: %v, %v", metricNames(got), err)
	}
	// A registry without them adds none, so no metric is left NOT_EVALUATED.
	got, err = service.PrepareEvalMetrics(&metrics.Registry{}, requested)
	if err != nil || strings.Join(metricNames(got), ",") != "response_match_score" {
		t.Errorf("empty registry: %v, %v", metricNames(got), err)
	}
}

// adk-python refuses a threshold on an informational metric; so does the
// run, before anything executes.
func TestPrepareEvalMetricsRefusesEfficiencyThreshold(t *testing.T) {
	_, err := service.PrepareEvalMetrics(metrics.NewDefaultRegistry(), []models.EvalMetric{{MetricName: "tool_call_count_v1", Threshold: 0.5}})
	if err == nil || !strings.Contains(err.Error(), "tool_call_count_v1") {
		t.Errorf("error = %v, want one naming tool_call_count_v1", err)
	}
}
