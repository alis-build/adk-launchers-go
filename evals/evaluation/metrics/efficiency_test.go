package metrics_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// effEval runs the efficiency metric name over actual.
func effEval(t *testing.T, name string, actual ...models.Invocation) metrics.EvaluationResult {
	t.Helper()
	ev, err := metrics.DefaultRegistry.GetEvaluator(models.EvalMetric{MetricName: name})
	if err != nil {
		t.Fatalf("GetEvaluator(%s): %v", name, err)
	}
	res, err := ev.EvaluateInvocations(context.Background(), actual, nil, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	return res
}

func approx(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

// withEvents is an invocation whose intermediate data holds events.
func withEvents(events ...models.InvocationEvent) models.Invocation {
	return models.Invocation{IntermediateData: models.InvocationEventsField(models.InvocationEvents{InvocationEvents: events})}
}

func usage(prompt, cached, toolUse, candidates, thoughts int32) models.InvocationEvent {
	return models.InvocationEvent{Author: "agent", UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: prompt, CachedContentTokenCount: cached, ToolUsePromptTokenCount: toolUse,
		CandidatesTokenCount: candidates, ThoughtsTokenCount: thoughts,
	}}
}

func toolCalls(names ...string) models.Invocation {
	var calls []*genai.FunctionCall
	for _, n := range names {
		calls = append(calls, &genai.FunctionCall{Name: n})
	}
	return models.Invocation{IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: calls})}
}

