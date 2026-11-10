package models

import (
	"encoding/json"

	"google.golang.org/genai"
)

// Prebuilt metric name constants (Python PrebuiltMetrics).
const (
	MetricToolTrajectoryAvgScore                  = "tool_trajectory_avg_score"
	MetricResponseEvaluationScore                 = "response_evaluation_score"
	MetricResponseMatchScore                      = "response_match_score"
	MetricSafetyV1                                = "safety_v1"
	MetricFinalResponseMatchV2                    = "final_response_match_v2"
	MetricRubricBasedFinalResponseQualityV1       = "rubric_based_final_response_quality_v1"
	MetricHallucinationsV1                        = "hallucinations_v1"
	MetricRubricBasedToolUseQualityV1             = "rubric_based_tool_use_quality_v1"
	MetricPerTurnUserSimulatorQualityV1           = "per_turn_user_simulator_quality_v1"
	MetricMultiTurnTaskSuccessV1                  = "multi_turn_task_success_v1"
	MetricMultiTurnTrajectoryQualityV1            = "multi_turn_trajectory_quality_v1"
	MetricMultiTurnToolUseQualityV1               = "multi_turn_tool_use_quality_v1"
	MetricRubricBasedMultiTurnTrajectoryQualityV1 = "rubric_based_multi_turn_trajectory_quality_v1"
	// Informational efficiency metrics: they report a value per eval and
	// never pass or fail it.
	MetricToolCallCountV1      = "tool_call_count_v1"
	MetricInferenceCallCountV1 = "inference_call_count_v1"
	MetricTokenUsageV1         = "token_usage_v1"
	MetricInvocationDurationV1 = "invocation_duration_v1"
)

// JudgeModelOptions configures LLM-as-judge metrics.
type JudgeModelOptions struct {
	JudgeModel       string                       `json:"judgeModel,omitempty"`
	JudgeModelConfig *genai.GenerateContentConfig `json:"judgeModelConfig,omitempty"`
	NumSamples       int                          `json:"numSamples,omitempty"`
}

// BaseCriterion is the base threshold criterion for a metric.
type BaseCriterion struct {
	Threshold                           float64 `json:"threshold"`
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
	JudgeModelOptions               JudgeModelOptions `json:"judgeModelOptions,omitempty"`
	EvaluateIntermediateNLResponses bool              `json:"evaluateIntermediateNlResponses,omitempty"`
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
	// IgnoreArgs compares tool names only, not their arguments.
	IgnoreArgs bool `json:"ignoreArgs,omitempty"`
}

