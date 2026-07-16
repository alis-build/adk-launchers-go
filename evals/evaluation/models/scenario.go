package models

import "encoding/json"

// PersonaBehavior describes a single behaviour the user simulator should
// exhibit. Ports adk-python simulation.pre_built_personas.PersonaBehavior.
type PersonaBehavior struct {
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	ViolationRubrics []string `json:"violationRubrics,omitempty"`
}

// UserPersona captures the persona description and behaviours consumed by the
// per-turn user simulator quality evaluator. Ports adk-python
// simulation.user_simulator_personas.UserPersona.
type UserPersona struct {
	Description string            `json:"description,omitempty"`
	Behaviors   []PersonaBehavior `json:"behaviors,omitempty"`
}

// ConversationScenario drives LLM-backed user simulation.
type ConversationScenario struct {
	StartingPrompt   string       `json:"startingPrompt"`
	ConversationPlan string       `json:"conversationPlan"`
	UserPersona      *UserPersona `json:"userPersona,omitempty"`
}

// UnmarshalJSON tolerates legacy or unknown-shape userPersona payloads by
// leaving UserPersona nil, so persisted eval sets continue to load.
func (s *ConversationScenario) UnmarshalJSON(data []byte) error {
	var raw struct {
		StartingPrompt   string          `json:"startingPrompt"`
		ConversationPlan string          `json:"conversationPlan"`
		UserPersona      json.RawMessage `json:"userPersona,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.StartingPrompt = raw.StartingPrompt
	s.ConversationPlan = raw.ConversationPlan
	if len(raw.UserPersona) == 0 || string(raw.UserPersona) == "null" {
		s.UserPersona = nil
		return nil
	}
	var persona UserPersona
	if err := json.Unmarshal(raw.UserPersona, &persona); err != nil {
		s.UserPersona = nil
		return nil
	}
	s.UserPersona = &persona
	return nil
}
