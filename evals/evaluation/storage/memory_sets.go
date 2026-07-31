package storage

import (
	"fmt"
	"sort"
	"sync"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// InMemoryEvalSetsManager is a dict-backed EvalSetsManager for tests and CLI file mode.
type InMemoryEvalSetsManager struct {
	mu   sync.RWMutex
	sets map[string]map[string]*models.EvalSet // app -> evalSetID -> set
}

// NewInMemoryEvalSetsManager returns an in-memory EvalSetsManager for tests.
func NewInMemoryEvalSetsManager() *InMemoryEvalSetsManager {
	return &InMemoryEvalSetsManager{sets: make(map[string]map[string]*models.EvalSet)}
}

func (m *InMemoryEvalSetsManager) GetEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if appSets, ok := m.sets[appName]; ok {
		if set, ok := appSets[evalSetID]; ok {
			copy := *set
			return &copy, nil
		}
	}
	return nil, nil
}

func (m *InMemoryEvalSetsManager) CreateEvalSet(appName, evalSetID string) (*models.EvalSet, error) {
	if err := ValidateEvalSetID(evalSetID); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sets[appName]; !ok {
		m.sets[appName] = make(map[string]*models.EvalSet)
	}
	if _, exists := m.sets[appName][evalSetID]; exists {
		return nil, fmt.Errorf("EvalSet %s already exists for app %s", evalSetID, appName)
	}
	name := evalSetID
	set := &models.EvalSet{
		EvalSetID:         evalSetID,
		Name:              &name,
		EvalCases:         []models.EvalCase{},
		CreationTimestamp: float64(nowUnix()),
	}
	m.sets[appName][evalSetID] = set
	copy := *set
	return &copy, nil
}

func (m *InMemoryEvalSetsManager) UpdateEvalSet(appName string, set models.EvalSet) error {
	if err := ValidateEvalSetID(set.EvalSetID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	appSets, ok := m.sets[appName]
	if !ok {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, set.EvalSetID)
	}
	if _, ok := appSets[set.EvalSetID]; !ok {
		return fmt.Errorf("%w: eval set %q not found", ErrNotFound, set.EvalSetID)
	}
	copy := set
	appSets[set.EvalSetID] = &copy
	return nil
}

func (m *InMemoryEvalSetsManager) ListEvalSets(appName string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	appSets, ok := m.sets[appName]
	if !ok {
		return nil, fmt.Errorf("%w: eval directory for app %q not found", ErrNotFound, appName)
	}
	ids := make([]string, 0, len(appSets))
	for id := range appSets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (m *InMemoryEvalSetsManager) GetEvalCase(appName, evalSetID, evalCaseID string) (*models.EvalCase, error) {
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

func (m *InMemoryEvalSetsManager) AddEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	set, err := m.getLocked(appName, evalSetID)
	if err != nil {
		return err
	}
	if err := addEvalCaseToSet(set, evalCase); err != nil {
		return err
	}
	return nil
}

func (m *InMemoryEvalSetsManager) UpdateEvalCase(appName, evalSetID string, evalCase models.EvalCase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	set, err := m.getLocked(appName, evalSetID)
	if err != nil {
		return err
	}
	return updateEvalCaseInSet(set, evalCase)
}

func (m *InMemoryEvalSetsManager) DeleteEvalCase(appName, evalSetID, evalCaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	set, err := m.getLocked(appName, evalSetID)
	if err != nil {
		return err
	}
	return deleteEvalCaseFromSet(set, evalCaseID)
}

func (m *InMemoryEvalSetsManager) DeleteEvalSet(appName, evalSetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if appSets, ok := m.sets[appName]; ok {
		if _, exists := appSets[evalSetID]; exists {
			delete(appSets, evalSetID)
			return nil
		}
	}
	return fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
}

func (m *InMemoryEvalSetsManager) getLocked(appName, evalSetID string) (*models.EvalSet, error) {
	appSets, ok := m.sets[appName]
	if !ok {
		return nil, fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	set, ok := appSets[evalSetID]
	if !ok {
		return nil, fmt.Errorf("%w: eval set %q not found", ErrNotFound, evalSetID)
	}
	return set, nil
}

// LoadEvalSetIntoMemory loads a parsed eval set into the in-memory manager.
func (m *InMemoryEvalSetsManager) LoadEvalSetIntoMemory(appName string, set models.EvalSet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sets[appName]; !ok {
		m.sets[appName] = make(map[string]*models.EvalSet)
	}
	copy := set
	m.sets[appName][set.EvalSetID] = &copy
}
