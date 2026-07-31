package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genai"
)

// Legacy eval set JSON is a top-level array of cases in the old format.
type legacyEvalCase struct {
	Name           string             `json:"name"`
	Data           []legacyInvocation `json:"data"`
	InitialSession map[string]any     `json:"initial_session,omitempty"`
}

type legacyInvocation struct {
	Query                              string                   `json:"query"`
	Reference                          string                   `json:"reference,omitempty"`
	ExpectedToolUse                    []legacyToolUse          `json:"expected_tool_use,omitempty"`
	ExpectedIntermediateAgentResponses []legacyIntermediateResp `json:"expected_intermediate_agent_responses,omitempty"`
}

type legacyToolUse struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

type legacyIntermediateResp struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// ConvertLegacyEvalSetJSON migrates old eval set array JSON to EvalSet (Python convert_eval_set_to_pydantic_schema).
func ConvertLegacyEvalSetJSON(evalSetID string, data []byte) (EvalSet, error) {
	var legacyCases []legacyEvalCase
	if err := json.Unmarshal(data, &legacyCases); err != nil {
		return EvalSet{}, err
	}
	cases := make([]EvalCase, 0, len(legacyCases))
	now := float64(time.Now().Unix())
	for _, old := range legacyCases {
		invs := make([]Invocation, 0, len(old.Data))
		for _, oldInv := range old.Data {
			invs = append(invs, convertLegacyInvocation(oldInv))
		}
		var sessionInput *SessionInput
		if len(old.InitialSession) > 0 {
			sessionInput = &SessionInput{
				AppName: stringField(old.InitialSession, "app_name"),
				UserID:  stringField(old.InitialSession, "user_id"),
				State:   mapField(old.InitialSession, "state"),
			}
		}
		cases = append(cases, EvalCase{
			EvalID:            old.Name,
			Conversation:      invs,
			SessionInput:      sessionInput,
			CreationTimestamp: now,
		})
	}
	return EvalSet{
		EvalSetID:         evalSetID,
		EvalCases:         cases,
		CreationTimestamp: now,
	}, nil
}

// convertLegacyInvocation maps one legacy turn to the current Invocation schema.
func convertLegacyInvocation(old legacyInvocation) Invocation {
	toolUses := make([]*genai.FunctionCall, 0, len(old.ExpectedToolUse))
	for _, tu := range old.ExpectedToolUse {
		toolUses = append(toolUses, &genai.FunctionCall{
			Name: tu.ToolName,
			Args: tu.ToolInput,
		})
	}
	var intermediateResponses []IntermediateResponse
	for _, ir := range old.ExpectedIntermediateAgentResponses {
		intermediateResponses = append(intermediateResponses, IntermediateResponse{
			Author: ir.Author,
			Parts:  []*genai.Part{{Text: ir.Text}},
		})
	}
	return Invocation{
		InvocationID: uuid.NewString(),
		UserContent: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: old.Query}},
		},
		FinalResponse: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: old.Reference}},
		},
		IntermediateData: jsonIntermediate{value: IntermediateData{
			ToolUses:              toolUses,
			IntermediateResponses: intermediateResponses,
		}},
		CreationTimestamp: float64(time.Now().Unix()),
	}
}

// ParseEvalSetFile detects new EvalSet vs legacy array format and returns EvalSet.
func ParseEvalSetFile(evalSetID string, data []byte) (EvalSet, error) {
	trimmed := trimSpaceBytes(data)
	if len(trimmed) == 0 {
		return EvalSet{}, nil
	}
	if trimmed[0] == '[' {
		return ConvertLegacyEvalSetJSON(evalSetID, data)
	}
	var set EvalSet
	if err := json.Unmarshal(data, &set); err != nil {
		return EvalSet{}, err
	}
	if set.EvalSetID == "" {
		set.EvalSetID = evalSetID
	}
	if set.EvalCases == nil {
		set.EvalCases = []EvalCase{}
	}
	for i := range set.EvalCases {
		if err := set.EvalCases[i].Validate(); err != nil {
			return EvalSet{}, err
		}
	}
	return set, nil
}

// stringField reads a string field from a legacy initial_session map.
func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// mapField reads a nested map from a legacy initial_session map.
func mapField(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if st, ok := v.(map[string]any); ok {
			return st
		}
	}
	return map[string]any{}
}

// trimSpaceBytes trims ASCII whitespace from JSON file bytes before format detection.
func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
