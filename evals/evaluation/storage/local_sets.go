package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// LocalEvalSetsManager stores eval sets on disk under agentsDir/{app}/{id}.evalset.json.
type LocalEvalSetsManager struct {
	AgentsDir string
}

// NewLocalEvalSetsManager stores eval sets at agentsDir/{app}/{id}.evalset.json.
func NewLocalEvalSetsManager(agentsDir string) *LocalEvalSetsManager {
	return &LocalEvalSetsManager{AgentsDir: agentsDir}
}

// evalSetPath returns the filesystem path for one eval set file.
func (m *LocalEvalSetsManager) evalSetPath(appName, evalSetID string) (string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return "", err
	}
	if err := ValidateEvalSetID(evalSetID); err != nil {
		return "", err
	}
	return filepath.Join(m.AgentsDir, appName, evalSetID+evalSetFileExtension), nil
}

func (m *LocalEvalSetsManager) GetEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	path, err := m.evalSetPath(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set, err := models.ParseEvalSetFile(evalSetID, data)
	if err != nil {
		return nil, err
	}
	return &set, nil
}

func (m *LocalEvalSetsManager) CreateEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	path, err := m.evalSetPath(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("EvalSet %s already exists for app %s", evalSetID, appName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	now := float64(time.Now().Unix())
	name := evalSetID
	set := models.EvalSet{
		EvalSetID:         evalSetID,
		Name:              &name,
		EvalCases:         []models.EvalCase{},
		CreationTimestamp: now,
	}
	if err := m.writeEvalSet(path, set); err != nil {
		return nil, err
	}
	return &set, nil
}

func (m *LocalEvalSetsManager) UpdateEvalSet(appName string, set models.EvalSet) error {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return err
	}
	if err := ValidateEvalSetID(set.EvalSetID); err != nil {
		return err
	}
	path, err := m.evalSetPath(appName, set.EvalSetID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, set.EvalSetID)
	} else if err != nil {
		return err
	}
	return m.writeEvalSet(path, set)
}

func (m *LocalEvalSetsManager) ListEvalSets(appName string) ([]string, error) {
	if err := ValidatePathSegment(appName, "app_name"); err != nil {
		return nil, err
	}
	dir := filepath.Join(m.AgentsDir, appName)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: eval directory for app %q not found", ErrNotFound, appName)
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
		if strings.HasSuffix(name, evalSetFileExtension) {
			ids = append(ids, strings.TrimSuffix(name, evalSetFileExtension))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (m *LocalEvalSetsManager) GetEvalCase(appName, evalSetID, evalCaseID string) (*models.EvalCase, error) {
	set, err := m.GetEvalSet(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, nil
	}
	if c := getEvalCaseFromSet(set, evalCaseID); c != nil {
		return c, nil
	}
	return nil, nil
}

func (m *LocalEvalSetsManager) AddEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	set, err := m.requireEvalSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := addEvalCaseToSet(set, evalCase); err != nil {
		return err
	}
	return m.persist(appName, evalSetID, set)
}

func (m *LocalEvalSetsManager) UpdateEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	set, err := m.requireEvalSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := updateEvalCaseInSet(set, evalCase); err != nil {
		return err
	}
	return m.persist(appName, evalSetID, set)
}

func (m *LocalEvalSetsManager) DeleteEvalCase(appName, evalSetID, evalCaseID string) error {
	set, err := m.requireEvalSet(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := deleteEvalCaseFromSet(set, evalCaseID); err != nil {
		return err
	}
	return m.persist(appName, evalSetID, set)
}

func (m *LocalEvalSetsManager) DeleteEvalSet(appName, evalSetID string) error {
	path, err := m.evalSetPath(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	return err
}

// requireEvalSet loads an eval set or returns ErrNotFound.
func (m *LocalEvalSetsManager) requireEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	set, err := m.GetEvalSet(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	return set, nil
}

// persist writes the eval set JSON atomically to disk.
func (m *LocalEvalSetsManager) persist(appName, evalSetID string, set *models.EvalSet) error {
	path, err := m.evalSetPath(appName, evalSetID)
	if err != nil {
		return err
	}
	return m.writeEvalSet(path, *set)
}

// writeEvalSet marshals and writes one eval set file.
func (m *LocalEvalSetsManager) writeEvalSet(path string, set models.EvalSet) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
