package interrupt

import (
	"strings"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/adk/v2/workflow"
)

// TestValidateResumeAgainstPendingIsReasonAware covers spec FR5: the
// payload.approved requirement belongs to tool_call alone.
//
// Before this, every resolved resume was run through the tool-confirmation
// schema, so answering an input request rejected the run with "payload missing
// required field approved" no matter what the node had actually asked for.
func TestValidateResumeAgainstPendingIsReasonAware(t *testing.T) {
	now := time.Now()

	t.Run("input request does not require approved", func(t *testing.T) {
		pending := []Record{{
			ID:       "i-1",
			Reason:   ReasonInputRequired,
			CallName: workflow.WorkflowInputFunctionCallName,
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "i-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"copies": 3},
		}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err != nil {
			t.Errorf("ValidateResumeAgainstPending() error = %v, want nil", err)
		}
	})

	t.Run("tool confirmation still requires approved", func(t *testing.T) {
		pending := []Record{{
			ID:             "c-1",
			Reason:         ReasonToolCall,
			CallName:       toolconfirmation.FunctionCallName,
			ResponseSchema: ToolConfirmationResponseSchema(),
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "c-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"note": "looks fine"},
		}}
		err := ValidateResumeAgainstPending(entries, pending, now)
		if err == nil {
			t.Fatal("ValidateResumeAgainstPending() error = nil, want a missing-approved error")
		}
		if !strings.Contains(err.Error(), "approved") {
			t.Errorf("error = %v, want it to name the missing approved field", err)
		}
	})

	t.Run("input payload is checked against its advertised schema", func(t *testing.T) {
		pending := []Record{{
			ID:       "i-2",
			Reason:   ReasonInputRequired,
			CallName: workflow.WorkflowInputFunctionCallName,
			ResponseSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"copies": map[string]any{"type": "integer"}},
				"required":   []any{"copies"},
			},
		}}

		ok := []types.ResumeEntry{{
			InterruptID: "i-2",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"copies": 3},
		}}
		if err := ValidateResumeAgainstPending(ok, pending, now); err != nil {
			t.Errorf("valid payload rejected: %v", err)
		}

		missing := []types.ResumeEntry{{
			InterruptID: "i-2",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"other": 1},
		}}
		if err := ValidateResumeAgainstPending(missing, pending, now); err == nil {
			t.Error("payload missing a required field was accepted, want an error")
		}

		wrongType := []types.ResumeEntry{{
			InterruptID: "i-2",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"copies": "three"},
		}}
		if err := ValidateResumeAgainstPending(wrongType, pending, now); err == nil {
			t.Error("payload with a wrong-typed field was accepted, want an error")
		}
	})

	t.Run("input request without a schema accepts any object", func(t *testing.T) {
		pending := []Record{{
			ID:       "i-3",
			Reason:   ReasonInputRequired,
			CallName: workflow.WorkflowInputFunctionCallName,
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "i-3",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"anything": []any{1, 2, 3}},
		}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err != nil {
			t.Errorf("ValidateResumeAgainstPending() error = %v, want nil", err)
		}
	})

	t.Run("input request accepts a scalar payload", func(t *testing.T) {
		// A node may advertise {"type":"string"}, so the answer is not an
		// object. The tool-confirmation path demands one; this must not.
		pending := []Record{{
			ID:             "i-4",
			Reason:         ReasonInputRequired,
			CallName:       workflow.WorkflowInputFunctionCallName,
			ResponseSchema: map[string]any{"type": "string"},
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "i-4",
			Status:      types.ResumeStatusResolved,
			Payload:     "blue",
		}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err != nil {
			t.Errorf("ValidateResumeAgainstPending() error = %v, want nil for a scalar answer", err)
		}
	})

	t.Run("tool confirmation rejects a non-object payload", func(t *testing.T) {
		// The launcher reads payload.approved to build the ADK response, so a
		// scalar has nowhere to carry the decision. Only input requests are
		// allowed a non-object answer.
		pending := []Record{{
			ID:       "c-9",
			Reason:   ReasonToolCall,
			CallName: toolconfirmation.FunctionCallName,
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "c-9",
			Status:      types.ResumeStatusResolved,
			Payload:     "yes",
		}}
		err := ValidateResumeAgainstPending(entries, pending, now)
		if err == nil {
			t.Fatal("scalar payload on a tool confirmation was accepted, want an error")
		}
		if !strings.Contains(err.Error(), "JSON object") {
			t.Errorf("error = %v, want it to say the payload must be a JSON object", err)
		}
	})

	t.Run("scalar payload is checked against its scalar schema", func(t *testing.T) {
		pending := []Record{{
			ID:             "i-7",
			Reason:         ReasonInputRequired,
			CallName:       workflow.WorkflowInputFunctionCallName,
			ResponseSchema: map[string]any{"type": "string"},
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "i-7",
			Status:      types.ResumeStatusResolved,
			Payload:     42,
		}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err == nil {
			t.Error("a number answering a string schema was accepted, want an error")
		}
	})

	t.Run("input request still requires a payload when resolved", func(t *testing.T) {
		pending := []Record{{
			ID:       "i-5",
			Reason:   ReasonInputRequired,
			CallName: workflow.WorkflowInputFunctionCallName,
		}}
		entries := []types.ResumeEntry{{InterruptID: "i-5", Status: types.ResumeStatusResolved}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err == nil {
			t.Error("resolved resume with no payload was accepted, want an error")
		}
	})

	t.Run("shared rules still apply to input requests", func(t *testing.T) {
		pending := []Record{{
			ID:        "i-6",
			Reason:    ReasonInputRequired,
			CallName:  workflow.WorkflowInputFunctionCallName,
			ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339),
		}}
		entries := []types.ResumeEntry{{
			InterruptID: "i-6",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"any": true},
		}}
		if err := ValidateResumeAgainstPending(entries, pending, now); err == nil {
			t.Error("expired input request was accepted, want an expiry error")
		}
	})
}
