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
	Criteria            map[string]jsonCriterion     `json:"criteria,omitempty"`
	CustomMetrics       map[string]CustomMetricConfig `json:"customMetrics,omitempty"`
	UserSimulatorConfig json.RawMessage              `json:"userSimulatorConfig,omitempty"`
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
	case float64:
		return v, nil
	default:
		return 0, fmt.Errorf("unsupported criterion type %T", c.value)
	}
}

// UnmarshalEvalConfig parses EvalConfig JSON, accepting bare float thresholds in criteria.
func UnmarshalEvalConfig(data []byte) (EvalConfig, error) {
	var raw struct {
		Criteria            map[string]json.RawMessage    `json:"criteria"`
		CustomMetrics       map[string]CustomMetricConfig `json:"customMetrics"`
		UserSimulatorConfig json.RawMessage               `json:"userSimulatorConfig"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return EvalConfig{}, err
	}
	cfg := EvalConfig{
		CustomMetrics:       raw.CustomMetrics,
		UserSimulatorConfig: raw.UserSimulatorConfig,
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
