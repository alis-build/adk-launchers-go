package service

import (
	"context"
	"encoding/json"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/session"
)

const defaultEvalUserID = "test_user_id"

// evalCaseUserID returns the session user id from the eval case or a default.
func evalCaseUserID(evalCase *models.EvalCase) string {
	if evalCase != nil && evalCase.SessionInput != nil && evalCase.SessionInput.UserID != "" {
		return evalCase.SessionInput.UserID
	}
	return defaultEvalUserID
}

// attachSessionMetadata fills UserID and SessionDetails on the eval case result.
func (s *LocalEvalService) attachSessionMetadata(
	ctx context.Context,
	inf InferenceResult,
	evalCase *models.EvalCase,
	result *models.EvalCaseResult,
) {
	if result == nil {
		return
	}
	userID := evalCaseUserID(evalCase)
	result.UserID = &userID
	if s == nil || s.Sessions == nil || inf.SessionID == "" {
		return
	}
	resp, err := s.Sessions.Get(ctx, &session.GetRequest{
		AppName:   inf.AppName,
		UserID:    userID,
		SessionID: inf.SessionID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return
	}
	raw, err := marshalSessionDetails(resp.Session)
	if err != nil {
		return
	}
	result.SessionDetails = raw
}

// marshalSessionDetails serializes session id, app, user, and state for API output.
func marshalSessionDetails(sess session.Session) (json.RawMessage, error) {
	if sess == nil {
		return nil, nil
	}
	state := make(map[string]any)
	for k, v := range sess.State().All() {
		state[k] = v
	}
	snap := map[string]any{
		"id":      sess.ID(),
		"appName": sess.AppName(),
		"userId":  sess.UserID(),
		"state":   state,
	}
	return json.Marshal(snap)
}
