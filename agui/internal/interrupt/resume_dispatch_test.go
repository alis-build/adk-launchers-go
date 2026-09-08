package interrupt

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// partsByID indexes FunctionResponse parts by the interrupt id they answer.
func partsByID(t *testing.T, content *genai.Content) map[string]*genai.FunctionResponse {
	t.Helper()
	out := make(map[string]*genai.FunctionResponse, len(content.Parts))
	for _, part := range content.Parts {
		if part.FunctionResponse == nil {
			t.Fatalf("part %+v has no FunctionResponse", part)
		}
		out[part.FunctionResponse.ID] = part.FunctionResponse
	}
	return out
}

func TestEntriesToResumeContentDispatchesByCallName(t *testing.T) {
	t.Run("tool confirmation is unchanged", func(t *testing.T) {
		pending := []Record{{ID: "c-1", Reason: ReasonToolCall, CallName: toolconfirmation.FunctionCallName}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "c-1",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		fr := partsByID(t, content)["c-1"]
		if fr.Name != toolconfirmation.FunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, toolconfirmation.FunctionCallName)
		}
		if fr.Response["confirmed"] != true {
			t.Errorf("response.confirmed = %v, want true", fr.Response["confirmed"])
		}
	})

	t.Run("input request answers adk_request_input", func(t *testing.T) {
		pending := []Record{{ID: "i-1", Reason: ReasonInputRequired, CallName: workflow.WorkflowInputFunctionCallName}}
		payload := map[string]any{"copies": float64(3)}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-1",
			Status:      types.ResumeStatusResolved,
			Payload:     payload,
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		fr := partsByID(t, content)["i-1"]
		if fr.Name != workflow.WorkflowInputFunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, workflow.WorkflowInputFunctionCallName)
		}
		got, ok := fr.Response["response"].(map[string]any)
		if !ok {
			t.Fatalf("response.response = %T, want the resume payload", fr.Response["response"])
		}
		if got["copies"] != float64(3) {
			t.Errorf("response.response.copies = %v, want 3", got["copies"])
		}
	})

	t.Run("cancelled input request sends a nil response", func(t *testing.T) {
		// Mirrors cancelled tool confirmations mapping to confirmed:false
		// rather than erroring. Interpreting a withheld answer is the node's
		// business, not the launcher's.
		pending := []Record{{ID: "i-2", Reason: ReasonInputRequired, CallName: workflow.WorkflowInputFunctionCallName}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-2",
			Status:      types.ResumeStatusCancelled,
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		fr := partsByID(t, content)["i-2"]
		if fr.Name != workflow.WorkflowInputFunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, workflow.WorkflowInputFunctionCallName)
		}
		resp, ok := fr.Response["response"]
		if !ok {
			t.Fatal("response key missing, want present and nil")
		}
		if resp != nil {
			t.Errorf("response.response = %v, want nil", resp)
		}
	})

	t.Run("cancelled tool confirmation is unchanged", func(t *testing.T) {
		pending := []Record{{ID: "c-2", Reason: ReasonToolCall, CallName: toolconfirmation.FunctionCallName}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "c-2",
			Status:      types.ResumeStatusCancelled,
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if got := partsByID(t, content)["c-2"].Response["confirmed"]; got != false {
			t.Errorf("response.confirmed = %v, want false", got)
		}
	})

	t.Run("mixed pending set answers each with its own call", func(t *testing.T) {
		pending := []Record{
			{ID: "c-3", Reason: ReasonToolCall, CallName: toolconfirmation.FunctionCallName},
			{ID: "i-3", Reason: ReasonInputRequired, CallName: workflow.WorkflowInputFunctionCallName},
		}
		content, err := EntriesToResumeContent([]types.ResumeEntry{
			{InterruptID: "c-3", Status: types.ResumeStatusResolved, Payload: map[string]any{"approved": false}},
			{InterruptID: "i-3", Status: types.ResumeStatusResolved, Payload: map[string]any{"note": "ok"}},
		}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if len(content.Parts) != 2 {
			t.Fatalf("len(content.Parts) = %d, want 2", len(content.Parts))
		}
		// Parts keep entry order so the ADK side sees answers in the order the
		// client sent them.
		if content.Parts[0].FunctionResponse.ID != "c-3" || content.Parts[1].FunctionResponse.ID != "i-3" {
			t.Errorf("part order = %q, %q; want c-3, i-3",
				content.Parts[0].FunctionResponse.ID, content.Parts[1].FunctionResponse.ID)
		}
		byID := partsByID(t, content)
		if byID["c-3"].Name != toolconfirmation.FunctionCallName {
			t.Errorf("c-3 answered %q, want %q", byID["c-3"].Name, toolconfirmation.FunctionCallName)
		}
		if byID["i-3"].Name != workflow.WorkflowInputFunctionCallName {
			t.Errorf("i-3 answered %q, want %q", byID["i-3"].Name, workflow.WorkflowInputFunctionCallName)
		}
	})

	t.Run("custom reason still routes by call name", func(t *testing.T) {
		// A host classifier can return any reason string. Dispatch must follow
		// CallName, or a custom reason resumes into the wrong ADK call.
		pending := []Record{{ID: "i-4", Reason: "acme.manager_approval", CallName: workflow.WorkflowInputFunctionCallName}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-4",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if got := partsByID(t, content)["i-4"].Name; got != workflow.WorkflowInputFunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", got, workflow.WorkflowInputFunctionCallName)
		}
	})

	t.Run("record without a call name resumes as a tool confirmation", func(t *testing.T) {
		// Records persisted before CallName existed were all tool confirmations.
		pending := []Record{{ID: "c-4", Reason: ReasonToolCall}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "c-4",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if got := partsByID(t, content)["c-4"].Name; got != toolconfirmation.FunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", got, toolconfirmation.FunctionCallName)
		}
	})

	t.Run("entry with no pending record resumes as a tool confirmation", func(t *testing.T) {
		// Validation rejects unknown ids before this point; the fallback exists
		// so an unvalidated call cannot silently produce an unnamed response.
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "c-5",
			Status:      types.ResumeStatusResolved,
			Payload:     map[string]any{"approved": true},
		}}, nil)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if got := partsByID(t, content)["c-5"].Name; got != toolconfirmation.FunctionCallName {
			t.Errorf("FunctionResponse.Name = %q, want %q", got, toolconfirmation.FunctionCallName)
		}
	})

	t.Run("empty entries is an error", func(t *testing.T) {
		if _, err := EntriesToResumeContent(nil, nil); err == nil {
			t.Error("EntriesToResumeContent(nil, nil) error = nil, want an error")
		}
	})

	t.Run("entry without an interrupt id is an error", func(t *testing.T) {
		_, err := EntriesToResumeContent([]types.ResumeEntry{{Status: types.ResumeStatusResolved}}, nil)
		if err == nil {
			t.Error("EntriesToResumeContent() error = nil, want an error for a missing interruptId")
		}
	})
}

