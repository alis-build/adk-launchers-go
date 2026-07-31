package models_test

import (
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

const legacyEvalSetJSON = `[
  {
    "name": "roll_dice",
    "data": [
      {
        "query": "What can you do?",
        "expected_tool_use": [],
        "expected_intermediate_agent_responses": [],
        "reference": "I can roll dice."
      },
      {
        "query": "Roll a 17 sided dice twice",
        "expected_tool_use": [
          {"tool_name": "roll_die", "tool_input": {"sides": 17}},
          {"tool_name": "roll_die", "tool_input": {"sides": 17}}
        ],
        "expected_intermediate_agent_responses": [],
        "reference": "Rolled twice."
      }
    ],
    "initial_session": {
      "state": {},
      "app_name": "hello_world",
      "user_id": "user"
    }
  }
]`

func TestConvertLegacyEvalSetJSON(t *testing.T) {
	set, err := models.ConvertLegacyEvalSetJSON("my_set", []byte(legacyEvalSetJSON))
	if err != nil {
		t.Fatalf("ConvertLegacyEvalSetJSON: %v", err)
	}
	if set.EvalSetID != "my_set" {
		t.Fatalf("evalSetId = %q", set.EvalSetID)
	}
	if len(set.EvalCases) != 1 {
		t.Fatalf("len(cases) = %d", len(set.EvalCases))
	}
	c := set.EvalCases[0]
	if c.EvalID != "roll_dice" {
		t.Fatalf("evalId = %q", c.EvalID)
	}
	if len(c.Conversation) != 2 {
		t.Fatalf("len(conversation) = %d", len(c.Conversation))
	}
	if c.SessionInput == nil || c.SessionInput.AppName != "hello_world" {
		t.Fatalf("sessionInput = %+v", c.SessionInput)
	}
	calls, err := models.GetAllToolCalls(c.Conversation[1].IntermediateData)
	if err != nil {
		t.Fatalf("GetAllToolCalls: %v", err)
	}
	if len(calls) != 2 || calls[0].Name != "roll_die" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestParseEvalSetFileLegacyArray(t *testing.T) {
	set, err := models.ParseEvalSetFile("set1", []byte(legacyEvalSetJSON))
	if err != nil {
		t.Fatalf("ParseEvalSetFile: %v", err)
	}
	if set.EvalSetID != "set1" {
		t.Fatalf("evalSetId = %q", set.EvalSetID)
	}
}

func TestParseEvalSetFileDefaultsMissingEvalCases(t *testing.T) {
	set, err := models.ParseEvalSetFile("set_a", []byte(`{"eval_set_id":"set_a"}`))
	if err != nil {
		t.Fatalf("ParseEvalSetFile: %v", err)
	}
	if set.EvalCases == nil {
		t.Fatal("EvalCases = nil, want empty slice so it serializes as []")
	}
}

func TestParseEvalSetFileModern(t *testing.T) {
	raw := `{
		"eval_set_id": "modern",
		"eval_cases": [
			{"evalId": "c1", "conversation": []}
		]
	}`
	set, err := models.ParseEvalSetFile("ignored", []byte(raw))
	if err != nil {
		t.Fatalf("ParseEvalSetFile: %v", err)
	}
	if set.EvalSetID != "modern" || len(set.EvalCases) != 1 {
		t.Fatalf("set = %+v", set)
	}
}

func TestParseEvalSetFileModernCamelCase(t *testing.T) {
	raw := `{
		"evalSetId": "modern",
		"evalCases": [
			{"evalId": "c1", "conversation": []}
		]
	}`
	set, err := models.ParseEvalSetFile("ignored", []byte(raw))
	if err != nil {
		t.Fatalf("ParseEvalSetFile: %v", err)
	}
	if set.EvalSetID != "modern" || len(set.EvalCases) != 1 {
		t.Fatalf("set = %+v", set)
	}
}
