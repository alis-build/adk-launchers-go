package models

import (
	"encoding/json"
	"fmt"
)

// SessionInput initializes a session for eval runs.
type SessionInput struct {
	AppName string         `json:"appName"`
	UserID  string         `json:"userId"`
	State   map[string]any `json:"state,omitempty"`
	Extra   map[string]any `json:"-"`
}

// UnmarshalJSON preserves unknown SessionInput fields in Extra for forward compatibility.
func (s *SessionInput) UnmarshalJSON(data []byte) error {
	type alias SessionInput
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	s.AppName = a.AppName
	s.UserID = a.UserID
	s.State = a.State

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delete(raw, "appName")
	delete(raw, "userId")
	delete(raw, "state")
	if len(raw) > 0 {
		s.Extra = make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return err
			}
			s.Extra[k] = val
		}
	}
	return nil
}

// MarshalJSON merges Extra fields into the on-wire SessionInput object.
func (s SessionInput) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 3+len(s.Extra))
	m["appName"] = s.AppName
	m["userId"] = s.UserID
	if s.State != nil {
		m["state"] = s.State
	}
	for k, v := range s.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// EvalCase is a single evaluation case.
type EvalCase struct {
	EvalID               string                `json:"evalId"`
	Conversation         []Invocation          `json:"conversation,omitempty"`
	ConversationScenario *ConversationScenario `json:"conversationScenario,omitempty"`
	SessionInput         *SessionInput         `json:"sessionInput,omitempty"`
	CreationTimestamp    float64               `json:"creationTimestamp,omitempty"`
	Rubrics              []Rubric              `json:"rubrics,omitempty"`
	FinalSessionState    map[string]any        `json:"finalSessionState,omitempty"`
	Extra                map[string]any        `json:"-"`
}

// UnmarshalJSON loads an eval case and validates conversation XOR scenario.
func (c *EvalCase) UnmarshalJSON(data []byte) error {
	type alias EvalCase
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	c.EvalID = a.EvalID
	c.Conversation = a.Conversation
	c.ConversationScenario = a.ConversationScenario
	c.SessionInput = a.SessionInput
	c.CreationTimestamp = a.CreationTimestamp
	c.Rubrics = a.Rubrics
	c.FinalSessionState = a.FinalSessionState

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, k := range []string{
		"evalId", "conversation", "conversationScenario", "sessionInput",
		"creationTimestamp", "rubrics", "finalSessionState",
	} {
		delete(raw, k)
	}
	if len(raw) > 0 {
		c.Extra = make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return err
			}
			c.Extra[k] = val
		}
	}
	return c.Validate()
}

// MarshalJSON serializes the eval case after validating invariants.
func (c EvalCase) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	m := map[string]any{
		"evalId": c.EvalID,
	}
	if c.Conversation != nil {
		m["conversation"] = c.Conversation
	}
	if c.ConversationScenario != nil {
		m["conversationScenario"] = c.ConversationScenario
	}
	if c.SessionInput != nil {
		m["sessionInput"] = c.SessionInput
	}
	if c.CreationTimestamp != 0 {
		m["creationTimestamp"] = c.CreationTimestamp
	}
	if len(c.Rubrics) > 0 {
		m["rubrics"] = c.Rubrics
	}
	if len(c.FinalSessionState) > 0 {
		m["finalSessionState"] = c.FinalSessionState
	}
	for k, v := range c.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// Validate checks eval case invariants (conversation XOR conversation_scenario).
func (c EvalCase) Validate() error {
	hasConversation := c.Conversation != nil
	hasScenario := c.ConversationScenario != nil
	if hasConversation == hasScenario {
		return fmt.Errorf("exactly one of conversation and conversationScenario must be provided in an EvalCase")
	}
	return nil
}

// EvalSet is a collection of eval cases.
type EvalSet struct {
	EvalSetID          string     `json:"eval_set_id"`
	Name               *string    `json:"name,omitempty"`
	Description        *string    `json:"description,omitempty"`
	EvalCases          []EvalCase `json:"eval_cases"`
	CreationTimestamp  float64    `json:"creation_timestamp,omitempty"`
	ModelExecutionMode *string    `json:"model_execution_mode,omitempty"`
	ToolExecutionMode  *string    `json:"tool_execution_mode,omitempty"`
}

// UnmarshalJSON accepts snake_case and camelCase eval set field names.
func (s *EvalSet) UnmarshalJSON(data []byte) error {
	type evalSetAlias EvalSet
	if err := json.Unmarshal(data, (*evalSetAlias)(s)); err != nil {
		return err
	}
	if s.EvalSetID != "" {
		return nil
	}
	var camel struct {
		EvalSetID          string     `json:"evalSetId"`
		Name               *string    `json:"name,omitempty"`
		Description        *string    `json:"description,omitempty"`
		EvalCases          []EvalCase `json:"evalCases"`
		CreationTimestamp  float64    `json:"creationTimestamp,omitempty"`
		ModelExecutionMode *string    `json:"modelExecutionMode,omitempty"`
		ToolExecutionMode  *string    `json:"toolExecutionMode,omitempty"`
	}
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}
	s.EvalSetID = camel.EvalSetID
	s.Name = camel.Name
	s.Description = camel.Description
	s.EvalCases = camel.EvalCases
	s.CreationTimestamp = camel.CreationTimestamp
	s.ModelExecutionMode = camel.ModelExecutionMode
	s.ToolExecutionMode = camel.ToolExecutionMode
	return nil
}
