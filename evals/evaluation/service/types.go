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
	Parallelism int `json:"parallelism,omitempty"`
	// NumRuns is how many times each case runs; 0 or less means once.
	NumRuns            int  `json:"numRuns,omitempty"`
	UseLive            bool `json:"useLive,omitempty"`
	LiveTimeoutSeconds int  `json:"liveTimeoutSeconds,omitempty"`
}

// InferenceRequest loads eval cases and runs agent inference.
type InferenceRequest struct {
	AppName         string          `json:"appName"`
	EvalSetID       string          `json:"evalSetId"`
	EvalCaseIDs     []string        `json:"evalCaseIds,omitempty"`
	InferenceConfig InferenceConfig `json:"inferenceConfig"`
	// SessionState is run-level ADK session bootstrap state merged into each
	// case's sessionInput.state before inference; case keys win on conflict.
	SessionState map[string]any `json:"sessionState,omitempty"`
	// UserID, when set, is the ADK user every case runs as, replacing the
	// case's stored sessionInput.userId.
	UserID string `json:"userId,omitempty"`
}

// InferenceResult holds generated invocations for one eval case.
type InferenceResult struct {
	AppName    string `json:"appName"`
	EvalSetID  string `json:"evalSetId"`
	EvalCaseID string `json:"evalCaseId"`
	SessionID  string `json:"sessionId"`
	// UserID is the ADK user the inference session ran as.
	UserID       string              `json:"userId,omitempty"`
	Inferences   []models.Invocation `json:"inferences,omitempty"`
	Status       InferenceStatus     `json:"status"`
	ErrorMessage string              `json:"errorMessage,omitempty"`
	// RepetitionIndex is which of the case's runs this is, from 0.
	RepetitionIndex *int `json:"repetitionIndex,omitempty"`
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
