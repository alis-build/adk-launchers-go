package models

import (
	"encoding/json"

	"google.golang.org/genai"
)

// AgentDetails describes one agent in the app tree for metric evaluators.
type AgentDetails struct {
	Name             string        `json:"name"`
	Instructions     string        `json:"instructions,omitempty"`
	ToolDeclarations []*genai.Tool `json:"toolDeclarations,omitempty"`
}

// AppDetails captures agent instructions and tools seen during an invocation.
type AppDetails struct {
	AgentDetails map[string]AgentDetails `json:"agentDetails,omitempty"`
}

// UnmarshalJSON tolerates malformed or unknown-shape payloads by leaving
// AgentDetails nil rather than surfacing a parse error to callers loading
// persisted eval sets whose AppDetails predate the typed schema.
func (a *AppDetails) UnmarshalJSON(data []byte) error {
	type alias AppDetails
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		// Legacy raw form: preserve the field's presence but drop its contents.
		return nil
	}
	*a = AppDetails(v)
	return nil
}

// GetDeveloperInstructions returns instructions for the named agent.
func (a *AppDetails) GetDeveloperInstructions(agentName string) (string, error) {
	if a == nil {
		return "", errAgentNotFound(agentName)
	}
	details, ok := a.AgentDetails[agentName]
	if !ok {
		return "", errAgentNotFound(agentName)
	}
	return details.Instructions, nil
}

// errAgentNotFound builds an error when AppDetails lacks the requested agent.
func errAgentNotFound(name string) error {
	return &agentNotFoundError{name: name}
}

// agentNotFoundError indicates a missing agent in AppDetails.
type agentNotFoundError struct {
	name string
}

func (e *agentNotFoundError) Error() string {
	return "`" + e.name + "` not found in the agentic system."
}
