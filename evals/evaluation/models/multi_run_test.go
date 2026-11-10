package models

import (
	"strings"
	"testing"
)

// Mirrors adk-python test_eval_config.py:498.
func TestMultiRunConfigReadsNumRunsInEitherSpelling(t *testing.T) {
	for _, body := range []string{
		`{"multi_run_config":{"num_runs":3}}`,
		`{"multiRunConfig":{"numRuns":3}}`,
	} {
		cfg, err := UnmarshalEvalConfig([]byte(body))
		if err != nil {
			t.Fatalf("UnmarshalEvalConfig(%s) error = %v", body, err)
		}
		if cfg.MultiRunConfig == nil || cfg.MultiRunConfig.NumRuns == nil || *cfg.MultiRunConfig.NumRuns != 3 {
			t.Errorf("UnmarshalEvalConfig(%s) MultiRunConfig = %+v, want NumRuns 3", body, cfg.MultiRunConfig)
		}
	}
}

// Mirrors adk-python test_eval_config.py:509.
func TestMultiRunConfigRejectsNumRunsBelowOne(t *testing.T) {
	_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"num_runs":0}}`))
	if err == nil || !strings.Contains(err.Error(), "num_runs") {
		t.Errorf("error = %v, want one naming num_runs", err)
	}
}

// Mirrors adk-python test_eval_config.py:682.
func TestMultiRunConfigReadsPassKCriteria(t *testing.T) {
	cfg, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"num_runs":3,"cross_run_criteria":[
		{"method":"pass_hat_k","k":3},
		{"method":"pass_at_k","k":2,"threshold":0.9},
		{"method":"pass_rate","threshold":0.5}]}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig() error = %v", err)
	}
	got := cfg.MultiRunConfig.CrossRunCriteria
	if len(got) != 3 {
		t.Fatalf("criteria = %+v, want 3", got)
	}
	if got[0].Method != "pass_hat_k" || got[0].K == nil || *got[0].K != 3 || got[0].Threshold != nil {
		t.Errorf("criterion 0 = %+v, want pass_hat_k k 3 without threshold", got[0])
	}
	if got[1].Method != "pass_at_k" || got[1].K == nil || *got[1].K != 2 || got[1].Threshold == nil || *got[1].Threshold != 0.9 {
		t.Errorf("criterion 1 = %+v, want pass_at_k k 2 threshold 0.9", got[1])
	}
	if got[2].Method != "pass_rate" || got[2].K != nil || got[2].Threshold == nil || *got[2].Threshold != 0.5 {
		t.Errorf("criterion 2 = %+v, want pass_rate threshold 0.5", got[2])
	}
}

// Mirrors adk-python test_eval_config.py:707: k above num_runs is checked
// when the eval starts, not here.
func TestMultiRunConfigKeepsKAboveNumRuns(t *testing.T) {
	_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"num_runs":2,"cross_run_criteria":[{"method":"pass_hat_k","k":4}]}}`))
	if err != nil {
		t.Errorf("UnmarshalEvalConfig() error = %v, want nil", err)
	}
}

// Mirrors adk-python test_eval_config.py:744.
func TestMultiRunConfigRejectsInvalidCriterion(t *testing.T) {
	tests := []struct {
		name, criterion, want string
	}{
		{"missing k", `{"method":"pass_hat_k"}`, `"pass_hat_k" needs k, the number of runs it looks at together`},
		{"missing k with threshold", `{"method":"pass_at_k","threshold":0.5}`, `"pass_at_k" needs k, the number of runs it looks at together`},
		{"pass_rate with k", `{"method":"pass_rate","k":2,"threshold":0.5}`, `'pass_rate' does not take k`},
		{"pass_rate without threshold", `{"method":"pass_rate"}`, `'pass_rate' needs a threshold`},
		{"k zero", `{"method":"pass_at_k","k":0}`, `k must be at least 1`},
		{"threshold above one", `{"method":"pass_rate","threshold":1.5}`, `threshold must be between 0 and 1`},
		{"unknown method", `{"method":"mean"}`, `Unknown method: "mean". Built-in methods: [pass_at_k pass_hat_k pass_rate]`},
		{"unknown criterion key", `{"method":"pass_rate","threshold":0.5,"weight":1}`, `unknown field "weight"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"cross_run_criteria":[` + tt.criterion + `]}}`))
			if err == nil || !strings.Contains(err.Error(), "cross_run_criteria[0]: "+tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, "cross_run_criteria[0]: "+tt.want)
			}
		})
	}
}

func TestMultiRunConfigRejectsUnportedField(t *testing.T) {
	_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"cross_run_scoring":"min_over_runs"}}`))
	if err == nil || !strings.Contains(err.Error(), "multi_run_config.cross_run_scoring is not supported") {
		t.Errorf("error = %v, want multi_run_config.cross_run_scoring is not supported", err)
	}
}

// Mirrors adk-python test_eval_config.py:533.
func TestMultiRunConfigRejectsUnknownKey(t *testing.T) {
	_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"num_run":3}}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "num_run"`) {
		t.Errorf("error = %v, want unknown field \"num_run\"", err)
	}
}

// Mirrors adk-python test_multi_run_aggregation_methods.py:247-263.
func TestDisplayNameNamesKMethods(t *testing.T) {
	three := 3
	for c, want := range map[CrossRunCriterion]string{
		{Method: "pass_at_k", K: &three}:  "pass@3",
		{Method: "pass_hat_k", K: &three}: "pass^3",
		{Method: "pass_rate"}:             "pass_rate",
	} {
		if got := c.DisplayName(); got != want {
			t.Errorf("DisplayName(%+v) = %q, want %q", c, got, want)
		}
	}
}

// A null value is unset, as adk-python reads its Optional fields: pydantic's
// model_dump writes "threshold": null for a report-only criterion.
func TestMultiRunConfigReadsNullAsUnset(t *testing.T) {
	cfg, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":{"num_runs":null,"cross_run_criteria":[{"method":"pass_hat_k","k":2,"threshold":null}]}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig() error = %v", err)
	}
	if cfg.MultiRunConfig.NumRuns != nil {
		t.Errorf("NumRuns = %v, want nil", *cfg.MultiRunConfig.NumRuns)
	}
	if got := cfg.MultiRunConfig.CrossRunCriteria[0].Threshold; got != nil {
		t.Errorf("threshold = %v, want nil", *got)
	}
	_, err = UnmarshalEvalConfig([]byte(`{"multi_run_config":{"cross_run_criteria":[{"method":"pass_rate","threshold":null}]}}`))
	if err == nil || !strings.Contains(err.Error(), "'pass_rate' needs a threshold") {
		t.Errorf("pass_rate with a null threshold error = %v, want it to need a threshold", err)
	}
}

// Errors come in a fixed order: unknown keys, then num_runs, then criteria.
func TestMultiRunConfigChecksInOrder(t *testing.T) {
	for body, want := range map[string]string{
		`{"num_runs":0,"cross_run_criteria":[{"method":"bad"}]}`: "num_runs must be at least 1, got 0",
		`{"cross_run_criteria":[{"method":"bad"}],"zzz":1}`:      `unknown field "zzz"`,
	} {
		_, err := UnmarshalEvalConfig([]byte(`{"multi_run_config":` + body + `}`))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s error = %v, want %q", body, err, want)
		}
	}
}
