package models

import "encoding/json"

// ConversationScenario drives LLM-backed user simulation.
type ConversationScenario struct {
	StartingPrompt    string          `json:"startingPrompt"`
	ConversationPlan  string          `json:"conversationPlan"`
	UserPersona       json.RawMessage `json:"userPersona,omitempty"`
}
