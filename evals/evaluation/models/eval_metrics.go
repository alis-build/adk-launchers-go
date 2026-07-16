package models

import (
	"encoding/json"

	"google.golang.org/genai"
)

// Prebuilt metric name constants (Python PrebuiltMetrics).
const (
	MetricToolTrajectoryAvgScore                    = "tool_trajectory_avg_score"
	MetricResponseEvaluationScore                   = "response_evaluation_score"
	MetricResponseMatchScore                        = "response_match_score"
	MetricSafetyV1                                  = "safety_v1"
	MetricFinalResponseMatchV2                      = "final_response_match_v2"
	MetricRubricBasedFinalResponseQualityV1         = "rubric_based_final_response_quality_v1"
	MetricHallucinationsV1                          = "hallucinations_v1"
	MetricRubricBasedToolUseQualityV1               = "rubric_based_tool_use_quality_v1"
	MetricPerTurnUserSimulatorQualityV1             = "per_turn_user_simulator_quality_v1"
	MetricMultiTurnTaskSuccessV1                    = "multi_turn_task_success_v1"
	MetricMultiTurnTrajectoryQualityV1              = "multi_turn_trajectory_quality_v1"
	MetricMultiTurnToolUseQualityV1                 = "multi_turn_tool_use_quality_v1"
	MetricRubricBasedMultiTurnTrajectoryQualityV1   = "rubric_based_multi_turn_trajectory_quality_v1"
)

// JudgeModelOptions configures LLM-as-judge metrics.
type JudgeModelOptions struct {
	JudgeModel       string                    `json:"judgeModel,omitempty"`
	JudgeModelConfig *genai.GenerateContentConfig `json:"judgeModelConfig,omitempty"`
	NumSamples       int                       `json:"numSamples,omitempty"`
}

// BaseCriterion is the base threshold criterion for a metric.
type BaseCriterion struct {
	Threshold                          float64 `json:"threshold"`
	IncludeIntermediateResponsesInFinal bool    `json:"includeIntermediateResponsesInFinal,omitempty"`
}

// LlmAsAJudgeCriterion extends BaseCriterion with judge model options.
type LlmAsAJudgeCriterion struct {
	BaseCriterion
	JudgeModelOptions JudgeModelOptions `json:"judgeModelOptions,omitempty"`
}

// LlmBackedUserSimulatorCriterion adds a stop signal to configure the per-turn
// user simulator quality evaluator. Matches adk-python
// LlmBackedUserSimulatorCriterion (eval_metrics.py).
type LlmBackedUserSimulatorCriterion struct {
	LlmAsAJudgeCriterion
	StopSignal string `json:"stopSignal,omitempty"`
}

// DefaultUserSimulatorStopSignal is the Python default for stop_signal.
const DefaultUserSimulatorStopSignal = "</finished>"

// RubricsBasedCriterion adds rubrics for rubric-based metrics.
type RubricsBasedCriterion struct {
	BaseCriterion
	JudgeModelOptions JudgeModelOptions `json:"judgeModelOptions,omitempty"`
	Rubrics           []Rubric          `json:"rubrics,omitempty"`
}

// HallucinationsCriterion configures hallucination detection.
type HallucinationsCriterion struct {
	BaseCriterion
	JudgeModelOptions                 JudgeModelOptions `json:"judgeModelOptions,omitempty"`
	EvaluateIntermediateNLResponses   bool              `json:"evaluateIntermediateNlResponses,omitempty"`
}

// ToolTrajectoryMatchType controls trajectory comparison mode.
type ToolTrajectoryMatchType int

const (
	ToolTrajectoryMatchExact ToolTrajectoryMatchType = iota
	ToolTrajectoryMatchInOrder
	ToolTrajectoryMatchAnyOrder
)

// ToolTrajectoryCriterion configures tool trajectory scoring.
type ToolTrajectoryCriterion struct {
	BaseCriterion
	MatchType ToolTrajectoryMatchType `json:"matchType,omitempty"`
}

// EvalMetric describes a metric to run with threshold and criterion payload.
type EvalMetric struct {
	MetricName           string          `json:"metricName"`
	Threshold            float64         `json:"threshold"`
	Criterion            jsonCriterion   `json:"criterion,omitempty"`
	CustomFunctionPath   *string         `json:"customFunctionPath,omitempty"`
}

// jsonCriterion holds unmarshaled criterion variants for EvalMetric.
type jsonCriterion struct {
	value any
}