// EvalMetric describes a metric to run with threshold and criterion payload.
type EvalMetric struct {
	MetricName         string        `json:"metricName"`
	Threshold          float64       `json:"threshold"`
	Criterion          jsonCriterion `json:"criterion,omitempty"`
	CustomFunctionPath *string       `json:"customFunctionPath,omitempty"`
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
	_, hasMatch := raw["matchType"]
	_, hasMatchSnake := raw["match_type"]
	_, hasIgnore := raw["ignoreArgs"]
	_, hasIgnoreSnake := raw["ignore_args"]
	if hasMatch || hasMatchSnake || hasIgnore || hasIgnoreSnake {
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
	MinValue  float64 `json:"minValue"`
	MaxValue  float64 `json:"maxValue"`
	OpenAtMin bool    `json:"openAtMin"`
	OpenAtMax bool    `json:"openAtMax"`
}

// MetricValueInfo describes metric value semantics. Interval is absent for
// metrics whose values have no fixed range.
type MetricValueInfo struct {
	Interval *MetricValueInterval `json:"interval,omitempty"`
}

// MetricInfo describes a registered metric for metrics-info endpoint.
type MetricInfo struct {
	MetricName      string          `json:"metricName"`
	Description     string          `json:"description,omitempty"`
	MetricValueInfo MetricValueInfo `json:"metricValueInfo"`
	// RequiresThreshold tells a client (adk-web) to ask for a pass threshold;
	// metrics that only report a value set it false.
	RequiresThreshold bool `json:"requiresThreshold"`
}

// EvalMetricResultDetails holds a metric result's details; rubric scores are
// present only for rubric-based metrics.
type EvalMetricResultDetails struct {
	RubricScores []RubricScore `json:"rubricScores,omitempty"`
	// TokenUsageDetails breaks token_usage_v1's total down by token type.
	TokenUsageDetails *TokenUsageDetails `json:"tokenUsageDetails,omitempty"`
	// DurationDetails splits invocation_duration_v1's total into model, tool
	// and framework time.
	DurationDetails *DurationDetails `json:"durationDetails,omitempty"`
}

// TokenUsageDetails is every token count behind token_usage_v1, with
// adk-python's telemetry meanings: total is input plus output; input is
// prompt plus tool use, of which cached; output is candidates plus
// reasoning. A count is nil when no model call reported it.
type TokenUsageDetails struct {
	TotalTokens      *float64 `json:"totalTokens,omitempty"`
	InputTokens      *float64 `json:"inputTokens,omitempty"`
	PromptTokens     *float64 `json:"promptTokens,omitempty"`
	CachedTokens     *float64 `json:"cachedTokens,omitempty"`
	ToolUseTokens    *float64 `json:"toolUseTokens,omitempty"`
	OutputTokens     *float64 `json:"outputTokens,omitempty"`
	CandidatesTokens *float64 `json:"candidatesTokens,omitempty"`
	ReasoningTokens  *float64 `json:"reasoningTokens,omitempty"`
}

// DurationDetails partitions invocation_duration_v1's total: seconds
// awaiting a model, seconds inside tools, and the framework's own time. Each
// is nil when it was not measured.
type DurationDetails struct {
	TotalSeconds     *float64 `json:"totalSeconds,omitempty"`
	ModelSeconds     *float64 `json:"modelSeconds,omitempty"`
	ToolSeconds      *float64 `json:"toolSeconds,omitempty"`
	FrameworkSeconds *float64 `json:"frameworkSeconds,omitempty"`
}

// EvalMetricResult is the outcome for one metric.
type EvalMetricResult struct {
	MetricName string     `json:"metricName"`
	Threshold  float64    `json:"threshold"`
	Score      *float64   `json:"score,omitempty"`
	EvalStatus EvalStatus `json:"evalStatus"`
	// Details is always present on the wire, as adk-python always builds it;
	// it is {} when the metric reports nothing beyond its score.
	Details *EvalMetricResultDetails `json:"details"`
}

// EvalMetricResultPerInvocation holds per-turn metric results.
type EvalMetricResultPerInvocation struct {
	ActualInvocation   Invocation         `json:"actualInvocation"`
	ExpectedInvocation *Invocation        `json:"expectedInvocation,omitempty"`
	EvalMetricResults  []EvalMetricResult `json:"evalMetricResults"`
}

// EvalCaseResult is case-level evaluation output.
type EvalCaseResult struct {
	EvalSetID                     string                          `json:"evalSetId"`
	EvalID                        string                          `json:"evalId"`
	FinalEvalStatus               EvalStatus                      `json:"finalEvalStatus"`
	OverallEvalMetricResults      []EvalMetricResult              `json:"overallEvalMetricResults"`
	EvalMetricResultPerInvocation []EvalMetricResultPerInvocation `json:"evalMetricResultPerInvocation"`
	SessionID                     string                          `json:"sessionId"`
	SessionDetails                json.RawMessage                 `json:"sessionDetails,omitempty"`
	UserID                        *string                         `json:"userId,omitempty"`
	// RepetitionIndex is which of the case's runs this result is, from 0.
	RepetitionIndex *int `json:"repetitionIndex,omitempty"`
	// CrossRunResults judges the case across its repeated runs, one result
	// per multi_run_config cross-run criterion. It is kept apart from the
	// metric results, as adk-python keeps cross_run_results.
	CrossRunResults []CrossRunResult `json:"crossRunResults,omitempty"`
}

// CrossRunResult is one cross-run criterion's verdict on a case (adk-python
// MethodResult): the criterion's display name, its value over the runs, its
// threshold if any, and how many runs passed.
type CrossRunResult struct {
	Name       string     `json:"name"`
	Value      *float64   `json:"value,omitempty"`
	Threshold  *float64   `json:"threshold,omitempty"`
	EvalStatus EvalStatus `json:"evalStatus"`
	Note       string     `json:"note,omitempty"`
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
	EvalSetFile                   string                          `json:"evalSetFile,omitempty"`
	EvalSetID                     string                          `json:"evalSetId"`
	EvalID                        string                          `json:"evalId"`
	FinalEvalStatus               EvalStatus                      `json:"finalEvalStatus"`
	OverallEvalMetricResults      []EvalMetricResult              `json:"overallEvalMetricResults"`
	EvalMetricResultPerInvocation []EvalMetricResultPerInvocation `json:"evalMetricResultPerInvocation"`
	// EvalMetricResults is adk-python's deprecated per-case field, kept on the
	// wire (always []) for clients that still read it.
	EvalMetricResults []EvalMetricResult `json:"evalMetricResults"`
	UserID            *string            `json:"userId,omitempty"`
	SessionID         string             `json:"sessionId"`
}
