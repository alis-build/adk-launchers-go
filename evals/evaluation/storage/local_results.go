package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// LocalEvalSetResultsManager stores results under agentsDir/{app}/.adk/eval_history/.
type LocalEvalSetResultsManager struct {
	AgentsDir string
}

// NewLocalEvalSetResultsManager stores results under agentsDir/{app}/.adk/eval_history/.
func NewLocalEvalSetResultsManager(agentsDir string) *LocalEvalSetResultsManager {
	return &LocalEvalSetResultsManager{AgentsDir: agentsDir}
}

// historyDir returns the per-app eval history directory path.
func (m *LocalEvalSetResultsManager) historyDir(appName string) (string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return "", err
	}
	return filepath.Join(m.AgentsDir, appName, historyDirName), nil
}

func (m *LocalEvalSetResultsManager) SaveEvalSetResult(appName, evalSetID string, caseResults []models.EvalCaseResult) (*models.EvalSetResult, error) {
	if err := ValidateEvalSetID(evalSetID); err != nil {
		return nil, err
	}
	result := createEvalSetResult(appName, evalSetID, caseResults)
	dir, err := m.historyDir(appName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := *result.EvalSetResultName
	path := filepath.Join(dir, name+evalResultFileExtension)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *LocalEvalSetResultsManager) GetEvalSetResult(appName, evalSetResultID string) (*models.EvalSetResult, error) {
	if err := ValidatePathSegment(evalSetResultID, "eval result id"); err != nil {
		return nil, err
	}
	dir, err := m.historyDir(appName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, evalSetResultID+evalResultFileExtension)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: eval result %q not found", ErrNotFound, evalSetResultID)
	}
	if err != nil {
		return nil, err
	}
	return parseEvalSetResultJSON(data)
}

func (m *LocalEvalSetResultsManager) ListEvalSetResults(appName string) ([]string, error) {
	dir, err := m.historyDir(appName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, evalResultFileExtension) {
			ids = append(ids, strings.TrimSuffix(name, evalResultFileExtension))
		}
	}
	sort.Strings(ids)
	return StringSliceForJSON(ids), nil
}

// parseEvalSetResultJSON unmarshals a persisted eval set result file.
func parseEvalSetResultJSON(data []byte) (*models.EvalSetResult, error) {
	var result models.EvalSetResult
	err := json.Unmarshal(data, &result)
	if err == nil && result.EvalSetResultID != "" {
		return &result, nil
	}
	// Some legacy writers stored the JSON as a JSON-encoded string; unwrap once.
	var encoded string
	if err2 := json.Unmarshal(data, &encoded); err2 == nil {
		return parseEvalSetResultJSON([]byte(encoded))
	}
	if err == nil {
		return nil, fmt.Errorf("parse eval set result: missing eval_set_result_id")
	}
	return nil, fmt.Errorf("parse eval set result: %w", err)
}
