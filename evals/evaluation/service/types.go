package service

import (
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// InferenceStatus is the outcome of running inference for an eval case.
type InferenceStatus string

const (
	InferenceStatusSuccess InferenceStatus = "success"
	InferenceStatusFailed  InferenceStatus = "failed"
)

// InferenceConfig controls inference execution.
type InferenceConfig struct {
	Parallelism        int  `json:"parallelism,omitempty"`
	UseLive            bool `json:"useLive,omitempty"`
	LiveTimeoutSeconds int  `json:"liveTimeoutSeconds,omitempty"`
}

// InferenceRequest loads eval cases and runs agent inference.
type InferenceRequest struct {
	AppName         string          `json:"appName"`
	EvalSetID       string          `json:"evalSetId"`
	EvalCaseIDs     []string        `json:"evalCaseIds,omitempty"`
	InferenceConfig InferenceConfig `json:"inferenceConfig"`
}

// InferenceResult holds generated invocations for one eval case.
type InferenceResult struct {
	AppName      string              `json:"appName"`
	EvalSetID    string              `json:"evalSetId"`
	EvalCaseID   string              `json:"evalCaseId"`
	SessionID    string              `json:"sessionId"`
	Inferences   []models.Invocation `json:"inferences,omitempty"`
	Status       InferenceStatus     `json:"status"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
}

// EvaluateConfig selects metrics and parallelism for scoring.
type EvaluateConfig struct {
	EvalMetrics []models.EvalMetric `json:"evalMetrics"`
	Parallelism int                 `json:"parallelism,omitempty"`
}

// EvaluateRequest scores inference results.
type EvaluateRequest struct {
	InferenceResults []InferenceResult `json:"inferenceResults"`
	EvaluateConfig   EvaluateConfig    `json:"evaluateConfig"`
}
