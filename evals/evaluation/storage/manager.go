package storage

import (
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// EvalSetsManager persists eval sets and cases.
type EvalSetsManager interface {
	GetEvalSet(appName, evalSetID string) (*models.EvalSet, error)
	CreateEvalSet(appName, evalSetID string) (*models.EvalSet, error)
	// UpdateEvalSet persists top-level eval set metadata (name, description,
	// model/tool execution mode). Implementations should overwrite the stored
	// set with the provided value, preserving eval cases when the caller
	// intends metadata-only updates.
	UpdateEvalSet(appName string, set models.EvalSet) error
	ListEvalSets(appName string) ([]string, error)
	GetEvalCase(appName, evalSetID, evalCaseID string) (*models.EvalCase, error)
	AddEvalCase(appName, evalSetID string, evalCase models.EvalCase) error
	UpdateEvalCase(appName, evalSetID string, evalCase models.EvalCase) error
	DeleteEvalCase(appName, evalSetID, evalCaseID string) error
	DeleteEvalSet(appName, evalSetID string) error
}

// EvalSetResultsManager persists evaluation run results.
type EvalSetResultsManager interface {
	SaveEvalSetResult(appName, evalSetID string, caseResults []models.EvalCaseResult) (*models.EvalSetResult, error)
	GetEvalSetResult(appName, evalSetResultID string) (*models.EvalSetResult, error)
	ListEvalSetResults(appName string) ([]string, error)
}
