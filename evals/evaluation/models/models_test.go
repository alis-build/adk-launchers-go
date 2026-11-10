package models_test

import (
	"encoding/json"
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

func TestEvalCaseCamelCaseJSON(t *testing.T) {
	raw := `{
		"evalId": "case_1",
		"conversation": [],
		"sessionInput": {
			"appName": "app",
			"userId": "user",
			"evalGroup": "retrieval"
		},
		"owner": "platform"
	}`
	var c models.EvalCase
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.EvalID != "case_1" {
		t.Fatalf("evalId = %q", c.EvalID)
	}
	if c.SessionInput == nil || c.SessionInput.AppName != "app" {
		t.Fatalf("sessionInput: %+v", c.SessionInput)
	}
	if c.SessionInput.Extra["evalGroup"] != "retrieval" {
		t.Fatalf("extra evalGroup = %v", c.SessionInput.Extra)
	}
	if c.Extra["owner"] != "platform" {
		t.Fatalf("extra owner = %v", c.Extra)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if m["owner"] != "platform" {
		t.Fatalf("marshaled owner = %v", m["owner"])
	}
}

func TestEvalCaseConversationXORScenario(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name: "conversation only",
			raw:  `{"evalId":"c1","conversation":[]}`,
		},
		{
			name: "scenario only",
			raw:  `{"evalId":"c1","conversationScenario":{"startingPrompt":"hi","conversationPlan":"plan"}}`,
		},
		{
			name:    "both set",
			raw:     `{"evalId":"c1","conversation":[],"conversationScenario":{"startingPrompt":"hi","conversationPlan":"plan"}}`,
			wantErr: true,
		},
		{
			name:    "neither set",
			raw:     `{"evalId":"c1"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c models.EvalCase
			err := json.Unmarshal([]byte(tt.raw), &c)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEvalStatusValues(t *testing.T) {
	if models.EvalStatusPassed != 1 || models.EvalStatusFailed != 2 || models.EvalStatusNotEvaluated != 3 {
		t.Fatalf("unexpected eval status constants")
	}
}

func TestGetAllToolCallsIntermediateData(t *testing.T) {
	inv := models.Invocation{
		UserContent: genai.NewContentFromText("q", genai.RoleUser),
		IntermediateData: models.IntermediateDataField(models.IntermediateData{
			ToolUses: []*genai.FunctionCall{
				{Name: "search", Args: map[string]any{"q": "weather"}},
			},
		}),
	}
	calls, err := models.GetAllToolCalls(inv.IntermediateData)
	if err != nil {
		t.Fatalf("GetAllToolCalls: %v", err)
	}
	if len(calls) != 1 || calls[0].Name != "search" {
		t.Fatalf("calls = %+v", calls)
	}
}

func intermediateDataObject(t *testing.T, inv models.Invocation) map[string]any {
	t.Helper()
	out, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := m["intermediateData"].(map[string]any)
	if !ok {
		t.Fatalf("intermediateData = %v (%T), want object", m["intermediateData"], m["intermediateData"])
	}
	return data
}

func TestIntermediateDataMarshalNeverNull(t *testing.T) {
	tests := []struct {
		name         string
		inv          models.Invocation
		wantToolUses int
	}{
		{
			name: "unset",
			inv:  models.Invocation{UserContent: genai.NewContentFromText("q", genai.RoleUser)},
		},
		{
			name: "empty intermediate data",
			inv: models.Invocation{
				UserContent:      genai.NewContentFromText("q", genai.RoleUser),
				IntermediateData: models.IntermediateDataField(models.IntermediateData{}),
			},
		},
		{
			name: "tool uses only",
			inv: models.Invocation{
				UserContent: genai.NewContentFromText("q", genai.RoleUser),
				IntermediateData: models.IntermediateDataField(models.IntermediateData{
					ToolUses: []*genai.FunctionCall{{Name: "search"}},
				}),
			},
			wantToolUses: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := intermediateDataObject(t, tt.inv)
			for _, key := range []string{"toolUses", "toolResponses", "intermediateResponses"} {
				arr, ok := data[key].([]any)
				if !ok {
					t.Fatalf("%s = %v (%T), want array", key, data[key], data[key])
				}
				if key == "toolUses" && len(arr) != tt.wantToolUses {
					t.Fatalf("len(toolUses) = %d, want %d", len(arr), tt.wantToolUses)
				}
			}
		})
	}
}

func TestIntermediateDataInvocationEventsMarshalMergesFlatArrays(t *testing.T) {
	tests := []struct {
		name              string
		events            models.InvocationEvents
		wantToolUses      int
		wantToolResponses int
	}{
		{
			name: "no tool calls",
			events: models.InvocationEvents{
				InvocationEvents: []models.InvocationEvent{{Author: "agent"}},
			},
		},
		{
			name:   "nil events",
			events: models.InvocationEvents{},
		},
		{
			name: "with tool calls and responses",
			events: models.InvocationEvents{
				InvocationEvents: []models.InvocationEvent{
					{
						Author: "agent",
						Content: &genai.Content{Parts: []*genai.Part{
							{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "search", Args: map[string]any{"q": "weather"}}},
						}},
					},
					{
						Author: "agent",
						Content: &genai.Content{Parts: []*genai.Part{
							{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "search"}},
						}},
					},
				},
			},
			wantToolUses:      1,
			wantToolResponses: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := models.Invocation{
				UserContent:      genai.NewContentFromText("q", genai.RoleUser),
				IntermediateData: models.InvocationEventsField(tt.events),
			}
			data := intermediateDataObject(t, inv)
			if _, ok := data["invocationEvents"].([]any); !ok {
				t.Fatalf("invocationEvents = %v (%T), want array", data["invocationEvents"], data["invocationEvents"])
			}
			for key, want := range map[string]int{
				"toolUses":              tt.wantToolUses,
				"toolResponses":         tt.wantToolResponses,
				"intermediateResponses": 0,
			} {
				arr, ok := data[key].([]any)
				if !ok {
					t.Fatalf("%s = %v (%T), want array", key, data[key], data[key])
				}
				if len(arr) != want {
					t.Fatalf("len(%s) = %d, want %d", key, len(arr), want)
				}
			}
		})
	}
}

func TestIntermediateDataInvocationEventsRoundTrip(t *testing.T) {
	inv := models.Invocation{
		UserContent: genai.NewContentFromText("q", genai.RoleUser),
		IntermediateData: models.InvocationEventsField(models.InvocationEvents{
			InvocationEvents: []models.InvocationEvent{
				{
					Author: "agent",
					Content: &genai.Content{Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "search"}},
					}},
				},
			},
		}),
	}
	out, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded models.Invocation
	if err = json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	events, ok := decoded.IntermediateData.AsInvocationEvents()
	if !ok {
		t.Fatalf("round-trip value = %T, want InvocationEvents", decoded.IntermediateData.Value())
	}
	if len(events.InvocationEvents) != 1 || events.InvocationEvents[0].Author != "agent" {
		t.Fatalf("events = %+v", events.InvocationEvents)
	}
	calls, err := models.GetAllToolCalls(decoded.IntermediateData)
	if err != nil {
		t.Fatalf("GetAllToolCalls: %v", err)
	}
	if len(calls) != 1 || calls[0].Name != "search" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestIntermediateDataNullRoundTripNormalizes(t *testing.T) {
	for name, raw := range map[string]string{
		"explicit null": `{"userContent":{"parts":[{"text":"q"}]},"intermediateData":null}`,
		"key absent":    `{"userContent":{"parts":[{"text":"q"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var inv models.Invocation
			if err := json.Unmarshal([]byte(raw), &inv); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			data := intermediateDataObject(t, inv)
			if _, ok := data["toolUses"].([]any); !ok {
				t.Fatalf("toolUses = %v, want array", data["toolUses"])
			}
			calls, err := models.GetAllToolCalls(inv.IntermediateData)
			if err != nil {
				t.Fatalf("GetAllToolCalls: %v", err)
			}
			if len(calls) != 0 {
				t.Fatalf("calls = %+v, want none", calls)
			}
		})
	}
}

func TestDefaultEvalConfigMetrics(t *testing.T) {
	cfg := models.DefaultEvalConfig()
	metrics, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		t.Fatalf("GetEvalMetricsFromConfig: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("len(metrics) = %d", len(metrics))
	}
}

func TestUnmarshalEvalConfigFloatThreshold(t *testing.T) {
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"criteria":{"response_match_score":0.7}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig: %v", err)
	}
	metrics, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		t.Fatalf("GetEvalMetricsFromConfig: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Threshold != 0.7 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func TestEffectiveThresholdPrefersCriterion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		metric models.EvalMetric
		want   float64
	}{
		{
			// adk-python test_rubric_based_evaluator.py: criterion 0.1 wins over top-level 0.9.
			name: "rubric criterion",
			metric: models.EvalMetric{Threshold: 0.9, Criterion: models.CriterionField(models.RubricsBasedCriterion{
				BaseCriterion: models.BaseCriterion{Threshold: 0.1},
			})},
			want: 0.1,
		},
		{
			// adk-python test_final_response_match_v1.py deprecated-threshold-only case.
			name:   "no criterion",
			metric: models.EvalMetric{Threshold: 0.8},
			want:   0.8,
		},
		{
			// Go-only: pins the user-simulator criterion case.
			name: "user simulator criterion",
			metric: models.EvalMetric{Threshold: 0.2, Criterion: models.CriterionField(models.LlmBackedUserSimulatorCriterion{
				LlmAsAJudgeCriterion: models.LlmAsAJudgeCriterion{BaseCriterion: models.BaseCriterion{Threshold: 0.6}},
				StopSignal:           "</finished>",
			})},
			want: 0.6,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := models.EffectiveThreshold(tc.metric); got != tc.want {
				t.Errorf("models.EffectiveThreshold() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A criterion with no threshold (0) would grade every score as passing; the
// top-level threshold applies instead. adk-python rejects such a request;
// Go keeps the earlier trajectory behaviour.
func TestEffectiveThresholdFallsBackWhenCriterionSetsNone(t *testing.T) {
	m := models.EvalMetric{Threshold: 0.7, Criterion: models.CriterionField(models.BaseCriterion{})}
	if got := models.EffectiveThreshold(m); got != 0.7 {
		t.Errorf("EffectiveThreshold() = %v, want the top-level 0.7", got)
	}
}

func TestInvocationEventUsageRoundTrip(t *testing.T) {
	in := `{"author":"agent","usageMetadata":{"promptTokenCount":7},"modelVersion":"m"}`
	var ev models.InvocationEvent
	if err := json.Unmarshal([]byte(in), &ev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ev.UsageMetadata == nil || ev.UsageMetadata.PromptTokenCount != 7 || ev.ModelVersion != "m" {
		t.Fatalf("decoded = %+v", ev)
	}
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"usageMetadata":{"promptTokenCount":7}`, `"modelVersion":"m"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("encoded %s, want it to contain %s", out, want)
		}
	}
}

// Mirrors adk-python test_eval_config.py:208.
func TestAppendDefaultEfficiencyMetricsAddsThemToAPlainList(t *testing.T) {
	got := models.AppendDefaultEfficiencyMetrics([]models.EvalMetric{{MetricName: models.MetricToolTrajectoryAvgScore, Threshold: 1}})
	var names []string
	for _, m := range got {
		names = append(names, m.MetricName)
	}
	want := []string{"tool_trajectory_avg_score", "tool_call_count_v1", "inference_call_count_v1", "token_usage_v1", "invocation_duration_v1"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, m := range got[1:] {
		if m.Threshold != 0 || m.Criterion.IsSet() {
			t.Errorf("%s threshold %v, criterion set %v; want none", m.MetricName, m.Threshold, m.Criterion.IsSet())
		}
	}
}

func TestAppendDefaultEfficiencyMetricsKeepsCallerEntry(t *testing.T) {
	got := models.AppendDefaultEfficiencyMetrics([]models.EvalMetric{{MetricName: models.MetricTokenUsageV1, Threshold: 1}})
	n := 0
	for _, m := range got {
		if m.MetricName == models.MetricTokenUsageV1 {
			n++
			if m.Threshold != 1 {
				t.Errorf("token_usage_v1 threshold = %v, want the caller's 1", m.Threshold)
			}
		}
	}
	if n != 1 {
		t.Errorf("token_usage_v1 appears %d times, want 1", n)
	}
}

func TestAppendDefaultEfficiencyMetricsDoesNotMutate(t *testing.T) {
	in := []models.EvalMetric{{MetricName: models.MetricToolTrajectoryAvgScore}}
	_ = models.AppendDefaultEfficiencyMetrics(in)
	if len(in) != 1 {
		t.Errorf("input length = %d, want 1", len(in))
	}
}

// Mirrors the three adk-python test_eval_config.py tests of e9e8f6156.
func TestUnmarshalEvalConfigParallelism(t *testing.T) {
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"parallelism":8}`))
	if err != nil || cfg.Parallelism == nil || *cfg.Parallelism != 8 {
		t.Errorf("parallelism 8 = %v, %v", cfg.Parallelism, err)
	}
	cfg, err = models.UnmarshalEvalConfig([]byte(`{}`))
	if err != nil || cfg.Parallelism != nil {
		t.Errorf("no parallelism = %v, %v; want nil", cfg.Parallelism, err)
	}
	for _, n := range []string{"0", "-1"} {
		if _, err := models.UnmarshalEvalConfig([]byte(`{"parallelism":` + n + `}`)); err == nil || !strings.Contains(err.Error(), "parallelism") {
			t.Errorf("parallelism %s error = %v, want one naming parallelism", n, err)
		}
	}
}

// Mirrors adk-python test_eval_config.py:390.
func TestUnmarshalEvalConfigSnakeUserSimulatorKey(t *testing.T) {
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"user_simulator_config":{"model":"legacy-model-snake"}}`))
	if err != nil || !strings.Contains(string(cfg.UserSimulatorConfig), "legacy-model-snake") {
		t.Errorf("user simulator config = %s, %v; want the snake_case key kept", cfg.UserSimulatorConfig, err)
	}
}

// Mirrors adk-python test_eval_case.py:190 and :202.
func TestSessionInputSessionIDRoundTrip(t *testing.T) {
	var in models.SessionInput
	if err := json.Unmarshal([]byte(`{"appName":"a","userId":"u","sessionId":"s1"}`), &in); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if in.SessionID != "s1" || in.Extra["sessionId"] != nil {
		t.Errorf("session input = %+v, want SessionID s1 and no Extra entry", in)
	}
	out, err := json.Marshal(in)
	if err != nil || !strings.Contains(string(out), `"sessionId":"s1"`) {
		t.Errorf("marshalled = %s, %v; want sessionId s1", out, err)
	}
	var none models.SessionInput
	if err := json.Unmarshal([]byte(`{"appName":"a","userId":"u"}`), &none); err != nil || none.SessionID != "" {
		t.Errorf("without sessionId: %+v, %v", none, err)
	}
	if out, _ := json.Marshal(none); strings.Contains(string(out), "sessionId") {
		t.Errorf("marshalled = %s, want no sessionId", out)
	}
}
