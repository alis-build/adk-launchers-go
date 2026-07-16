package evals

import (
	"encoding/json"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// UnmarshalJSON accepts both snake_case (adk-python DevServer) and camelCase
// (adk-web/JS) bodies. Snake_case wins for each field individually so mixed
// payloads (e.g. snake `eval_id` with camel `sessionId`) never drop fields.
func (r *AddSessionToEvalSetRequest) UnmarshalJSON(data []byte) error {
	type alias AddSessionToEvalSetRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	var camel struct {
		EvalID    string `json:"evalId"`
		SessionID string `json:"sessionId"`
		UserID    string `json:"userId"`
	}
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}
	if r.EvalID == "" {
		r.EvalID = camel.EvalID
	}
	if r.SessionID == "" {
		r.SessionID = camel.SessionID
	}
	if r.UserID == "" {
		r.UserID = camel.UserID
	}
	return nil
}

// UnmarshalJSON accepts both snake_case and camelCase run bodies and merges
// them per-field so mixed payloads keep every provided value.
func (r *RunEvalRequest) UnmarshalJSON(data []byte) error {
	type alias RunEvalRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	var camel struct {
		EvalIDs     []string            `json:"evalIds"`
		EvalCaseIDs []string            `json:"evalCaseIds"`
		EvalMetrics []models.EvalMetric `json:"evalMetrics"`
	}
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}
	if len(r.EvalIDs) == 0 {
		r.EvalIDs = camel.EvalIDs
	}
	if len(r.EvalCaseIDs) == 0 {
		r.EvalCaseIDs = camel.EvalCaseIDs
	}
	if len(r.EvalMetrics) == 0 {
		r.EvalMetrics = camel.EvalMetrics
	}
	return nil
}
