package interrupt

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

func TestValidateResumeAgainstPending(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	pending := []Record{{
		ID:             "confirm-1",
		Reason:         "tool_call",
		ResponseSchema: ToolConfirmationResponseSchema(),
	}}

	t.Run("covers all pending", func(t *testing.T) {
		err := ValidateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, pending, now)
		if err != nil {
			t.Fatalf("ValidateResumeAgainstPending() error = %v", err)
		}
	})

	t.Run("missing resume when pending", func(t *testing.T) {
		err := ValidateResumeAgainstPending(nil, pending, now)
		if err == nil {
			t.Fatal("expected error when pending interrupts exist without resume")
		}
	})

	t.Run("unknown interrupt id", func(t *testing.T) {
		err := ValidateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "other",
			Status:      types.ResumeStatusCancelled,
		}}, pending, now)
		if err == nil {
			t.Fatal("expected error for unknown interruptId")
		}
	})

	t.Run("expired interrupt", func(t *testing.T) {
		expired := []Record{{
			ID:             "confirm-1",
			ExpiresAt:      "2020-01-01T00:00:00Z",
			ResponseSchema: ToolConfirmationResponseSchema(),
		}}
		err := ValidateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, expired, now)
		if err == nil {
			t.Fatal("expected error for expired interrupt")
		}
	})

	t.Run("resume without pending state", func(t *testing.T) {
		err := ValidateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, nil, now)
		if err == nil {
			t.Fatal("expected error for resume without pending interrupts")
		}
	})

	t.Run("cancelled must not have payload", func(t *testing.T) {
		err := ValidateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusCancelled,
			Payload:     map[string]any{"approved": false},
		}}, pending, now)
		if err == nil {
			t.Fatal("expected error for payload on cancelled resume")
		}
	})
}

func TestValidatePayloadAgainstSchema(t *testing.T) {
	schema := ToolConfirmationResponseSchema()

	t.Run("valid approved", func(t *testing.T) {
		err := ValidatePayloadAgainstSchema(map[string]any{"approved": true}, schema)
		if err != nil {
			t.Fatalf("ValidatePayloadAgainstSchema() error = %v", err)
		}
	})

	t.Run("missing approved", func(t *testing.T) {
		err := ValidatePayloadAgainstSchema(map[string]any{}, schema)
		if err == nil {
			t.Fatal("expected error for missing approved")
		}
	})

	t.Run("editedArgs must be object", func(t *testing.T) {
		err := ValidatePayloadAgainstSchema(map[string]any{
			"approved":   true,
			"editedArgs": "not-an-object",
		}, schema)
		if err == nil {
			t.Fatal("expected error for invalid editedArgs type")
		}
	})

	t.Run("integer rejects fractional float64", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"count": float64(3)}, schema); err != nil {
			t.Fatalf("whole float64 should pass integer check: %v", err)
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"count": float64(1.5)}, schema); err == nil {
			t.Fatal("expected error for fractional float64 in integer field")
		}
	})

	t.Run("enum constraint", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"quarter": map[string]any{
					"type": "string",
					"enum": []any{"Q1", "Q2", "Q3", "Q4"},
				},
			},
			"required": []any{"quarter"},
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"quarter": "Q1"}, schema); err != nil {
			t.Fatalf("valid enum: %v", err)
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"quarter": "Q5"}, schema); err == nil {
			t.Fatal("expected error for invalid enum value")
		}
	})

	t.Run("enum rejects cross-type match", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"flag": map[string]any{
					"type": "boolean",
					"enum": []any{true},
				},
			},
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"flag": true}, schema); err != nil {
			t.Fatalf("bool true should match enum [true]: %v", err)
		}
		if err := ValidatePayloadAgainstSchema(map[string]any{"flag": "true"}, schema); err == nil {
			t.Fatal("expected error: string 'true' should not match boolean true enum")
		}
	})
}

func TestRecordsFromInterrupts_PreservesInvocationID(t *testing.T) {
	interrupts := []types.Interrupt{{
		ID:     "confirm-1",
		Reason: "tool_call",
		Metadata: map[string]any{
			"adk": map[string]any{
				"invocationId":       "e-abc123",
				"confirmationCallId": "confirm-1",
			},
		},
	}}
	records := RecordsFromInterrupts(interrupts)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].InvocationID != "e-abc123" {
		t.Errorf("InvocationID = %q, want e-abc123", records[0].InvocationID)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTrip []Record
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip[0].InvocationID != "e-abc123" {
		t.Errorf("round-trip invocationId = %q, want e-abc123", roundTrip[0].InvocationID)
	}
}

func TestInvocationIDFromPending(t *testing.T) {
	pending := []Record{
		{ID: "confirm-1", InvocationID: "e-from-pending"},
		{ID: "confirm-2", InvocationID: ""},
	}
	t.Run("matches first resume entry with id", func(t *testing.T) {
		got := InvocationIDFromPending(pending, []types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
		}})
		if got != "e-from-pending" {
			t.Errorf("got %q, want e-from-pending", got)
		}
	})
	t.Run("skips empty pending invocation", func(t *testing.T) {
		got := InvocationIDFromPending(pending, []types.ResumeEntry{{
			InterruptID: "confirm-2",
			Status:      types.ResumeStatusResolved,
		}})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestToolConfirmationResponseSchemaIncludesEditedArgs(t *testing.T) {
	schema := ToolConfirmationResponseSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing")
	}
	if _, ok := props["editedArgs"]; !ok {
		t.Fatal("editedArgs property missing from response schema")
	}
}
