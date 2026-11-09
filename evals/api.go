package evals

import (
	"encoding/json"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

const missingMetricRegistryMessage = "evals: metric registry is not configured"

// CreateEvalSetRequest is the POST /eval-sets request body.
type CreateEvalSetRequest struct {
	EvalSet models.EvalSet `json:"eval_set"`
}

// AddSessionToEvalSetRequest is the POST add-session request body.
type AddSessionToEvalSetRequest struct {
	EvalID    string `json:"eval_id"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
}

// RunEvalRequest is the POST run request body.
type RunEvalRequest struct {
	EvalIDs      []string            `json:"eval_ids,omitempty"`
	EvalCaseIDs  []string            `json:"eval_case_ids,omitempty"`
	EvalMetrics  []models.EvalMetric `json:"eval_metrics"`
	SessionState map[string]any      `json:"session_state,omitempty"`
	// LiveModelConfig and UserSimulatorConfig are adk-web main's live-mode and
	// audio-simulator settings, kept raw until they are supported. A field is
	// set when present and not JSON null; {} counts as set, as in adk-python.
	LiveModelConfig     json.RawMessage `json:"live_model_config,omitempty"`
	UserSimulatorConfig json.RawMessage `json:"user_simulator_config,omitempty"`
}

// RunEvalResponse wraps per-case run results.
type RunEvalResponse struct {
	RunEvalResults []models.RunEvalResult `json:"runEvalResults"`
}

// ListEvalSetsResponse lists eval set IDs.
type ListEvalSetsResponse struct {
	EvalSetIDs []string `json:"evalSetIds"`
}

// ListEvalResultsResponse lists persisted result IDs.
type ListEvalResultsResponse struct {
	EvalResultIDs []string `json:"evalResultIds"`
}

// ListMetricsInfoResponse lists registered metric metadata.
type ListMetricsInfoResponse struct {
	MetricsInfo []models.MetricInfo `json:"metricsInfo"`
}
