package metrics_test

import (
	"context"
	"encoding/json"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// fc is a function call named name with args.
func fc(name string, args map[string]any) *genai.FunctionCall {
	return &genai.FunctionCall{Name: name, Args: args}
}

// trajectoryScores scores actual against expected, one invocation per pair,
// with criterion decoded from JSON as a config would carry it.
func trajectoryScores(t *testing.T, criterion string, pairs ...[2][]*genai.FunctionCall) metrics.EvaluationResult {
	t.Helper()
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"criteria":{"tool_trajectory_avg_score":` + criterion + `}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig: %v", err)
	}
	ms, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		t.Fatalf("GetEvalMetricsFromConfig: %v", err)
	}
	ev, err := metrics.DefaultRegistry.GetEvaluator(ms[0])
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	var actual, expected []models.Invocation
	for _, p := range pairs {
		actual = append(actual, models.Invocation{IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: p[0]})})
		expected = append(expected, models.Invocation{IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: p[1]})})
	}
	res, err := ev.EvaluateInvocations(context.Background(), actual, expected, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	return res
}

func score(r metrics.EvaluationResult) float64 {
	if r.OverallScore == nil {
		return -1
	}
	return *r.OverallScore
}

// Mirrors adk-python test_trajectory_evaluator.py:664.
func TestIgnoreArgsComparesToolNamesOnly(t *testing.T) {
	a := map[string]any{"a": 1}
	b := map[string]any{"b": 2}
	x := map[string]any{"x": 99}
	y := map[string]any{"y": 100}
	tests := []struct {
		name, match      string
		actual, expected []*genai.FunctionCall
		want             float64
	}{
		{"exact different args", "EXACT", []*genai.FunctionCall{fc("t1", a), fc("t2", b)}, []*genai.FunctionCall{fc("t1", x), fc("t2", y)}, 1},
		{"exact different names", "EXACT", []*genai.FunctionCall{fc("t1", nil)}, []*genai.FunctionCall{fc("t2", nil)}, 0},
		{"exact different count", "EXACT", []*genai.FunctionCall{fc("t1", nil), fc("t2", nil)}, []*genai.FunctionCall{fc("t1", nil)}, 0},
		{"exact empty", "EXACT", nil, nil, 1},
		{"in-order different args with extra", "IN_ORDER", []*genai.FunctionCall{fc("t1", a), fc("extra", nil), fc("t2", b)}, []*genai.FunctionCall{fc("t1", x), fc("t2", y)}, 1},
		{"in-order wrong order", "IN_ORDER", []*genai.FunctionCall{fc("t2", nil), fc("t1", nil)}, []*genai.FunctionCall{fc("t1", nil), fc("t2", nil)}, 0},
		{"in-order missing", "IN_ORDER", []*genai.FunctionCall{fc("t1", nil)}, []*genai.FunctionCall{fc("t1", nil), fc("t2", nil)}, 0},
		{"any-order different args swapped", "ANY_ORDER", []*genai.FunctionCall{fc("t2", b), fc("t1", a)}, []*genai.FunctionCall{fc("t1", x), fc("t2", y)}, 1},
		{"any-order missing", "ANY_ORDER", []*genai.FunctionCall{fc("t1", nil)}, []*genai.FunctionCall{fc("t1", nil), fc("t2", nil)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crit, _ := json.Marshal(map[string]any{"threshold": 0.5, "matchType": tt.match, "ignoreArgs": true})
			if got := score(trajectoryScores(t, string(crit), [2][]*genai.FunctionCall{tt.actual, tt.expected})); got != tt.want {
				t.Errorf("score = %v, want %v", got, tt.want)
			}
		})
	}
}

// Mirrors adk-python test_trajectory_evaluator.py:679.
func TestIgnoreArgsFalseStillChecksArgs(t *testing.T) {
	got := score(trajectoryScores(t, `{"threshold":0.5,"matchType":"EXACT","ignoreArgs":false}`,
		[2][]*genai.FunctionCall{{fc("t1", map[string]any{"a": 1})}, {fc("t1", map[string]any{"a": 2})}}))
	if got != 0 {
		t.Errorf("score = %v, want 0", got)
	}
}

// Mirrors adk-python test_trajectory_evaluator.py:707.
func TestIgnoreArgsMixedInvocations(t *testing.T) {
	res := trajectoryScores(t, `{"threshold":0.5,"matchType":"EXACT","ignoreArgs":true}`,
		[2][]*genai.FunctionCall{{fc("t1", map[string]any{"a": 1})}, {fc("t1", map[string]any{"z": 99})}},
		[2][]*genai.FunctionCall{{fc("t1", nil)}, {fc("t2", nil)}})
	if score(res) != 0.5 {
		t.Errorf("overall = %v, want 0.5", score(res))
	}
	for i, want := range []float64{1, 0} {
		if s := res.PerInvocationResults[i].Score; s == nil || *s != want {
			t.Errorf("invocation %d = %v, want %v", i, s, want)
		}
	}
}

// Acceptance 3; mirrors adk-python test_trajectory_evaluator.py:742.
func TestIgnoreArgsCamelCaseCriterion(t *testing.T) {
	got := score(trajectoryScores(t, `{"threshold":0.5,"matchType":"EXACT","ignoreArgs":true}`,
		[2][]*genai.FunctionCall{{fc("t1", map[string]any{"a": 1})}, {fc("t1", map[string]any{"z": 999})}}))
	if got != 1 {
		t.Errorf("score = %v, want 1", got)
	}
}

// Without matchType the flag must still select the trajectory criterion.
func TestIgnoreArgsWithoutMatchType(t *testing.T) {
	for _, crit := range []string{`{"threshold":0.5,"ignoreArgs":true}`, `{"threshold":0.5,"ignore_args":true}`} {
		got := score(trajectoryScores(t, crit,
			[2][]*genai.FunctionCall{{fc("t1", map[string]any{"a": 1})}, {fc("t1", map[string]any{"z": 999})}}))
		if got != 1 {
			t.Errorf("%s score = %v, want 1", crit, got)
		}
	}
}

// adk-python accepts the snake_case field name too (populate_by_name); its
// test_trajectory_evaluator.py:77 uses "match_type".
func TestSnakeCaseMatchTypeCriterion(t *testing.T) {
	for _, crit := range []string{`{"threshold":0.5,"match_type":"ANY_ORDER"}`, `{"threshold":0.5,"match_type":"ANY_ORDER","ignore_args":true}`} {
		got := score(trajectoryScores(t, crit,
			[2][]*genai.FunctionCall{{fc("t1", nil), fc("t2", nil)}, {fc("t2", nil), fc("t1", nil)}}))
		if got != 1 {
			t.Errorf("%s score = %v, want 1", crit, got)
		}
	}
}

// When both spellings appear the camelCase alias wins, as in pydantic.
func TestIgnoreArgsAliasWinsOverFieldName(t *testing.T) {
	got := score(trajectoryScores(t, `{"threshold":0.5,"ignoreArgs":false,"ignore_args":true}`,
		[2][]*genai.FunctionCall{{fc("t1", map[string]any{"a": 1})}, {fc("t1", map[string]any{"z": 999})}}))
	if got != 0 {
		t.Errorf("score = %v, want 0 (arguments compared)", got)
	}
}
