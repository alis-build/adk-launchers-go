package agenteval_test

import (
	"context"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/agenteval"
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

func TestEvaluateEvalSetRequiresService(t *testing.T) {
	_, err := agenteval.EvaluateEvalSet(context.Background(), nil, "app", &models.EvalSet{EvalSetID: "set1"}, models.DefaultEvalConfig(), 1)
	if err == nil {
		t.Fatal("expected error for nil service")
	}
}
