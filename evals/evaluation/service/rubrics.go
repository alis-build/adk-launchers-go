package service

import (
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// copyEvalCaseRubricsToActual attaches case-level rubrics to every actual invocation.
func copyEvalCaseRubricsToActual(evalCase models.EvalCase, actual []models.Invocation) error {
	if len(evalCase.Rubrics) == 0 {
		return nil
	}
	for i := range actual {
		if err := addRubrics(&actual[i], evalCase.Rubrics); err != nil {
			return err
		}
	}
	return nil
}

// copyInvocationRubricsToActual copies per-turn rubrics from golden to actual invocations.
func copyInvocationRubricsToActual(expected, actual []models.Invocation) error {
	for i := range actual {
		if i >= len(expected) {
			break
		}
		if len(expected[i].Rubrics) == 0 {
			continue
		}
		if err := addRubrics(&actual[i], expected[i].Rubrics); err != nil {
			return err
		}
	}
	return nil
}

// addRubrics appends rubrics to inv, rejecting duplicate rubric_id values.
func addRubrics(inv *models.Invocation, rubrics []models.Rubric) error {
	existing := make(map[string]struct{}, len(inv.Rubrics))
	for _, r := range inv.Rubrics {
		existing[r.RubricID] = struct{}{}
	}
	for _, r := range rubrics {
		if _, ok := existing[r.RubricID]; ok {
			return fmt.Errorf("rubric with rubric_id %q already exists", r.RubricID)
		}
		inv.Rubrics = append(inv.Rubrics, r)
		existing[r.RubricID] = struct{}{}
	}
	return nil
}
