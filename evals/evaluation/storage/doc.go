// Package storage provides eval set and result persistence (local, in-memory, GCS).
//
// Local backends read and write whole eval set files per operation. That is
// appropriate for dev-scale eval sets; large production workloads should use
// [NewGCSManagers] or a custom [EvalSetsManager] implementation.
//
// [EvalSetsManager] and [EvalSetResultsManager] are the storage interfaces.
// Local backends write {agentsDir}/{app}/{id}.evalset.json and
// {agentsDir}/{app}/.adk/eval_history/*.evalset_result.json. [NewGCSManagers]
// configures a shared GCS bucket for both sets and results.
package storage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	evalSetFileExtension   = ".evalset.json"
	evalResultFileExtension = ".evalset_result.json"
	historyDirName         = ".adk/eval_history"
)

var evalSetIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ErrNotFound indicates a missing eval set, case, or result.
var ErrNotFound = errors.New("eval: not found")

// ValidatePathSegment rejects values that could alter a filesystem path.
func ValidatePathSegment(value, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", fieldName)
	}
	if strings.Contains(value, "\x00") {
		return fmt.Errorf("%s must not contain null bytes", fieldName)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s %q must not contain path separators", fieldName, value)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s %q must not contain traversal segments", fieldName, value)
	}
	return nil
}

// ValidateEvalSetID checks eval set ID format.
func ValidateEvalSetID(id string) error {
	if err := ValidatePathSegment(id, "Eval Set ID"); err != nil {
		return err
	}
	if !evalSetIDPattern.MatchString(id) {
		return fmt.Errorf("Eval Set ID %q must match %s", id, evalSetIDPattern.String())
	}
	return nil
}

// sanitizeResultName replaces path separators so result IDs are safe object keys.
func sanitizeResultName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
