package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// inputRequestEvent builds the event shape ADK's workflow.NewRequestInputEvent
// produces: a typed RequestedInput plus a mirrored synthetic FunctionCall.
func inputRequestEvent(t *testing.T, invocationID string, req session.RequestInput) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = invocationID
	ev.RequestedInput = &req

	args := map[string]any{
		"interruptId": req.InterruptID,
		"message":     req.Message,
		"payload":     req.Payload,
	}
	if req.ResponseSchema != nil {
		args["responseSchema"] = req.ResponseSchema
	}
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				ID:   req.InterruptID,
				Name: workflow.WorkflowInputFunctionCallName,
				Args: args,
			},
		}},
	}
	return ev
}

// singleInterrupt runs one event and returns the sole interrupt it produced.
func singleInterrupt(t *testing.T, l *aguiLauncher, ev *session.Event) (types.Interrupt, []sseEvent) {
	t.Helper()
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	done, err := l.processEvent(e, ev, state, nil)
	if err != nil {
		t.Fatalf("processEvent() error = %v, want nil", err)
	}
	if !done {
		t.Fatal("processEvent() done = false, want true (input request finalizes the run)")
	}

	evts := parseSSEEvents(rec.Body.String())
	outcome := interruptsFromRunFinished(t, evts[len(evts)-1])
	if len(outcome.Interrupts) != 1 {
		t.Fatalf("len(outcome.Interrupts) = %d, want 1", len(outcome.Interrupts))
	}
	return outcome.Interrupts[0], evts
}

func TestProcessEvent_InputRequestInterrupt(t *testing.T) {
	l := newTestLauncher("test-app")
	req := session.RequestInput{
		InterruptID: "input-1",
		Message:     "How many copies?",
		Payload:     map[string]any{"document": "contract.pdf"},
		ResponseSchema: &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{"copies": {Type: "integer"}},
			Required:   []string{"copies"},
		},
	}

	intr, evts := singleInterrupt(t, l, inputRequestEvent(t, "inv-input", req))

	if intr.ID != "input-1" {
		t.Errorf("interrupt.ID = %q, want input-1", intr.ID)
	}
	if intr.Reason != "input_required" {
		t.Errorf("interrupt.Reason = %q, want input_required", intr.Reason)
	}
	if intr.Message != "How many copies?" {
		t.Errorf("interrupt.Message = %q, want 'How many copies?'", intr.Message)
	}
	// Per the AG-UI spec a non-tool reason has no bound tool call.
	if intr.ToolCallID != "" {
		t.Errorf("interrupt.ToolCallID = %q, want empty for a non-tool interrupt", intr.ToolCallID)
	}
	if intr.ResponseSchema == nil {
		t.Fatal("interrupt.ResponseSchema is nil, want the converted ADK schema")
	}
	if got := intr.ResponseSchema["type"]; got != "object" {
		t.Errorf("responseSchema.type = %v, want object", got)
	}
	props, _ := intr.ResponseSchema["properties"].(map[string]any)
	if _, ok := props["copies"]; !ok {
		t.Errorf("responseSchema.properties missing 'copies', got %v", props)
	}

	adkMeta, ok := intr.Metadata["adk"].(map[string]any)
	if !ok {
		t.Fatal("interrupt.Metadata['adk'] missing or wrong type")
	}
	if adkMeta["invocationId"] != "inv-input" {
		t.Errorf("adk.invocationId = %v, want inv-input", adkMeta["invocationId"])
	}
	if adkMeta["callName"] != workflow.WorkflowInputFunctionCallName {
		t.Errorf("adk.callName = %v, want %q", adkMeta["callName"], workflow.WorkflowInputFunctionCallName)
	}
	payload, ok := adkMeta["requestPayload"].(map[string]any)
	if !ok {
		t.Fatalf("adk.requestPayload missing or wrong type, got %T", adkMeta["requestPayload"])
	}
	if payload["document"] != "contract.pdf" {
		t.Errorf("adk.requestPayload.document = %v, want contract.pdf", payload["document"])
	}

	// An input request is not a tool proposal, so no tool lifecycle is emitted.
	for _, x := range evts {
		if x.Type == events.EventTypeToolCallStart {
			t.Errorf("unexpected TOOL_CALL_START for an input request: %v", x.Raw)
		}
	}
}

func TestProcessEvent_InputRequestBooleanSchemaIsConfirmation(t *testing.T) {
	l := newTestLauncher("test-app")
	req := session.RequestInput{
		InterruptID:    "input-2",
		Message:        "Publish now?",
		ResponseSchema: &jsonschema.Schema{Type: "boolean"},
	}

	intr, _ := singleInterrupt(t, l, inputRequestEvent(t, "inv-bool", req))
	if intr.Reason != "confirmation" {
		t.Errorf("interrupt.Reason = %q, want confirmation for a boolean schema", intr.Reason)
	}
}

func TestProcessEvent_InputRequestNoSchemaOmitsResponseSchema(t *testing.T) {
	l := newTestLauncher("test-app")
	req := session.RequestInput{InterruptID: "input-3", Message: "Anything to add?"}

	intr, _ := singleInterrupt(t, l, inputRequestEvent(t, "inv-none", req))
	if intr.ResponseSchema != nil {
		t.Errorf("interrupt.ResponseSchema = %v, want nil when ADK advertised none", intr.ResponseSchema)
	}
	if intr.Reason != "input_required" {
		t.Errorf("interrupt.Reason = %q, want input_required", intr.Reason)
	}
	adkMeta, _ := intr.Metadata["adk"].(map[string]any)
	if _, ok := adkMeta["requestPayload"]; ok {
		t.Errorf("adk.requestPayload present for a nil payload, want omitted")
	}
}

