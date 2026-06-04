package agui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

func TestValidateResumeAgainstPending(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	pending := []pendingInterruptRecord{{
		ID:             "confirm-1",
		Reason:         "tool_call",
		ResponseSchema: toolConfirmationResponseSchema(),
	}}

	t.Run("covers all pending", func(t *testing.T) {
		err := validateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, pending, now)
		if err != nil {
			t.Fatalf("validateResumeAgainstPending() error = %v", err)
		}
	})

	t.Run("missing resume when pending", func(t *testing.T) {
		err := validateResumeAgainstPending(nil, pending, now)
		if err == nil {
			t.Fatal("expected error when pending interrupts exist without resume")
		}
	})

	t.Run("unknown interrupt id", func(t *testing.T) {
		err := validateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "other",
			Status:      types.ResumeStatusCancelled,
		}}, pending, now)
		if err == nil {
			t.Fatal("expected error for unknown interruptId")
		}
	})

	t.Run("expired interrupt", func(t *testing.T) {
		expired := []pendingInterruptRecord{{
			ID:             "confirm-1",
			ExpiresAt:      "2020-01-01T00:00:00Z",
			ResponseSchema: toolConfirmationResponseSchema(),
		}}
		err := validateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, expired, now)
		if err == nil {
			t.Fatal("expected error for expired interrupt")
		}
	})

	t.Run("resume without pending state", func(t *testing.T) {
		err := validateResumeAgainstPending([]types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, nil, now)
		if err == nil {
			t.Fatal("expected error for resume without pending interrupts")
		}
	})

	t.Run("cancelled must not have payload", func(t *testing.T) {
		err := validateResumeAgainstPending([]types.ResumeEntry{{
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
	schema := toolConfirmationResponseSchema()

	t.Run("valid approved", func(t *testing.T) {
		err := validatePayloadAgainstSchema(map[string]any{"approved": true}, schema)
		if err != nil {
			t.Fatalf("validatePayloadAgainstSchema() error = %v", err)
		}
	})

	t.Run("missing approved", func(t *testing.T) {
		err := validatePayloadAgainstSchema(map[string]any{}, schema)
		if err == nil {
			t.Fatal("expected error for missing approved")
		}
	})

	t.Run("editedArgs must be object", func(t *testing.T) {
		err := validatePayloadAgainstSchema(map[string]any{
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
		if err := validatePayloadAgainstSchema(map[string]any{"count": float64(3)}, schema); err != nil {
			t.Fatalf("whole float64 should pass integer check: %v", err)
		}
		if err := validatePayloadAgainstSchema(map[string]any{"count": float64(1.5)}, schema); err == nil {
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
		if err := validatePayloadAgainstSchema(map[string]any{"quarter": "Q1"}, schema); err != nil {
			t.Fatalf("valid enum: %v", err)
		}
		if err := validatePayloadAgainstSchema(map[string]any{"quarter": "Q5"}, schema); err == nil {
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
		if err := validatePayloadAgainstSchema(map[string]any{"flag": true}, schema); err != nil {
			t.Fatalf("bool true should match enum [true]: %v", err)
		}
		if err := validatePayloadAgainstSchema(map[string]any{"flag": "true"}, schema); err == nil {
			t.Fatal("expected error: string 'true' should not match boolean true enum")
		}
	})
}

func TestMergeInterruptCapabilities(t *testing.T) {
	caps := Capabilities{}
	MergeInterruptCapabilities(&caps)
	if caps.HumanInTheLoop == nil || caps.HumanInTheLoop.Interrupts == nil || !*caps.HumanInTheLoop.Interrupts {
		t.Fatal("expected interrupts capability to be true")
	}
	if caps.HumanInTheLoop.ApproveWithEdits == nil || !*caps.HumanInTheLoop.ApproveWithEdits {
		t.Fatal("expected approveWithEdits capability to be true")
	}
}

func TestPendingRecordsFromInterrupts_PreservesInvocationID(t *testing.T) {
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
	records := pendingRecordsFromInterrupts(interrupts)
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
	var roundTrip []pendingInterruptRecord
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTrip[0].InvocationID != "e-abc123" {
		t.Errorf("round-trip invocationId = %q, want e-abc123", roundTrip[0].InvocationID)
	}
}

func TestInvocationIDFromPendingInterrupts(t *testing.T) {
	pending := []pendingInterruptRecord{
		{ID: "confirm-1", InvocationID: "e-from-pending"},
		{ID: "confirm-2", InvocationID: ""},
	}
	t.Run("matches first resume entry with id", func(t *testing.T) {
		got := invocationIDFromPendingInterrupts(pending, []types.ResumeEntry{{
			InterruptID: "confirm-1",
			Status:      types.ResumeStatusResolved,
		}})
		if got != "e-from-pending" {
			t.Errorf("got %q, want e-from-pending", got)
		}
	})
	t.Run("skips empty pending invocation", func(t *testing.T) {
		got := invocationIDFromPendingInterrupts(pending, []types.ResumeEntry{{
			InterruptID: "confirm-2",
			Status:      types.ResumeStatusResolved,
		}})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestResolveInvocationIDForResume(t *testing.T) {
	pending := []pendingInterruptRecord{{ID: "confirm-1", InvocationID: "e-pending"}}
	entries := []types.ResumeEntry{{InterruptID: "confirm-1", Status: types.ResumeStatusResolved}}

	t.Run("prefers pending over client state", func(t *testing.T) {
		client := map[string]any{"adk": map[string]any{"invocationId": "e-client"}}
		got := resolveInvocationIDForResume(pending, entries, client, nil)
		if got != "e-pending" {
			t.Errorf("got %q, want e-pending", got)
		}
	})

	t.Run("client state when pending missing id", func(t *testing.T) {
		got := resolveInvocationIDForResume(nil, entries, map[string]any{
			"adk": map[string]any{"invocationId": "e-client"},
		}, nil)
		if got != "e-client" {
			t.Errorf("got %q, want e-client", got)
		}
	})

	t.Run("session events fallback", func(t *testing.T) {
		svc := session.InMemoryService()
		ctx := context.Background()
		createResp, err := svc.Create(ctx, &session.CreateRequest{
			AppName: "test-app", UserID: "u1", SessionID: "t1",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ev := session.NewEvent("inv0")
		ev.InvocationID = "e-from-session"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{
					ID:   "confirm-1",
					Name: toolconfirmation.FunctionCallName,
				},
			}},
		}
		if err := svc.AppendEvent(ctx, createResp.Session, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		got := resolveInvocationIDForResume(nil, entries, nil, createResp.Session)
		if got != "e-from-session" {
			t.Errorf("got %q, want e-from-session", got)
		}
	})
}

func TestToolConfirmationResponseSchemaIncludesEditedArgs(t *testing.T) {
	schema := toolConfirmationResponseSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing")
	}
	if _, ok := props["editedArgs"]; !ok {
		t.Fatal("editedArgs property missing from response schema")
	}
}
