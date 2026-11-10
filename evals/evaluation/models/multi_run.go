package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Cross-run methods (adk-python _multi_run_aggregation_methods.py).
const (
	CrossRunPassAtK  = "pass_at_k"
	CrossRunPassHatK = "pass_hat_k"
	CrossRunPassRate = "pass_rate"
)

// crossRunMethods lists the built-in methods, sorted, as adk-python's error
// names them.
var crossRunMethods = []string{CrossRunPassAtK, CrossRunPassHatK, CrossRunPassRate}

// CrossRunCriterion judges a case across its repeated runs: pass_rate over
// all decided runs, or pass@k / pass^k over k runs at a time. Without a
// threshold it only reports.
type CrossRunCriterion struct {
	Method    string   `json:"method"`
	Threshold *float64 `json:"threshold,omitempty"`
	K         *int     `json:"k,omitempty"`
}

// DisplayName is "pass@<k>", "pass^<k>" or the method name, as adk-python
// labels the criterion's result.
func (c CrossRunCriterion) DisplayName() string {
	if c.K != nil {
		switch c.Method {
		case CrossRunPassAtK:
			return fmt.Sprintf("pass@%d", *c.K)
		case CrossRunPassHatK:
			return fmt.Sprintf("pass^%d", *c.K)
		}
	}
	return c.Method
}

// MultiRunConfig is adk-python's multi_run_config, limited to num_runs and
// cross_run_criteria. Its other keys (cross_run_scoring,
// cross_run_scoring_by_metric, custom_methods, max_crashed_runs) are refused
// with an error naming the key rather than ignored.
type MultiRunConfig struct {
	// NumRuns is how many times each case runs; nil leaves it to the caller.
	NumRuns          *int                `json:"numRuns,omitempty"`
	CrossRunCriteria []CrossRunCriterion `json:"crossRunCriteria,omitempty"`
}

// unportedMultiRunKeys are adk-python multi_run_config keys Go refuses.
var unportedMultiRunKeys = []string{
	"crossRunScoring", "cross_run_scoring",
	"crossRunScoringByMetric", "cross_run_scoring_by_metric",
	"customMethods", "custom_methods",
	"maxCrashedRuns", "max_crashed_runs",
}

// UnmarshalJSON implements json.Unmarshaler. It accepts camelCase and
// snake_case keys, as adk-python's models do, reads a null value as unset,
// and rejects, in this order: a key Go does not port, any other unknown key
// (adk-python's extra="forbid"), num_runs below 1, then each invalid
// criterion.
func (c *MultiRunConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	keys := sortedKeys(raw)
	for _, k := range keys {
		if slices.Contains(unportedMultiRunKeys, k) {
			return fmt.Errorf("multi_run_config.%s is not supported", k)
		}
	}
	for _, k := range keys {
		switch k {
		case "numRuns", "num_runs", "crossRunCriteria", "cross_run_criteria":
		default:
			return fmt.Errorf("multi_run_config: unknown field %q", k)
		}
	}
	var out MultiRunConfig
	for _, k := range []string{"numRuns", "num_runs"} {
		v, ok := raw[k]
		if !ok || isNull(v) {
			continue
		}
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			return fmt.Errorf("multi_run_config.%s: %w", k, err)
		}
		out.NumRuns = &n
	}
	if out.NumRuns != nil && *out.NumRuns < 1 {
		return fmt.Errorf("multi_run_config.num_runs must be at least 1, got %d", *out.NumRuns)
	}
	for _, k := range []string{"crossRunCriteria", "cross_run_criteria"} {
		v, ok := raw[k]
		if !ok || isNull(v) {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err != nil {
			return fmt.Errorf("multi_run_config.%s: %w", k, err)
		}
		out.CrossRunCriteria = make([]CrossRunCriterion, 0, len(items))
		for i, item := range items {
			crit, err := decodeCrossRunCriterion(item)
			if err != nil {
				return fmt.Errorf("cross_run_criteria[%d]: %w", i, err)
			}
			out.CrossRunCriteria = append(out.CrossRunCriteria, crit)
		}
	}
	*c = out
	return nil
}

// isNull reports whether v is JSON null, which adk-python's Optional fields
// read as unset.
func isNull(v json.RawMessage) bool {
	return string(bytes.TrimSpace(v)) == "null"
}

// sortedKeys returns m's keys in order, so the first error is deterministic.
func sortedKeys(m map[string]json.RawMessage) []string {
	return slices.Sorted(maps.Keys(m))
}

// decodeCrossRunCriterion decodes and validates one cross-run criterion.
func decodeCrossRunCriterion(data []byte) (CrossRunCriterion, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return CrossRunCriterion{}, err
	}
	var c CrossRunCriterion
	if m, ok := raw["method"]; ok {
		if err := json.Unmarshal(m, &c.Method); err != nil {
			return CrossRunCriterion{}, fmt.Errorf("method: %w", err)
		}
	}
	if !slices.Contains(crossRunMethods, c.Method) {
		return CrossRunCriterion{}, fmt.Errorf("Unknown method: %q. Built-in methods: %v", c.Method, crossRunMethods)
	}
	for _, k := range sortedKeys(raw) {
		switch k {
		case "method":
		case "threshold", "k":
			if isNull(raw[k]) {
				continue
			}
		default:
			return CrossRunCriterion{}, fmt.Errorf("unknown field %q", k)
		}
		switch k {
		case "threshold":
			var t float64
			if err := json.Unmarshal(raw[k], &t); err != nil {
				return CrossRunCriterion{}, fmt.Errorf("threshold: %w", err)
			}
			c.Threshold = &t
		case "k":
			var n int
			if err := json.Unmarshal(raw[k], &n); err != nil {
				return CrossRunCriterion{}, fmt.Errorf("k: %w", err)
			}
			c.K = &n
		}
	}
	switch {
	case c.Method == CrossRunPassRate && c.K != nil:
		return CrossRunCriterion{}, errors.New("'pass_rate' does not take k")
	case c.Method == CrossRunPassRate && c.Threshold == nil:
		return CrossRunCriterion{}, errors.New("'pass_rate' needs a threshold")
	case c.Method != CrossRunPassRate && c.K == nil:
		return CrossRunCriterion{}, fmt.Errorf("%q needs k, the number of runs it looks at together", c.Method)
	case c.K != nil && *c.K < 1:
		return CrossRunCriterion{}, errors.New("k must be at least 1")
	case c.Threshold != nil && (*c.Threshold < 0 || *c.Threshold > 1):
		return CrossRunCriterion{}, errors.New("threshold must be between 0 and 1")
	}
	return c, nil
}