// Mirrors adk-python test_efficiency_evaluators.py:95.
func TestToolCallCountAveragesPerInvocation(t *testing.T) {
	res := effEval(t, models.MetricToolCallCountV1, toolCalls("a", "b"), toolCalls("a"))
	if !approx(res.OverallScore, 1.5) || res.OverallEvalStatus != models.EvalStatusInformational {
		t.Errorf("overall = %v, %d; want 1.5 informational", res.OverallScore, res.OverallEvalStatus)
	}
	for i, want := range []float64{2, 1} {
		r := res.PerInvocationResults[i]
		if !approx(r.Score, want) || r.EvalStatus != models.EvalStatusInformational {
			t.Errorf("invocation %d = %v, %d; want %v informational", i, r.Score, r.EvalStatus, want)
		}
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:162, :172.
func TestEfficiencyMetricRejectsThreshold(t *testing.T) {
	_, err := metrics.DefaultRegistry.GetEvaluator(models.EvalMetric{MetricName: models.MetricToolCallCountV1, Threshold: 1.0})
	if err == nil || !strings.Contains(err.Error(), "does not support a threshold") {
		t.Errorf("threshold error = %v", err)
	}
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"criteria":{"tool_call_count_v1":{"threshold":0.5}}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig: %v", err)
	}
	ms, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		t.Fatalf("GetEvalMetricsFromConfig: %v", err)
	}
	if _, err := metrics.DefaultRegistry.GetEvaluator(ms[0]); err == nil || !strings.Contains(err.Error(), "does not support a threshold") {
		t.Errorf("criterion error = %v", err)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:183.
func TestEfficiencyMetricWithNoInvocations(t *testing.T) {
	res := effEval(t, models.MetricToolCallCountV1)
	if res.OverallScore != nil || res.OverallEvalStatus != models.EvalStatusInformational || len(res.PerInvocationResults) != 0 {
		t.Errorf("result = %+v, want informational with no score", res)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:203, :226, :244.
func TestInferenceCallCountCountsModelEvents(t *testing.T) {
	if res := effEval(t, models.MetricInferenceCallCountV1, withEvents(usage(1, 0, 0, 1, 0), usage(2, 0, 0, 2, 0))); !approx(res.OverallScore, 2) {
		t.Errorf("two model events = %v, want 2", res.OverallScore)
	}
	if res := effEval(t, models.MetricInferenceCallCountV1, toolCalls("a")); res.OverallScore != nil || res.OverallEvalStatus != models.EvalStatusInformational {
		t.Errorf("no invocation events = %v, %d; want nil informational", res.OverallScore, res.OverallEvalStatus)
	}
	if res := effEval(t, models.MetricInferenceCallCountV1, withEvents()); !approx(res.OverallScore, 0) {
		t.Errorf("empty events = %v, want 0", res.OverallScore)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:266.
func TestTokenUsageSumsAcrossModelCalls(t *testing.T) {
	res := effEval(t, models.MetricTokenUsageV1, withEvents(usage(60, 0, 0, 40, 0), usage(200, 0, 0, 50, 0)))
	if !approx(res.OverallScore, 350) {
		t.Errorf("total = %v, want 350", res.OverallScore)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:282.
func TestTokenUsageTotalIsDerivedNotBackendTotal(t *testing.T) {
	ev := usage(100, 0, 0, 20, 5)
	ev.UsageMetadata.TotalTokenCount = 9999
	if res := effEval(t, models.MetricTokenUsageV1, withEvents(ev)); !approx(res.OverallScore, 125) {
		t.Errorf("total = %v, want 125", res.OverallScore)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:298.
func TestTokenUsageGroupsInputAndOutputParts(t *testing.T) {
	d := effEval(t, models.MetricTokenUsageV1, withEvents(usage(700, 100, 20, 200, 80))).OverallTokenUsageDetails
	if d == nil || !approx(d.InputTokens, 720) || !approx(d.OutputTokens, 280) || !approx(d.TotalTokens, 1000) {
		t.Errorf("details = %+v, want input 720, output 280, total 1000", d)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:510.
func TestTokenUsageAveragesUnreportedCountAsZero(t *testing.T) {
	d := effEval(t, models.MetricTokenUsageV1, withEvents(usage(50, 0, 0, 10, 40)), withEvents(usage(200, 0, 0, 100, 0))).OverallTokenUsageDetails
	if d == nil || !approx(d.TotalTokens, 200) || !approx(d.ReasoningTokens, 20) {
		t.Errorf("details = %+v, want total 200, reasoning 20", d)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:538. adk-python asserts
// cached 0.0 here; Go reads a zero count as not reported, so cached is nil.
func TestTokenUsageLeavesOutTurnWithNoUsage(t *testing.T) {
	versionOnly := models.InvocationEvent{Author: "agent", ModelVersion: "test-model"}
	res := effEval(t, models.MetricTokenUsageV1, withEvents(usage(900, 0, 0, 100, 40)), withEvents(versionOnly), withEvents(usage(0, 50, 0, 0, 0)))
	scores := res.PerInvocationResults
	if !approx(scores[0].Score, 1040) || scores[1].Score != nil || scores[2].Score != nil {
		t.Errorf("per-invocation = %v, %v, %v; want 1040, nil, nil", scores[0].Score, scores[1].Score, scores[2].Score)
	}
	d := res.OverallTokenUsageDetails
	if !approx(d.TotalTokens, 1040) || !approx(d.InputTokens, 900) || !approx(d.OutputTokens, 140) || !approx(d.ReasoningTokens, 40) || d.CachedTokens != nil {
		t.Errorf("overall = %+v, want 1040/900/140/40 and nil cached", d)
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:578.
func TestTokenUsageCountsToolUseTokens(t *testing.T) {
	d := effEval(t, models.MetricTokenUsageV1, withEvents(usage(470, 0, 30, 0, 0), usage(488, 0, 12, 0, 0))).OverallTokenUsageDetails
	if d == nil || !approx(d.ToolUseTokens, 42) || !approx(d.InputTokens, 1000) {
		t.Errorf("details = %+v, want tool-use 42, input 1000", d)
	}
}

func timed(total float64, model, tool *float64) models.Invocation {
	return models.Invocation{Duration: &total, ModelDuration: model, ToolDuration: tool}
}

func f64(v float64) *float64 { return &v }

// Mirrors adk-python test_efficiency_evaluators.py:606-842.
func TestDurationEvaluator(t *testing.T) {
	type want struct{ total, model, tool, framework *float64 }
	tests := []struct {
		name string
		in   []models.Invocation
		want want
	}{
		{"recorded time (:606)", []models.Invocation{timed(2.844, nil, nil)}, want{total: f64(2.844)}},
		{"averages invocations (:620)", []models.Invocation{timed(2, nil, nil), timed(4, nil, nil)}, want{total: f64(3)}},
		{"untimed (:635)", []models.Invocation{{}}, want{}},
		{"only timed (:655)", []models.Invocation{timed(3, nil, nil), {}}, want{total: f64(3)}},
		{"parts add up (:694)", []models.Invocation{timed(10, f64(6), f64(3))}, want{f64(10), f64(6), f64(3), f64(1)}},
		{"tool kept out of framework (:711)", []models.Invocation{timed(20, f64(1), f64(18))}, want{f64(20), f64(1), f64(18), f64(1)}},
		{"never negative (:725)", []models.Invocation{timed(2, f64(2.001), f64(0))}, want{f64(2), f64(2.001), f64(0), f64(0)}},
		{"unsplit turn is framework (:742)", []models.Invocation{timed(4, f64(0), f64(0))}, want{f64(4), f64(0), f64(0), f64(4)}},
		{"no parts no framework (:752)", []models.Invocation{timed(10, nil, nil)}, want{total: f64(10)}},
		{"needs both parts (:770)", []models.Invocation{timed(10, f64(6), nil)}, want{total: f64(10), model: f64(6)}},
		{"framework over split turns (:782)", []models.Invocation{timed(10, f64(6), f64(2)), timed(20, nil, nil)}, want{f64(15), f64(6), f64(2), f64(2)}},
		{"averages only timed (:814)", []models.Invocation{timed(10, f64(6), f64(2)), {}}, want{f64(10), f64(6), f64(2), f64(2)}},
		{"averages each part (:828)", []models.Invocation{timed(10, f64(8), f64(1)), timed(20, f64(10), f64(5))}, want{f64(15), f64(9), f64(3), f64(3)}},
	}
	same := func(got, want *float64) bool {
		if want == nil {
			return got == nil
		}
		return approx(got, *want)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := effEval(t, models.MetricInvocationDurationV1, tt.in...)
			d := res.OverallDurationDetails
			if d == nil {
				t.Fatal("no overall duration details")
			}
			if !same(d.TotalSeconds, tt.want.total) || !same(d.ModelSeconds, tt.want.model) || !same(d.ToolSeconds, tt.want.tool) || !same(d.FrameworkSeconds, tt.want.framework) {
				t.Errorf("details = %v/%v/%v/%v, want %v/%v/%v/%v", d.TotalSeconds, d.ModelSeconds, d.ToolSeconds, d.FrameworkSeconds, tt.want.total, tt.want.model, tt.want.tool, tt.want.framework)
			}
			if !same(res.OverallScore, tt.want.total) || res.OverallEvalStatus != models.EvalStatusInformational {
				t.Errorf("score = %v, %d; want %v informational", res.OverallScore, res.OverallEvalStatus, tt.want.total)
			}
		})
	}
}

// Mirrors adk-python test_efficiency_evaluators.py:842.
func TestDurationScoreStaysTotal(t *testing.T) {
	res := effEval(t, models.MetricInvocationDurationV1, timed(2.844, f64(2), f64(0.5)))
	if !approx(res.OverallScore, 2.844) || !approx(res.PerInvocationResults[0].Score, 2.844) {
		t.Errorf("scores = %v, %v; want 2.844", res.OverallScore, res.PerInvocationResults[0].Score)
	}
}

// Python's round(x, 3) rounds the stored value: the mean of 10.123 and
// 10.124 is 10.1235 in decimal but stored just below it, so it rounds down.
func TestDurationRoundsAsPython(t *testing.T) {
	res := effEval(t, models.MetricInvocationDurationV1, timed(10.123, nil, nil), timed(10.124, nil, nil))
	if res.OverallScore == nil || *res.OverallScore != 10.123 {
		t.Errorf("average = %v, want 10.123", res.OverallScore)
	}
}
