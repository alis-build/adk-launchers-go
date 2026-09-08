package agui

import (
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// TestGraphInterruptMetadata covers FR2: an interrupt raised inside a node
// carries where it came from and where the graph would have gone.
//
// Interrupt metadata is the only place per-event provenance can travel in the
// pinned Go SDK: types.Interrupt.Metadata exists, event-level metadata does not.
// Everything else in this track rides step names.
func TestGraphInterruptMetadata(t *testing.T) {
	t.Run("tool confirmation carries node path and routes", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-graph"
		ev.Author = "reviewer"
		ev.NodeInfo = &session.NodeInfo{Path: "review/approve@run-1"}
		ev.Routes = []string{"publish", "reject"}
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
		}

		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		intr := interruptsFromRunFinished(t, evts[len(evts)-1]).Interrupts[0]
		adkMeta, ok := intr.Metadata["adk"].(map[string]any)
		if !ok {
			t.Fatal("interrupt.Metadata['adk'] missing")
		}
		if adkMeta["nodePath"] != "review/approve@run-1" {
			t.Errorf("adk.nodePath = %v, want review/approve@run-1", adkMeta["nodePath"])
		}
		routes, ok := adkMeta["routes"].([]any)
		if !ok {
			t.Fatalf("adk.routes = %T, want a list", adkMeta["routes"])
		}
		if len(routes) != 2 || routes[0] != "publish" || routes[1] != "reject" {
			t.Errorf("adk.routes = %v, want [publish reject]", routes)
		}
	})

	t.Run("input request carries node path and routes", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-graph"
		ev.Author = "reviewer"
		ev.NodeInfo = &session.NodeInfo{Path: "review"}
		ev.Routes = []string{"publish"}
		ev.RequestedInput = &session.RequestInput{InterruptID: "input-1", Message: "Approve?"}
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID:   "input-1",
				Name: workflow.WorkflowInputFunctionCallName,
				Args: map[string]any{"interruptId": "input-1", "message": "Approve?"},
			}}},
		}

		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		intr := interruptsFromRunFinished(t, evts[len(evts)-1]).Interrupts[0]
		adkMeta, _ := intr.Metadata["adk"].(map[string]any)
		if adkMeta["nodePath"] != "review" {
			t.Errorf("adk.nodePath = %v, want review", adkMeta["nodePath"])
		}
		if routes, _ := adkMeta["routes"].([]any); len(routes) != 1 || routes[0] != "publish" {
			t.Errorf("adk.routes = %v, want [publish]", adkMeta["routes"])
		}
		// The interrupt keeps everything aguihitl put there.
		if adkMeta["callName"] != workflow.WorkflowInputFunctionCallName {
			t.Errorf("adk.callName = %v, want it preserved", adkMeta["callName"])
		}
	})

	t.Run("empty path and routes are omitted, not written as blanks", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-graph"
		ev.Author = "reviewer"
		ev.NodeInfo = &session.NodeInfo{MessageAsOutput: true}
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
		}

		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		intr := interruptsFromRunFinished(t, evts[len(evts)-1]).Interrupts[0]
		adkMeta, _ := intr.Metadata["adk"].(map[string]any)
		if v, present := adkMeta["nodePath"]; present {
			t.Errorf("adk.nodePath present as %v, want omitted for an empty path", v)
		}
		if v, present := adkMeta["routes"]; present {
			t.Errorf("adk.routes present as %v, want omitted when there are none", v)
		}
	})

	t.Run("a non-workflow interrupt gains nothing", func(t *testing.T) {
		// The tool_call wire is pinned byte-for-byte by aguihitl's baseline;
		// this asserts the same contract from the graph side.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-plain"
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
		}

		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		intr := interruptsFromRunFinished(t, evts[len(evts)-1]).Interrupts[0]
		adkMeta, _ := intr.Metadata["adk"].(map[string]any)
		for _, key := range []string{"nodePath", "routes"} {
			if v, present := adkMeta[key]; present {
				t.Errorf("adk.%s present as %v on a non-workflow interrupt", key, v)
			}
		}
	})
}