// UnmarshalJSON probes criterion JSON for specialized fields before falling back to BaseCriterion.
func (c *jsonCriterion) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var base BaseCriterion
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	// Try specialized types by probing distinctive fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["rubrics"]; ok {
		var r RubricsBasedCriterion
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		c.value = r
		return nil
	}
	if _, ok := raw["evaluateIntermediateNlResponses"]; ok {
		var h HallucinationsCriterion
		if err := json.Unmarshal(data, &h); err != nil {
			return err
		}
		c.value = h
		return nil
	}
	if _, ok := raw["matchType"]; ok {
		var t ToolTrajectoryCriterion
		if err := json.Unmarshal(data, &t); err != nil {
			return err
		}
		c.value = t
		return nil
	}
	if _, ok := raw["stopSignal"]; ok {
		var u LlmBackedUserSimulatorCriterion
		if err := json.Unmarshal(data, &u); err != nil {
			return err
		}
		c.value = u
		return nil
	}
	if _, ok := raw["judgeModelOptions"]; ok {
		var l LlmAsAJudgeCriterion
		if err := json.Unmarshal(data, &l); err != nil {
			return err
		}
		c.value = l
		return nil
	}
	c.value = base
	return nil
}

func (c jsonCriterion) MarshalJSON() ([]byte, error) {
	if c.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(c.value)
}

// MetricValueInterval describes valid score bounds for a metric.
type MetricValueInterval struct {
	MinValue   float64 `json:"minValue"`
	MaxValue   float64 `json:"maxValue"`
	OpenAtMin  bool    `json:"openAtMin,omitempty"`
	OpenAtMax  bool    `json:"openAtMax,omitempty"`
}

// MetricValueInfo describes metric value semantics.
type MetricValueInfo struct {
	Interval MetricValueInterval `json:"interval"`
}

// MetricInfo describes a registered metric for metrics-info endpoint.
type MetricInfo struct {
	MetricName      string          `json:"metricName"`
	Description     string          `json:"description,omitempty"`
	MetricValueInfo MetricValueInfo `json:"metricValueInfo,omitempty"`
}

// EvalMetricResultDetails holds optional rubric scores.
type EvalMetricResultDetails struct {
	RubricScores []RubricScore `json:"rubricScores,omitempty"`
}

// EvalMetricResult is the outcome for one metric.
type EvalMetricResult struct {
	MetricName  string                   `json:"metricName"`
	Threshold   float64                  `json:"threshold"`
	Score       *float64                 `json:"score,omitempty"`
	EvalStatus  EvalStatus               `json:"evalStatus"`
	Details     *EvalMetricResultDetails `json:"details,omitempty"`
}

// EvalMetricResultPerInvocation holds per-turn metric results.
type EvalMetricResultPerInvocation struct {
	ActualInvocation   Invocation         `json:"actualInvocation"`
	ExpectedInvocation *Invocation        `json:"expectedInvocation,omitempty"`
	EvalMetricResults  []EvalMetricResult `json:"evalMetricResults"`
}

// EvalCaseResult is case-level evaluation output.
type EvalCaseResult struct {
	EvalSetID                      string                          `json:"evalSetId"`
	EvalID                         string                          `json:"evalId"`
	FinalEvalStatus                EvalStatus                      `json:"finalEvalStatus"`
	OverallEvalMetricResults       []EvalMetricResult              `json:"overallEvalMetricResults"`
	EvalMetricResultPerInvocation  []EvalMetricResultPerInvocation `json:"evalMetricResultPerInvocation"`
	SessionID                      string                          `json:"sessionId"`
	SessionDetails                 json.RawMessage                 `json:"sessionDetails,omitempty"`
	UserID                         *string                         `json:"userId,omitempty"`
}

// EvalSetResult is set-level evaluation output persisted to history.
type EvalSetResult struct {
	EvalSetResultID   string           `json:"evalSetResultId"`
	EvalSetResultName *string          `json:"evalSetResultName,omitempty"`
	EvalSetID         string           `json:"evalSetId"`
	EvalCaseResults   []EvalCaseResult `json:"evalCaseResults"`
	CreationTimestamp float64          `json:"creationTimestamp,omitempty"`
}

// RunEvalResult is the legacy per-case response from run_eval.
type RunEvalResult struct {
	EvalSetFile                    string                          `json:"evalSetFile,omitempty"`
	EvalSetID                      string                          `json:"evalSetId"`
	EvalID                         string                          `json:"evalId"`
	FinalEvalStatus                EvalStatus                      `json:"finalEvalStatus"`
	OverallEvalMetricResults       []EvalMetricResult              `json:"overallEvalMetricResults"`
	EvalMetricResultPerInvocation  []EvalMetricResultPerInvocation `json:"evalMetricResultPerInvocation"`
	UserID                         *string                         `json:"userId,omitempty"`
	SessionID                      string                          `json:"sessionId"`
}
