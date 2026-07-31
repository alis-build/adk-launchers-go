package storage

import (
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// getEvalCaseFromSet finds a case by EvalID within an eval set.
func getEvalCaseFromSet(set *models.EvalSet, evalCaseID string) *models.EvalCase {
	for i := range set.EvalCases {
		if set.EvalCases[i].EvalID == evalCaseID {
			return &set.EvalCases[i]
		}
	}
	return nil
}

// addEvalCaseToSet appends a case after validating uniqueness and invariants.
func addEvalCaseToSet(set *models.EvalSet, evalCase models.EvalCase) error {
	for _, existing := range set.EvalCases {
		if existing.EvalID == evalCase.EvalID {
			return fmt.Errorf("Eval id %q already exists in %q eval set", evalCase.EvalID, set.EvalSetID)
		}
	}
	if err := evalCase.Validate(); err != nil {
		return err
	}
	set.EvalCases = append(set.EvalCases, evalCase)
	return nil
}

// updateEvalCaseInSet replaces an existing case by matching EvalID.
func updateEvalCaseInSet(set *models.EvalSet, updated models.EvalCase) error {
	if err := updated.Validate(); err != nil {
		return err
	}
	for i, existing := range set.EvalCases {
		if existing.EvalID == updated.EvalID {
			set.EvalCases[i] = updated
			return nil
		}
	}
	return fmt.Errorf("%w: eval case %q not found in eval set %q", ErrNotFound, updated.EvalID, set.EvalSetID)
}

// deleteEvalCaseFromSet removes a case by EvalID.
func deleteEvalCaseFromSet(set *models.EvalSet, evalCaseID string) error {
	for i, existing := range set.EvalCases {
		if existing.EvalID == evalCaseID {
			set.EvalCases = append(set.EvalCases[:i], set.EvalCases[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: eval case %q not found in eval set %q", ErrNotFound, evalCaseID, set.EvalSetID)
}

// createEvalSetResult builds a persisted result record with a stable integer timestamp id.
func createEvalSetResult(appName, evalSetID string, caseResults []models.EvalCaseResult) *models.EvalSetResult {
	ts := nowUnix()
	// Use a fixed-point integer timestamp so IDs never format as scientific
	// notation (e.g. "1.7e+09") from %v on large float64 values. ADK Python
	// keeps a decimal timestamp on disk; matching that avoids result IDs that
	// break URL paths or file globs.
	id := fmt.Sprintf("%s_%s_%d", appName, evalSetID, ts)
	name := sanitizeResultName(id)
	return &models.EvalSetResult{
		EvalSetResultID:   id,
		EvalSetResultName: &name,
		EvalSetID:         evalSetID,
		EvalCaseResults:   caseResults,
		CreationTimestamp: float64(ts),
	}
}

// StringSliceForJSON returns ids when non-nil, or an empty slice when nil, so JSON
// encoders emit [] instead of null for empty list results.
func StringSliceForJSON(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}
