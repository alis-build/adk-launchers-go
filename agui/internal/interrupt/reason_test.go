package interrupt

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/adk/v2/workflow"
)

func TestReasonConstants(t *testing.T) {
	// The wire values are fixed by the AG-UI reason taxonomy; a rename here is
	// a protocol break, not a refactor.
	// https://docs.ag-ui.com/concepts/interrupts#reason-taxonomy
	tests := []struct {
		got  string
		want string
	}{
		{ReasonToolCall, "tool_call"},
		{ReasonInputRequired, "input_required"},
		{ReasonConfirmation, "confirmation"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("reason constant = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestADKCallName(t *testing.T) {
	t.Run("input request routes to adk_request_input", func(t *testing.T) {
		rec := Record{ID: "i-1", Reason: ReasonInputRequired, CallName: workflow.WorkflowInputFunctionCallName}
		if got := ADKCallName(rec); got != workflow.WorkflowInputFunctionCallName {
			t.Errorf("ADKCallName() = %q, want %q", got, workflow.WorkflowInputFunctionCallName)
		}
	})

	t.Run("custom reason still routes by call name", func(t *testing.T) {
		// A host classifier may return any string; the reason must not be the
		// dispatch key or custom reasons resume into the wrong ADK call.
		rec := Record{ID: "i-1", Reason: "acme.approval", CallName: workflow.WorkflowInputFunctionCallName}
		if got := ADKCallName(rec); got != workflow.WorkflowInputFunctionCallName {
			t.Errorf("ADKCallName() = %q, want %q", got, workflow.WorkflowInputFunctionCallName)
		}
	})

	t.Run("tool confirmation routes to adk_request_confirmation", func(t *testing.T) {
		rec := Record{ID: "i-1", Reason: ReasonToolCall, CallName: toolconfirmation.FunctionCallName}
		if got := ADKCallName(rec); got != toolconfirmation.FunctionCallName {
			t.Errorf("ADKCallName() = %q, want %q", got, toolconfirmation.FunctionCallName)
		}
	})

	t.Run("empty call name defaults to tool confirmation", func(t *testing.T) {
		// Records persisted before CallName existed carry only a reason. They
		// were all tool confirmations, so an empty value must resume as one.
		rec := Record{ID: "i-1", Reason: ReasonToolCall}
		if got := ADKCallName(rec); got != toolconfirmation.FunctionCallName {
			t.Errorf("ADKCallName() = %q, want %q", got, toolconfirmation.FunctionCallName)
		}
	})
}

func TestRecordsFromInterruptsCarriesCallName(t *testing.T) {
	records := RecordsFromInterrupts([]types.Interrupt{{
		ID:     "i-1",
		Reason: ReasonInputRequired,
		Metadata: map[string]any{
			"adk": map[string]any{
				"invocationId": "inv-1",
				"callName":     workflow.WorkflowInputFunctionCallName,
			},
		},
	}})
	if len(records) != 1 {
		t.Fatalf("RecordsFromInterrupts() len = %d, want 1", len(records))
	}
	if records[0].CallName != workflow.WorkflowInputFunctionCallName {
		t.Errorf("Record.CallName = %q, want %q", records[0].CallName, workflow.WorkflowInputFunctionCallName)
	}
	if records[0].InvocationID != "inv-1" {
		t.Errorf("Record.InvocationID = %q, want %q", records[0].InvocationID, "inv-1")
	}
}

func TestDecodeRecordsRoundTripsCallName(t *testing.T) {
	// Records survive a JSON round trip through ADK session state, so CallName
	// must decode from the generic []any form the state layer hands back.
	raw := []any{
		map[string]any{
			"id":       "i-1",
			"reason":   ReasonInputRequired,
			"callName": workflow.WorkflowInputFunctionCallName,
		},
	}
	got, err := DecodeRecords(raw)
	if err != nil {
		t.Fatalf("DecodeRecords() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("DecodeRecords() len = %d, want 1", len(got))
	}
	if got[0].CallName != workflow.WorkflowInputFunctionCallName {
		t.Errorf("Record.CallName = %q, want %q", got[0].CallName, workflow.WorkflowInputFunctionCallName)
	}
}
