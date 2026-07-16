package models

import (
	"bytes"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

// IntermediateData holds tool trajectory and intermediate agent responses.
type IntermediateData struct {
	ToolUses              []*genai.FunctionCall     `json:"toolUses,omitempty"`
	ToolResponses         []*genai.FunctionResponse `json:"toolResponses,omitempty"`
	IntermediateResponses []IntermediateResponse    `json:"intermediateResponses,omitempty"`
}

// IntermediateResponse is an intermediate sub-agent response (author + parts).
type IntermediateResponse struct {
	Author string        `json:"author"`
	Parts  []*genai.Part `json:"parts"`
}

// InvocationEvent is a projection of a session event for eval storage.
type InvocationEvent struct {
	Author  string         `json:"author"`
	Content *genai.Content `json:"content,omitempty"`
}

// InvocationEvents is a container of invocation-scoped events.
type InvocationEvents struct {
	InvocationEvents []InvocationEvent `json:"invocationEvents"`
}

// Invocation is a single user/agent turn in an eval case.
type Invocation struct {
	InvocationID      string           `json:"invocationId,omitempty"`
	UserContent       *genai.Content   `json:"userContent"`
	FinalResponse     *genai.Content   `json:"finalResponse,omitempty"`
	IntermediateData  jsonIntermediate `json:"intermediateData,omitempty"`
	CreationTimestamp float64          `json:"creationTimestamp,omitempty"`
	Rubrics           []Rubric         `json:"rubrics,omitempty"`
	AppDetails        *AppDetails      `json:"appDetails,omitempty"`
}

// jsonIntermediate unmarshals intermediateData as either IntermediateData or InvocationEvents.
type jsonIntermediate struct {
	value any
}

// UnmarshalJSON accepts IntermediateData or InvocationEvents-shaped JSON.
func (j *jsonIntermediate) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if jsonHasKey(data, "invocationEvents") {
		var events InvocationEvents
		if err := json.Unmarshal(data, &events); err != nil {
			return err
		}
		j.value = events
		return nil
	}
	var dataObj IntermediateData
	if err := json.Unmarshal(data, &dataObj); err != nil {
		return err
	}
	j.value = dataObj
	return nil
}

func (j jsonIntermediate) MarshalJSON() ([]byte, error) {
	if j.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(j.value)
}

// Value returns the decoded intermediate payload.
func (j jsonIntermediate) Value() any {
	return j.value
}

// AsIntermediateData returns toolUses/toolResponses form when present.
func (j jsonIntermediate) AsIntermediateData() (*IntermediateData, bool) {
	v, ok := j.value.(IntermediateData)
	if !ok {
		return nil, false
	}
	return &v, true
}

// AsInvocationEvents returns invocationEvents form when present.
func (j jsonIntermediate) AsInvocationEvents() (*InvocationEvents, bool) {
	v, ok := j.value.(InvocationEvents)
	if !ok {
		return nil, false
	}
	return &v, true
}

// GetAllToolCalls extracts function calls from intermediate data.
func GetAllToolCalls(intermediate jsonIntermediate) ([]*genai.FunctionCall, error) {
	if intermediate.value == nil {
		return nil, nil
	}
	if data, ok := intermediate.AsIntermediateData(); ok {
		return data.ToolUses, nil
	}
	if events, ok := intermediate.AsInvocationEvents(); ok {
		var calls []*genai.FunctionCall
		for _, ev := range events.InvocationEvents {
			if ev.Content == nil {
				continue
			}
			for _, p := range ev.Content.Parts {
				if p != nil && p.FunctionCall != nil {
					calls = append(calls, p.FunctionCall)
				}
			}
		}
		return calls, nil
	}
	return nil, fmt.Errorf("unsupported intermediate data type %T", intermediate.value)
}

// GetAllToolResponses extracts function responses from intermediate data.
func GetAllToolResponses(intermediate jsonIntermediate) ([]*genai.FunctionResponse, error) {
	if intermediate.value == nil {
		return nil, nil
	}
	if data, ok := intermediate.AsIntermediateData(); ok {
		return data.ToolResponses, nil
	}
	if events, ok := intermediate.AsInvocationEvents(); ok {
		var responses []*genai.FunctionResponse
		for _, ev := range events.InvocationEvents {
			if ev.Content == nil {
				continue
			}
			for _, p := range ev.Content.Parts {
				if p != nil && p.FunctionResponse != nil {
					responses = append(responses, p.FunctionResponse)
				}
			}
		}
		return responses, nil
	}
	return nil, fmt.Errorf("unsupported intermediate data type %T", intermediate.value)
}

// ToolCallAndResponse pairs a function call with an optional response.
type ToolCallAndResponse struct {
	Call     *genai.FunctionCall
	Response *genai.FunctionResponse
}

// GetAllToolCallsWithResponses returns tool calls paired with responses when available.
func GetAllToolCallsWithResponses(intermediate jsonIntermediate) ([]ToolCallAndResponse, error) {
	responses, err := GetAllToolResponses(intermediate)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*genai.FunctionResponse, len(responses))
	for _, r := range responses {
		if r != nil && r.ID != "" {
			byID[r.ID] = r
		}
	}
	calls, err := GetAllToolCalls(intermediate)
	if err != nil {
		return nil, err
	}
	out := make([]ToolCallAndResponse, 0, len(calls))
	for _, call := range calls {
		var resp *genai.FunctionResponse
		if call != nil {
			resp = byID[call.ID]
		}
		out = append(out, ToolCallAndResponse{Call: call, Response: resp})
	}
	return out, nil
}

// jsonHasKey reports whether a JSON object contains key at the top level,
// without allocating a map for the full payload.
func jsonHasKey(data []byte, key string) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return false
	}
	for dec.More() {
		fieldTok, err := dec.Token()
		if err != nil {
			return false
		}
		field, ok := fieldTok.(string)
		if !ok {
			return false
		}
		if field == key {
			return true
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return false
		}
	}
	return false
}
