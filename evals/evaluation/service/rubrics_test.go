package service

import (
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

func TestCopyRubricsDuplicateID(t *testing.T) {
	actual := []models.Invocation{{}}
	expected := []models.Invocation{{Rubrics: []models.Rubric{{RubricID: "r1"}}}}
	if err := copyInvocationRubricsToActual(expected, actual); err != nil {
		t.Fatalf("first copy: %v", err)
	}
	if err := copyInvocationRubricsToActual(expected, actual); err == nil {
		t.Fatal("expected duplicate rubric error")
	}
}
