package storage_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
)

func TestLocalEvalSetsManagerCRUD(t *testing.T) {
	dir := t.TempDir()
	m := storage.NewLocalEvalSetsManager(dir)

	set, err := m.CreateEvalSet("my_app", "eval_set_1")
	if err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	if set.EvalSetID != "eval_set_1" {
		t.Fatalf("set id = %q", set.EvalSetID)
	}

	ids, err := m.ListEvalSets("my_app")
	if err != nil {
		t.Fatalf("ListEvalSets: %v", err)
	}
	if len(ids) != 1 || ids[0] != "eval_set_1" {
		t.Fatalf("ids = %v", ids)
	}

	case1 := models.EvalCase{
		EvalID:       "case_1",
		Conversation: []models.Invocation{},
	}
	if err := m.AddEvalCase("my_app", "eval_set_1", case1); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}

	got, err := m.GetEvalCase("my_app", "eval_set_1", "case_1")
	if err != nil || got == nil || got.EvalID != "case_1" {
		t.Fatalf("GetEvalCase = %+v, err = %v", got, err)
	}

	case1.Extra = map[string]any{"tag": "updated"}
	if err := m.UpdateEvalCase("my_app", "eval_set_1", case1); err != nil {
		t.Fatalf("UpdateEvalCase: %v", err)
	}

	if err := m.DeleteEvalCase("my_app", "eval_set_1", "case_1"); err != nil {
		t.Fatalf("DeleteEvalCase: %v", err)
	}

	path := filepath.Join(dir, "my_app", "eval_set_1.evalset.json")
	if _, err := m.GetEvalSet("my_app", "eval_set_1"); err != nil {
		t.Fatalf("GetEvalSet after delete case: %v", err)
	}
	if _, statErr := dirStat(path); statErr != nil {
		t.Fatalf("eval set file missing: %v", statErr)
	}
}

func dirStat(path string) (any, error) {
	return filepath.Glob(path)
}

func TestLocalEvalSetResultsManager(t *testing.T) {
	dir := t.TempDir()
	m := storage.NewLocalEvalSetResultsManager(dir)
	result, err := m.SaveEvalSetResult("app", "set1", []models.EvalCaseResult{
		{
			EvalSetID:                     "set1",
			EvalID:                        "c1",
			FinalEvalStatus:               models.EvalStatusPassed,
			OverallEvalMetricResults:      []models.EvalMetricResult{},
			EvalMetricResultPerInvocation: []models.EvalMetricResultPerInvocation{},
			SessionID:                     "sess",
		},
	})
	if err != nil {
		t.Fatalf("SaveEvalSetResult: %v", err)
	}

	ids, err := m.ListEvalSetResults("app")
	if err != nil || len(ids) != 1 {
		t.Fatalf("ListEvalSetResults = %v, err = %v", ids, err)
	}

	got, err := m.GetEvalSetResult("app", result.EvalSetResultID)
	if err != nil || got == nil || got.EvalSetID != "set1" {
		t.Fatalf("GetEvalSetResult = %+v, err = %v", got, err)
	}
}

func TestInMemoryEvalSetsManagerDuplicateCase(t *testing.T) {
	m := storage.NewInMemoryEvalSetsManager()
	if _, err := m.CreateEvalSet("app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	c := models.EvalCase{EvalID: "c1", Conversation: []models.Invocation{}}
	if err := m.AddEvalCase("app", "set1", c); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}
	err := m.AddEvalCase("app", "set1", c)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestValidatePathSegment(t *testing.T) {
	if err := storage.ValidatePathSegment("../bad", "id"); err == nil {
		t.Fatal("expected error for traversal")
	}
}

func TestGetEvalSetResultRejectsPathTraversal(t *testing.T) {
	m := storage.NewLocalEvalSetResultsManager(t.TempDir())
	for _, id := range []string{"../escape", "a/b", "..", "."} {
		if _, err := m.GetEvalSetResult("app", id); err == nil {
			t.Errorf("GetEvalSetResult(id=%q) expected error", id)
		}
	}
}

func TestLocalEvalSetsManagerUpdateEvalSet(t *testing.T) {
	dir := t.TempDir()
	m := storage.NewLocalEvalSetsManager(dir)
	if _, err := m.CreateEvalSet("app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	desc := "updated"
	mode := "live"
	if err := m.UpdateEvalSet("app", models.EvalSet{
		EvalSetID:          "set1",
		Description:        &desc,
		ModelExecutionMode: &mode,
		EvalCases:          []models.EvalCase{},
	}); err != nil {
		t.Fatalf("UpdateEvalSet: %v", err)
	}
	got, err := m.GetEvalSet("app", "set1")
	if err != nil || got == nil {
		t.Fatalf("GetEvalSet: %+v %v", got, err)
	}
	if got.Description == nil || *got.Description != desc {
		t.Fatalf("description = %v", got.Description)
	}
	if got.ModelExecutionMode == nil || *got.ModelExecutionMode != mode {
		t.Fatalf("model_execution_mode = %v", got.ModelExecutionMode)
	}
	if err := m.UpdateEvalSet("app", models.EvalSet{EvalSetID: "missing"}); err == nil {
		t.Fatal("expected error updating missing set")
	}
}

func TestListEvalSetsMissingApp(t *testing.T) {
	m := storage.NewLocalEvalSetsManager(t.TempDir())
	_, err := m.ListEvalSets("missing_app")
	if err == nil || !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestListEvalSetResultsEmptyReturnsNonNilSlice(t *testing.T) {
	dir := t.TempDir()
	m := storage.NewLocalEvalSetResultsManager(dir)

	ids, err := m.ListEvalSetResults("app")
	if err != nil {
		t.Fatalf("ListEvalSetResults: %v", err)
	}
	if ids == nil || len(ids) != 0 {
		t.Fatalf("ids = %v, want non-nil empty slice", ids)
	}

	historyDir := filepath.Join(dir, "app", ".adk", "eval_history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ids, err = m.ListEvalSetResults("app")
	if err != nil {
		t.Fatalf("ListEvalSetResults empty dir: %v", err)
	}
	if ids == nil || len(ids) != 0 {
		t.Fatalf("ids = %v, want non-nil empty slice", ids)
	}
}

func TestListEvalSetsEmptyAppDirReturnsNonNilSlice(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "my_app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	m := storage.NewLocalEvalSetsManager(dir)

	ids, err := m.ListEvalSets("my_app")
	if err != nil {
		t.Fatalf("ListEvalSets: %v", err)
	}
	if ids == nil || len(ids) != 0 {
		t.Fatalf("ids = %v, want non-nil empty slice", ids)
	}
}