func TestProcessEvent_InputRequestArgsFallback(t *testing.T) {
	// Clients that do not model RequestedInput round-trip only the FunctionCall
	// args through session state, so the args alone must be enough.
	l := newTestLauncher("test-app")
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-args"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				ID:   "input-4",
				Name: workflow.WorkflowInputFunctionCallName,
				Args: map[string]any{
					"interruptId":    "input-4",
					"message":        "Pick a colour",
					"payload":        map[string]any{"swatch": "blue"},
					"responseSchema": map[string]any{"type": "string"},
				},
			},
		}},
	}

	intr, _ := singleInterrupt(t, l, ev)
	if intr.ID != "input-4" {
		t.Errorf("interrupt.ID = %q, want input-4", intr.ID)
	}
	if intr.Message != "Pick a colour" {
		t.Errorf("interrupt.Message = %q, want 'Pick a colour'", intr.Message)
	}
	if got := intr.ResponseSchema["type"]; got != "string" {
		t.Errorf("responseSchema.type = %v, want string", got)
	}
	adkMeta, _ := intr.Metadata["adk"].(map[string]any)
	payload, _ := adkMeta["requestPayload"].(map[string]any)
	if payload["swatch"] != "blue" {
		t.Errorf("adk.requestPayload.swatch = %v, want blue", payload["swatch"])
	}
}

func TestProcessEvent_InputRequestTypedFieldWinsOverArgs(t *testing.T) {
	// The typed field is written by the emitting node; args are the mirror. When
	// they disagree the typed field is authoritative.
	l := newTestLauncher("test-app")
	ev := inputRequestEvent(t, "inv-both", session.RequestInput{
		InterruptID: "input-5",
		Message:     "typed message",
	})
	ev.Content.Parts[0].FunctionCall.Args["message"] = "stale args message"

	intr, _ := singleInterrupt(t, l, ev)
	if intr.Message != "typed message" {
		t.Errorf("interrupt.Message = %q, want the typed RequestedInput message", intr.Message)
	}
}

func TestProcessEvent_InputRequestHostClassifierWins(t *testing.T) {
	l := newTestLauncher("test-app")
	WithInterruptReasonClassifier(func(req session.RequestInput) string {
		if req.InterruptID == "input-6" {
			return "acme.manager_approval"
		}
		return ""
	})(l.config)

	intr, _ := singleInterrupt(t, l, inputRequestEvent(t, "inv-custom", session.RequestInput{
		InterruptID:    "input-6",
		Message:        "Approve?",
		ResponseSchema: &jsonschema.Schema{Type: "boolean"},
	}))

	if intr.Reason != "acme.manager_approval" {
		t.Errorf("interrupt.Reason = %q, want the host classifier's value", intr.Reason)
	}
}

func TestProcessEvent_InputRequestArgsSchemaForms(t *testing.T) {
	// Without a typed RequestedInput the schema is read from args, where it may
	// arrive as a live *jsonschema.Schema, its value form, or the decoded JSON
	// object left by a session round-trip.
	tests := []struct {
		name     string
		schema   any
		wantType any
		wantNil  bool
	}{
		{
			name:     "pointer schema",
			schema:   &jsonschema.Schema{Type: "string"},
			wantType: "string",
		},
		{
			name:     "value schema",
			schema:   jsonschema.Schema{Type: "boolean"},
			wantType: "boolean",
		},
		{
			name:     "decoded object",
			schema:   map[string]any{"type": "integer"},
			wantType: "integer",
		},
		{
			// "type" must be a string or array of strings; 5 is neither, so the
			// schema is dropped and the interrupt still goes out.
			name:    "malformed object is dropped",
			schema:  map[string]any{"type": 5},
			wantNil: true,
		},
		{
			name:    "unexpected type is ignored",
			schema:  "not-a-schema",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLauncher("test-app")
			ev := session.NewEvent(t.Context(), "inv1")
			ev.InvocationID = "inv-schema"
			ev.Content = &genai.Content{
				Role: string(genai.RoleModel),
				Parts: []*genai.Part{{
					FunctionCall: &genai.FunctionCall{
						ID:   "input-schema",
						Name: workflow.WorkflowInputFunctionCallName,
						Args: map[string]any{
							"interruptId":    "input-schema",
							"message":        "Answer please",
							"responseSchema": tt.schema,
						},
					},
				}},
			}

			intr, _ := singleInterrupt(t, l, ev)
			if tt.wantNil {
				if intr.ResponseSchema != nil {
					t.Errorf("interrupt.ResponseSchema = %v, want nil", intr.ResponseSchema)
				}
				return
			}
			if got := intr.ResponseSchema["type"]; got != tt.wantType {
				t.Errorf("responseSchema.type = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func TestProcessEvent_InputRequestNilArgs(t *testing.T) {
	// A bare adk_request_input with neither a typed request nor args still has
	// to produce a resumable interrupt, keyed by the call id.
	l := newTestLauncher("test-app")
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-bare"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				ID:   "input-bare",
				Name: workflow.WorkflowInputFunctionCallName,
			},
		}},
	}

	intr, _ := singleInterrupt(t, l, ev)
	if intr.ID != "input-bare" {
		t.Errorf("interrupt.ID = %q, want input-bare (the call id)", intr.ID)
	}
	if intr.Reason != "input_required" {
		t.Errorf("interrupt.Reason = %q, want input_required", intr.Reason)
	}
}
