// Package interrupt implements the AG-UI human-in-the-loop interrupt protocol:
// resume mapping, validation, and JSON-schema checks for pending interrupts.
package interrupt

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// Record is the JSON-serializable subset of [types.Interrupt] stored in ADK
// session state for server-side resume validation.
type Record struct {
	ID             string         `json:"id"`
	Reason         string         `json:"reason"`
	ExpiresAt      string         `json:"expiresAt,omitempty"`
	ResponseSchema map[string]any `json:"responseSchema,omitempty"`
	InvocationID   string         `json:"invocationId,omitempty"`
}

// ToolConfirmationResponseSchema returns the JSON Schema advertised to clients
// on tool-bound interrupts.
func ToolConfirmationResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{"type": "boolean"},
			"editedArgs": map[string]any{
				"type":        "object",
				"description": "Full replacement of the tool args. Not merged.",
			},
		},
		"required": []any{"approved"},
	}
}

// RecordsFromInterrupts copies interrupt fields needed for validation into the
// session-persisted form.
func RecordsFromInterrupts(interrupts []types.Interrupt) []Record {
	out := make([]Record, len(interrupts))
	for i, intr := range interrupts {
		out[i] = Record{
			ID:             intr.ID,
			Reason:         intr.Reason,
			ExpiresAt:      intr.ExpiresAt,
			ResponseSchema: intr.ResponseSchema,
			InvocationID:   InvocationIDFromMetadata(intr.Metadata),
		}
	}
	return out
}

// InvocationIDFromMetadata reads metadata.adk.invocationId from an AG-UI interrupt.
func InvocationIDFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	adkMeta, ok := metadata["adk"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := adkMeta["invocationId"].(string)
	return id
}

// InvocationIDFromPending returns the invocation id for the first resume entry
// that matches a pending record with a non-empty InvocationID.
func InvocationIDFromPending(pending []Record, entries []types.ResumeEntry) string {
	if len(pending) == 0 || len(entries) == 0 {
		return ""
	}
	byID := make(map[string]Record, len(pending))
	for _, p := range pending {
		byID[p.ID] = p
	}
	for _, entry := range entries {
		if entry.InterruptID == "" {
			continue
		}
		if rec, ok := byID[entry.InterruptID]; ok && rec.InvocationID != "" {
			return rec.InvocationID
		}
	}
	return ""
}

// DecodeRecords normalizes session state values that may have been stored as
// typed slices or generic JSON-decoded []any.
func DecodeRecords(raw any) ([]Record, error) {
	switch v := raw.(type) {
	case []Record:
		return v, nil
	case []any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var out []Record
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("pending interrupts: unsupported type %T", raw)
		}
		var out []Record
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// EntriesToConfirmationContent converts AG-UI resume entries into a single ADK
// user [genai.Content] containing one FunctionResponse part per entry.
func EntriesToConfirmationContent(entries []types.ResumeEntry) (*genai.Content, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("resume entries must not be empty")
	}

	parts := make([]*genai.Part, 0, len(entries))
	for i, entry := range entries {
		if entry.InterruptID == "" {
			return nil, fmt.Errorf("resume[%d]: interruptId is required", i)
		}

		response, err := confirmationResponseFromResumeEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("resume[%d]: %w", i, err)
		}

		parts = append(parts, &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				Name:     toolconfirmation.FunctionCallName,
				ID:       entry.InterruptID,
				Response: response,
			},
		})
	}

	return genai.NewContentFromParts(parts, genai.RoleUser), nil
}

func confirmationResponseFromResumeEntry(entry types.ResumeEntry) (map[string]any, error) {
	switch entry.Status {
	case types.ResumeStatusCancelled:
		return map[string]any{"confirmed": false}, nil

	case types.ResumeStatusResolved:
		payload, err := ResumePayloadMap(entry.Payload)
		if err != nil {
			return nil, err
		}
		approved, ok := payload["approved"].(bool)
		if !ok {
			return nil, fmt.Errorf("resolved resume requires payload.approved (bool)")
		}

		response := map[string]any{"confirmed": approved}
		if editedArgs, ok := payload["editedArgs"]; ok {
			response["payload"] = editedArgs
		} else if len(payload) > 1 {
			extra := make(map[string]any, len(payload)-1)
			for k, v := range payload {
				if k == "approved" {
					continue
				}
				extra[k] = v
			}
			if len(extra) > 0 {
				response["payload"] = extra
			}
		}
		return response, nil

	default:
		return nil, fmt.Errorf("unsupported resume status %q", entry.Status)
	}
}

// ResumePayloadMap extracts the resume payload object for a resolved entry.
func ResumePayloadMap(payload any) (map[string]any, error) {
	if payload == nil {
		return nil, fmt.Errorf("resolved resume requires a payload")
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("payload must be a JSON object, got %T", payload)
	}
	return m, nil
}

