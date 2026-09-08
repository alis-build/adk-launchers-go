package agui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// confirmationPart builds one adk_request_confirmation FunctionCall part
// wrapping an original tool call.
func confirmationPart(confirmID, hint, origID, origName string) *genai.Part {
	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   confirmID,
			Name: toolconfirmation.FunctionCallName,
			Args: map[string]any{
				"toolConfirmation": map[string]any{"hint": hint},
				"originalFunctionCall": map[string]any{
					"ID":   origID,
					"Name": origName,
					"Args": map[string]any{},
				},
			},
		},
	}
}

// interruptsFromRunFinished decodes the interrupt outcome of a RUN_FINISHED event.
func interruptsFromRunFinished(t *testing.T, ev sseEvent) events.RunFinishedOutcome {
	t.Helper()
	outcomeBytes, err := json.Marshal(ev.Raw["outcome"])
	if err != nil {
		t.Fatalf("marshal outcome: %v", err)
	}
	var outcome events.RunFinishedOutcome
	if err := json.Unmarshal(outcomeBytes, &outcome); err != nil {
		t.Fatalf("unmarshal outcome: %v", err)
	}
	return outcome
}

// TestProcessEvent_MultipleInterruptsInOneEvent covers spec FR3: every
// interrupt-producing call in an event must reach the client.
//
// Before this, ProcessEvent returned on the first one, so an agent proposing
// three tools in a single turn left two of them silently unanswerable: the
// client could not resume them, and resume validation would then reject the
// run for missing interrupt ids it was never told about.
func TestProcessEvent_MultipleInterruptsInOneEvent(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-multi"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{
			confirmationPart("confirm-1", "Send the email?", "orig-1", "send_email"),
			confirmationPart("confirm-2", "Charge the card?", "orig-2", "charge_card"),
			confirmationPart("confirm-3", "Delete the file?", "orig-3", "delete_file"),
		},
	}

	done, err := l.processEvent(e, ev, state, nil)
	if err != nil {
		t.Fatalf("processEvent() error = %v, want nil", err)
	}
	if !done {
		t.Fatal("processEvent() done = false, want true")
	}

	evts := parseSSEEvents(rec.Body.String())

	// Exactly one terminal event: the AG-UI protocol allows one per run.
	var runFinished []sseEvent
	for _, x := range evts {
		if x.Type == events.EventTypeRunFinished {
			runFinished = append(runFinished, x)
		}
	}
	if len(runFinished) != 1 {
		t.Fatalf("got %d RUN_FINISHED events, want exactly 1", len(runFinished))
	}

	outcome := interruptsFromRunFinished(t, runFinished[0])
	if outcome.Type != events.RunFinishedOutcomeTypeInterrupt {
		t.Errorf("outcome.Type = %v, want interrupt", outcome.Type)
	}
	if len(outcome.Interrupts) != 3 {
		t.Fatalf("len(outcome.Interrupts) = %d, want 3", len(outcome.Interrupts))
	}

	// Interrupts appear in part order, so a client can line them up with the
	// tool proposals it just saw.
	wantIDs := []string{"confirm-1", "confirm-2", "confirm-3"}
	wantToolCallIDs := []string{"orig-1", "orig-2", "orig-3"}
	wantMessages := []string{"Send the email?", "Charge the card?", "Delete the file?"}
	for i, intr := range outcome.Interrupts {
		if intr.ID != wantIDs[i] {
			t.Errorf("interrupt[%d].ID = %q, want %q", i, intr.ID, wantIDs[i])
		}
		if intr.ToolCallID != wantToolCallIDs[i] {
			t.Errorf("interrupt[%d].ToolCallID = %q, want %q", i, intr.ToolCallID, wantToolCallIDs[i])
		}
		if intr.Message != wantMessages[i] {
			t.Errorf("interrupt[%d].Message = %q, want %q", i, intr.Message, wantMessages[i])
		}
		if intr.Reason != "tool_call" {
			t.Errorf("interrupt[%d].Reason = %q, want tool_call", i, intr.Reason)
		}
	}

	// Each interrupt keeps its own tool lifecycle, and every lifecycle precedes
	// the single terminal event.
	for _, origID := range wantToolCallIDs {
		if n := countToolCallStarts(evts, origID); n != 1 {
			t.Errorf("TOOL_CALL_START count for %q = %d, want 1", origID, n)
		}
	}
	lastStart := -1
	for i, x := range evts {
		if x.Type == events.EventTypeToolCallEnd {
			lastStart = i
		}
	}
	runFinishedIdx := -1
	for i, x := range evts {
		if x.Type == events.EventTypeRunFinished {
			runFinishedIdx = i
		}
	}
	if lastStart > runFinishedIdx {
		t.Errorf("a TOOL_CALL_END at index %d follows RUN_FINISHED at %d; the terminal event must be last", lastStart, runFinishedIdx)
	}

	// State records all three so interrupt persistence can validate a resume
	// that answers every one of them.
	if len(state.EmittedInterrupts) != 3 {
		t.Errorf("len(state.EmittedInterrupts) = %d, want 3", len(state.EmittedInterrupts))
	}
}

