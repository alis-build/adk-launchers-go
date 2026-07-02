package evals

import (
	"encoding/json"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

func TestAddSessionToEvalSetRequestUnmarshalCamelCase(t *testing.T) {
	var req AddSessionToEvalSetRequest
	if err := json.Unmarshal([]byte(`{
		"evalId": "case1",
		"sessionId": "sess1",
		"userId": "user1"
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.EvalID != "case1" || req.SessionID != "sess1" || req.UserID != "user1" {
		t.Fatalf("req = %+v", req)
	}
}

func TestAddSessionToEvalSetRequestUnmarshalSnakeCase(t *testing.T) {
	var req AddSessionToEvalSetRequest
	if err := json.Unmarshal([]byte(`{
		"eval_id": "case1",
		"session_id": "sess1",
		"user_id": "user1"
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.EvalID != "case1" || req.SessionID != "sess1" || req.UserID != "user1" {
		t.Fatalf("req = %+v", req)
	}
}

func TestRunEvalRequestUnmarshalCamelCase(t *testing.T) {
	var req RunEvalRequest
	if err := json.Unmarshal([]byte(`{
		"evalIds": ["case1"],
		"evalMetrics": [{"metricName":"response_match_score","threshold":0.5}]
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.EvalIDs) != 1 || req.EvalIDs[0] != "case1" {
		t.Fatalf("eval ids = %v", req.EvalIDs)
	}
	if len(req.EvalMetrics) != 1 || req.EvalMetrics[0].MetricName != models.MetricResponseMatchScore {
		t.Fatalf("metrics = %+v", req.EvalMetrics)
	}
}

func TestRunEvalRequestUnmarshalSnakeCase(t *testing.T) {
	var req RunEvalRequest
	if err := json.Unmarshal([]byte(`{
		"eval_case_ids": ["case1"],
		"eval_metrics": [{"metricName":"response_match_score","threshold":0.5}]
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.EvalCaseIDs) != 1 || req.EvalCaseIDs[0] != "case1" {
		t.Fatalf("eval case ids = %v", req.EvalCaseIDs)
	}
}

func TestAddSessionToEvalSetRequestUnmarshalMixedCase(t *testing.T) {
	var req AddSessionToEvalSetRequest
	if err := json.Unmarshal([]byte(`{
		"eval_id": "case1",
		"sessionId": "sess1",
		"user_id": "user1"
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.EvalID != "case1" || req.SessionID != "sess1" || req.UserID != "user1" {
		t.Fatalf("mixed case dropped fields: %+v", req)
	}
}

func TestRunEvalRequestUnmarshalMixedCase(t *testing.T) {
	var req RunEvalRequest
	if err := json.Unmarshal([]byte(`{
		"eval_ids": ["case1"],
		"evalMetrics": [{"metricName":"response_match_score","threshold":0.5}]
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.EvalIDs) != 1 || req.EvalIDs[0] != "case1" {
		t.Fatalf("eval_ids dropped: %+v", req.EvalIDs)
	}
	if len(req.EvalMetrics) != 1 || req.EvalMetrics[0].MetricName != models.MetricResponseMatchScore {
		t.Fatalf("evalMetrics dropped: %+v", req.EvalMetrics)
	}
}

func TestCreateEvalSetRequestUnmarshalAdkWebBody(t *testing.T) {
	var req CreateEvalSetRequest
	if err := json.Unmarshal([]byte(`{
		"eval_set": {
			"eval_set_id": "eval_set_1",
			"model_execution_mode": "live",
			"tool_execution_mode": "live",
			"eval_cases": []
		}
	}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.EvalSet.EvalSetID != "eval_set_1" {
		t.Fatalf("eval_set_id = %q", req.EvalSet.EvalSetID)
	}
	if req.EvalSet.ModelExecutionMode == nil || *req.EvalSet.ModelExecutionMode != "live" {
		t.Fatalf("model_execution_mode = %v", req.EvalSet.ModelExecutionMode)
	}
}
