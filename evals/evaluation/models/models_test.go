package models_test

import (
	"encoding/json"
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
