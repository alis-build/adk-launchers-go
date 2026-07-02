package models

import "encoding/json"

// AgentDetails describes one agent in the app tree for metric evaluators.
type AgentDetails struct {
	Name             string          `json:"name"`
	Instructions     string          `json:"instructions,omitempty"`
	ToolDeclarations json.RawMessage `json:"toolDeclarations,omitempty"`
}

// AppDetails captures agent instructions and tools seen during an invocation.
type AppDetails struct {
	AgentDetails map[string]AgentDetails `json:"agentDetails"`
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
