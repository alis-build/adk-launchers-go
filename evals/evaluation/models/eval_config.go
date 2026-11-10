package models

import (
	"encoding/json"
	"fmt"
)

// CodeConfig locates a custom metric function (Python CodeConfig.name).
type CodeConfig struct {
	Name string `json:"name"`
}

// CustomMetricConfig configures a user-defined metric.
type CustomMetricConfig struct {
	CodeConfig  CodeConfig  `json:"codeConfig"`
	MetricInfo  *MetricInfo `json:"metricInfo,omitempty"`
	Description string      `json:"description,omitempty"`
}

// EvalConfig holds evaluation criteria and simulator settings.
type EvalConfig struct {
	Criteria            map[string]jsonCriterion      `json:"criteria,omitempty"`
	CustomMetrics       map[string]CustomMetricConfig `json:"customMetrics,omitempty"`
	UserSimulatorConfig json.RawMessage               `json:"userSimulatorConfig,omitempty"`
	// MultiRunConfig repeats each case and judges it across the runs.
	MultiRunConfig *MultiRunConfig `json:"multiRunConfig,omitempty"`
	// Parallelism caps how many cases run and are scored at once; nil
	// leaves the service default.
	Parallelism *int `json:"parallelism,omitempty"`
}

// DefaultEvalConfig matches Python _DEFAULT_EVAL_CONFIG.
func DefaultEvalConfig() EvalConfig {
	return EvalConfig{
		Criteria: map[string]jsonCriterion{
			MetricToolTrajectoryAvgScore: {value: BaseCriterion{Threshold: 1.0}},
			MetricResponseMatchScore:     {value: BaseCriterion{Threshold: 0.8}},
		},
	}
}

// GetEvalMetricsFromConfig maps EvalConfig criteria to EvalMetric slice.
func GetEvalMetricsFromConfig(cfg EvalConfig) ([]EvalMetric, error) {
	var out []EvalMetric
	for name, criterion := range cfg.Criteria {
		var customPath *string
		if cfg.CustomMetrics != nil {
			if cm, ok := cfg.CustomMetrics[name]; ok {
				customPath = &cm.CodeConfig.Name
			}
		}
		threshold, err := thresholdFromCriterion(criterion)
		if err != nil {
			return nil, fmt.Errorf("criteria[%q]: %w", name, err)
		}
		out = append(out, EvalMetric{
			MetricName:         name,
			Threshold:          threshold,
			Criterion:          criterion,
			CustomFunctionPath: customPath,
		})
	}
	return out, nil
}

// thresholdFromCriterion extracts the pass threshold from any criterion variant.
func thresholdFromCriterion(c jsonCriterion) (float64, error) {
	if c.value == nil {
		return 0, fmt.Errorf("missing criterion value")
	}
	switch v := c.value.(type) {
	case BaseCriterion:
		return v.Threshold, nil
	case LlmAsAJudgeCriterion:
		return v.Threshold, nil
	case RubricsBasedCriterion:
		return v.Threshold, nil
	case HallucinationsCriterion:
		return v.Threshold, nil
	case ToolTrajectoryCriterion:
		return v.Threshold, nil
	case LlmBackedUserSimulatorCriterion:
		return v.Threshold, nil
	case float64:
		return v, nil
	default:
		return 0, fmt.Errorf("unsupported criterion type %T", c.value)
	}
}

// EffectiveThreshold returns the threshold a metric is graded with: the
// criterion's threshold whenever a criterion sets one, else m.Threshold. This
// is adk-python's _get_metric_threshold rule, except that a criterion without a
// threshold falls back to m.Threshold instead of being rejected.
func EffectiveThreshold(m EvalMetric) float64 {
	if m.Criterion.value == nil {
		return m.Threshold
	}
	t, err := thresholdFromCriterion(m.Criterion)
	if err != nil {
		return m.Threshold
	}
	// A criterion that sets no threshold decodes as 0, which would pass every
	// score. adk-python rejects such a request; here the top-level threshold
	// applies instead, as trajectory metrics always did.
	if t == 0 {
		return m.Threshold
	}
	return t
}

// UnmarshalEvalConfig parses EvalConfig JSON, accepting bare float thresholds in criteria.
func UnmarshalEvalConfig(data []byte) (EvalConfig, error) {
	var raw struct {
		Criteria            map[string]json.RawMessage    `json:"criteria"`
		CustomMetrics       map[string]CustomMetricConfig `json:"customMetrics"`
		UserSimulatorConfig json.RawMessage               `json:"userSimulatorConfig"`
		MultiRunCamel       json.RawMessage               `json:"multiRunConfig"`
		MultiRunSnake       json.RawMessage               `json:"multi_run_config"`
		Parallelism         *int                          `json:"parallelism"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return EvalConfig{}, err
	}
	cfg := EvalConfig{
		CustomMetrics:       raw.CustomMetrics,
		UserSimulatorConfig: raw.UserSimulatorConfig,
		Parallelism:         raw.Parallelism,
	}
	if cfg.Parallelism != nil && *cfg.Parallelism < 1 {
		return EvalConfig{}, fmt.Errorf("parallelism must be at least 1, got %d", *cfg.Parallelism)
	}
	// snake_case wins when both appear, as the run request's keys do.
	if mr := raw.MultiRunSnake; len(mr) > 0 && string(mr) != "null" {
		cfg.MultiRunConfig = &MultiRunConfig{}
		if err := json.Unmarshal(mr, cfg.MultiRunConfig); err != nil {
			return EvalConfig{}, err
		}
	} else if mr := raw.MultiRunCamel; len(mr) > 0 && string(mr) != "null" {
		cfg.MultiRunConfig = &MultiRunConfig{}
		if err := json.Unmarshal(mr, cfg.MultiRunConfig); err != nil {
			return EvalConfig{}, err
		}
	}
	if len(raw.Criteria) > 0 {
		cfg.Criteria = make(map[string]jsonCriterion, len(raw.Criteria))
		for name, v := range raw.Criteria {
			var f float64
			if err := json.Unmarshal(v, &f); err == nil {
				cfg.Criteria[name] = jsonCriterion{value: BaseCriterion{Threshold: f}}
				continue
			}
			var c jsonCriterion
			if err := json.Unmarshal(v, &c); err != nil {
				return EvalConfig{}, fmt.Errorf("criteria[%q]: %w", name, err)
			}
			cfg.Criteria[name] = c
		}
	}
	return cfg, nil
}

// defaultEfficiencyMetrics are reported on every eval, in this order
// (adk-python's _DEFAULT_EFFICIENCY_METRICS).
var defaultEfficiencyMetrics = []string{
	MetricToolCallCountV1,
	MetricInferenceCallCountV1,
	MetricTokenUsageV1,
	MetricInvocationDurationV1,
}

// AppendDefaultEfficiencyMetrics returns a new slice: metrics, then each
// informational efficiency metric metrics does not already name, with no
// threshold or criterion. A metric the caller named is kept as written, so a
// threshold on it is still refused. metrics is not modified.
func AppendDefaultEfficiencyMetrics(metrics []EvalMetric) []EvalMetric {
	out := make([]EvalMetric, 0, len(metrics)+len(defaultEfficiencyMetrics))
	out = append(out, metrics...)
	named := make(map[string]bool, len(metrics))
	for _, m := range metrics {
		named[m.MetricName] = true
	}
	for _, name := range defaultEfficiencyMetrics {
		if !named[name] {
			out = append(out, EvalMetric{MetricName: name})
		}
	}
	return out
}