// ValidateResumeAgainstPending enforces AG-UI interrupt contract rules when
// session state contains pending interrupts.
func ValidateResumeAgainstPending(entries []types.ResumeEntry, pending []Record, now time.Time) error {
	pendingByID := make(map[string]Record, len(pending))
	for _, p := range pending {
		pendingByID[p.ID] = p
	}

	if len(pending) > 0 {
		if len(entries) == 0 {
			return fmt.Errorf("thread has %d pending interrupt(s); resume is required", len(pending))
		}
		resumeIDs := make(map[string]struct{}, len(entries))
		for i, entry := range entries {
			if entry.InterruptID == "" {
				return fmt.Errorf("resume[%d]: interruptId is required", i)
			}
			if _, dup := resumeIDs[entry.InterruptID]; dup {
				return fmt.Errorf("resume[%d]: duplicate interruptId %q", i, entry.InterruptID)
			}
			resumeIDs[entry.InterruptID] = struct{}{}

			rec, ok := pendingByID[entry.InterruptID]
			if !ok {
				return fmt.Errorf("resume[%d]: unknown interruptId %q", i, entry.InterruptID)
			}
			if rec.ExpiresAt != "" && IsExpired(rec.ExpiresAt, now) {
				return fmt.Errorf("resume[%d]: interrupt %q has expired", i, entry.InterruptID)
			}
			if entry.Status == types.ResumeStatusResolved {
				payload, err := ResumePayloadMap(entry.Payload)
				if err != nil {
					return fmt.Errorf("resume[%d]: %w", i, err)
				}
				if err := ValidatePayloadAgainstSchema(payload, rec.ResponseSchema); err != nil {
					return fmt.Errorf("resume[%d]: %w", i, err)
				}
			} else if entry.Status != types.ResumeStatusCancelled {
				return fmt.Errorf("resume[%d]: unsupported status %q", i, entry.Status)
			} else if entry.Payload != nil {
				return fmt.Errorf("resume[%d]: cancelled resume must not include payload", i)
			}
		}
		for id := range pendingByID {
			if _, ok := resumeIDs[id]; !ok {
				return fmt.Errorf("resume missing interruptId %q", id)
			}
		}
		return nil
	}

	if len(entries) > 0 {
		return fmt.Errorf("resume is not valid: no pending interrupts for this thread")
	}
	return nil
}

// IsExpired reports whether expiresAt (ISO 8601) is in the past relative to now.
func IsExpired(expiresAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, expiresAt)
	}
	if err != nil {
		return false
	}
	return !t.After(now)
}

// ValidatePayloadAgainstSchema performs a minimal subset of JSON Schema validation
// for interrupt resume payloads.
func ValidatePayloadAgainstSchema(payload map[string]any, schema map[string]any) error {
	if schema == nil {
		return nil
	}
	required := stringSliceFromSchema(schema["required"])
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			return fmt.Errorf("payload missing required field %q", key)
		}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	for propName, propSchema := range props {
		propMap, ok := propSchema.(map[string]any)
		if !ok {
			continue
		}
		val, exists := payload[propName]
		if !exists {
			continue
		}
		if err := validateJSONSchemaValue(val, propMap, propName); err != nil {
			return err
		}
	}
	return nil
}

func validateJSONSchemaValue(value any, schema map[string]any, field string) error {
	if err := validateJSONSchemaType(value, schema, field); err != nil {
		return err
	}
	return validateJSONSchemaEnum(value, schema, field)
}

func stringSliceFromSchema(v any) []string {
	switch req := v.(type) {
	case []string:
		return req
	case []any:
		out := make([]string, 0, len(req))
		for _, item := range req {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func validateJSONSchemaType(value any, schema map[string]any, field string) error {
	want, _ := schema["type"].(string)
	if want == "" {
		return nil
	}
	switch want {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", field)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s must be an object", field)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", field)
		}
	case "integer":
		switch v := value.(type) {
		case int, int32, int64:
		case float64:
			if v != math.Trunc(v) {
				return fmt.Errorf("%s must be an integer", field)
			}
		default:
			return fmt.Errorf("%s must be an integer", field)
		}
	case "number":
		switch value.(type) {
		case int, int32, int64, float32, float64:
		default:
			return fmt.Errorf("%s must be a number", field)
		}
	}
	if want == "object" {
		if obj, ok := value.(map[string]any); ok {
			if nestedProps, ok := schema["properties"].(map[string]any); ok {
				for nestedName, nestedSchema := range nestedProps {
					nestedMap, ok := nestedSchema.(map[string]any)
					if !ok {
						continue
					}
					nestedVal, exists := obj[nestedName]
					if !exists {
						continue
					}
					if err := validateJSONSchemaValue(nestedVal, nestedMap, field+"."+nestedName); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validateJSONSchemaEnum(value any, schema map[string]any, field string) error {
	enumRaw, ok := schema["enum"]
	if !ok {
		return nil
	}
	enumSlice, ok := enumRaw.([]any)
	if !ok {
		return nil
	}
	for _, allowed := range enumSlice {
		if reflect.DeepEqual(allowed, value) {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of the allowed enum values", field)
}