// TestProcessEvent_SingleInterruptUnchanged guards the common case against the
// multi-interrupt change: one confirmation still yields one interrupt.
func TestProcessEvent_SingleInterruptUnchanged(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-single"
	ev.Content = &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: []*genai.Part{confirmationPart("confirm-1", "Send it?", "orig-1", "send_email")},
	}

	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v, want nil", err)
	}

	evts := parseSSEEvents(rec.Body.String())
	outcome := interruptsFromRunFinished(t, evts[len(evts)-1])
	if len(outcome.Interrupts) != 1 {
		t.Fatalf("len(outcome.Interrupts) = %d, want 1", len(outcome.Interrupts))
	}
	if outcome.Interrupts[0].ID != "confirm-1" {
		t.Errorf("interrupt.ID = %q, want confirm-1", outcome.Interrupts[0].ID)
	}
}

// TestProcessEvent_MultipleInterrupts_SnapshotsEmittedOnce pins the decision
// that snapshots describe the event, not each pause.
//
// Snapshots moved out of the per-interrupt path when interrupt collection
// landed. Emitting them once per interrupt would repeat identical STATE_SNAPSHOT
// and MESSAGES_SNAPSHOT payloads for every proposal in the turn, which a client
// would apply redundantly.
func TestProcessEvent_MultipleInterrupts_SnapshotsEmittedOnce(t *testing.T) {
	svc := session.InMemoryService()
	ctx := context.Background()
	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "t1",
		State:     map[string]any{"count": 1},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	seed := session.NewEvent(t.Context(), "inv0")
	seed.Content = genai.NewContentFromText("Hello", genai.RoleUser)
	if err := svc.AppendEvent(ctx, createResp.Session, seed); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	l := newTestLauncher("test-app", svc)
	e, rec := newTestEmitter()
	state := &streamState{
		RunID: "r1", ThreadID: "t1", UserID: "user-1", RunCtx: ctx,
		RootAppName: "test-app",
		ReqState:    map[string]any{"ui": "panel"},
	}

	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-multi-snap"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{
			confirmationPart("confirm-1", "Send the email?", "orig-1", "send_email"),
			confirmationPart("confirm-2", "Charge the card?", "orig-2", "charge_card"),
			confirmationPart("confirm-3", "Delete the file?", "orig-3", "delete_file"),
		},
	}

	done, err := l.processEvent(e, ev, state, nil)
	if err != nil || !done {
		t.Fatalf("processEvent() done=%v err=%v", done, err)
	}

	var stateSnaps, msgSnaps int
	for _, x := range parseSSEEvents(rec.Body.String()) {
		switch x.Type {
		case events.EventTypeStateSnapshot:
			stateSnaps++
		case events.EventTypeMessagesSnapshot:
			msgSnaps++
		}
	}
	if stateSnaps != 1 {
		t.Errorf("got %d STATE_SNAPSHOT events for 3 interrupts, want 1", stateSnaps)
	}
	if msgSnaps != 1 {
		t.Errorf("got %d MESSAGES_SNAPSHOT events for 3 interrupts, want 1", msgSnaps)
	}
}
