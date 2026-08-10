package service

import (
	"go.alis.build/adk/launchers/evals/evaluation/maps"
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// mergeSessionState returns run-level state overlaid by case-level state; case
// keys win. Returns nil when both inputs are empty.
func mergeSessionState(runLevel, caseLevel map[string]any) map[string]any {
	return maps.MergeStringAny(runLevel, caseLevel)
}

// effectiveSessionInput merges run-level session_state from run_eval into the
// eval case's sessionInput. When run-level state is absent the case input is
// returned unchanged.
func effectiveSessionInput(runLevel map[string]any, evalCase models.EvalCase) *models.SessionInput {
	if len(runLevel) == 0 {
		return evalCase.SessionInput
	}
	var caseState map[string]any
	if evalCase.SessionInput != nil {
		caseState = evalCase.SessionInput.State
	}
	merged := mergeSessionState(runLevel, caseState)
	if evalCase.SessionInput == nil {
		return &models.SessionInput{State: merged}
	}
	cp := *evalCase.SessionInput
	cp.State = merged
	return &cp
}