func TestInputResumeEntryErrors(t *testing.T) {
	pending := []Record{{ID: "i-9", Reason: ReasonInputRequired, CallName: workflow.WorkflowInputFunctionCallName}}

	t.Run("resolved without a payload is an error", func(t *testing.T) {
		// The node asked a question; resolving it with nothing to say is a
		// client bug, not a withheld answer. Withholding is what cancelled is.
		_, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-9",
			Status:      types.ResumeStatusResolved,
		}}, pending)
		if err == nil {
			t.Error("EntriesToResumeContent() error = nil, want an error for a missing payload")
		}
	})

	t.Run("unknown status is an error", func(t *testing.T) {
		_, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-9",
			Status:      types.ResumeStatus("wat"),
		}}, pending)
		if err == nil {
			t.Error("EntriesToResumeContent() error = nil, want an error for an unsupported status")
		}
	})

	t.Run("scalar payload passes through", func(t *testing.T) {
		// A node may advertise a scalar schema, so the payload is not forced
		// into an object the way a tool confirmation's is.
		content, err := EntriesToResumeContent([]types.ResumeEntry{{
			InterruptID: "i-9",
			Status:      types.ResumeStatusResolved,
			Payload:     "blue",
		}}, pending)
		if err != nil {
			t.Fatalf("EntriesToResumeContent() error = %v", err)
		}
		if got := partsByID(t, content)["i-9"].Response["response"]; got != "blue" {
			t.Errorf("response.response = %v, want blue", got)
		}
	})
}
