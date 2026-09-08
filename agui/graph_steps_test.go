package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// stepNames returns the STEP_STARTED / STEP_FINISHED sequence as
// "start:<name>" and "finish:<name>" entries, in emission order.
func stepNames(evts []sseEvent) []string {
	var out []string
	for _, ev := range evts {
		switch ev.Type {
		case events.EventTypeStepStarted:
			out = append(out, "start:"+ev.str("stepName"))
		case events.EventTypeStepFinished:
			out = append(out, "finish:"+ev.str("stepName"))
		}
	}
	return out
}

// nodeEvent builds a workflow event emitted by the node at path.
func nodeEvent(t *testing.T, author, path, text string) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-graph"
	ev.Author = author
	ev.NodeInfo = &session.NodeInfo{Path: path}
	if text != "" {
		ev.Content = genai.NewContentFromText(text, genai.RoleModel)
		ev.Partial = true
	}
	return ev
}

func TestGraphSteps(t *testing.T) {
	t.Run("a node path names its step", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		want := []string{"start:review"}
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("steps = %v, want %v", got, want)
		}
	})

	t.Run("a path change closes the previous step and opens the next", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		for _, ev := range []*session.Event{
			nodeEvent(t, "reviewer", "review", "checking"),
			nodeEvent(t, "publisher", "publish", "shipping"),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		want := []string{"start:review", "finish:review", "start:publish"}
		if len(got) != len(want) {
			t.Fatalf("steps = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("steps = %v, want %v", got, want)
				break
			}
		}
	})

	t.Run("consecutive events from one node do not reopen its step", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		for i := 0; i < 3; i++ {
			if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review", "tick"), state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		if len(got) != 1 || got[0] != "start:review" {
			t.Errorf("steps = %v, want one start:review", got)
		}
	})

	t.Run("a node with no path falls back to its agent name", func(t *testing.T) {
		// Top-level static nodes carry an empty path. ADK only collapses an
		// all-zero NodeInfo to nil when decoding JSON, so an in-process event
		// reaches us with a real NodeInfo and no path.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := nodeEvent(t, "reviewer", "", "checking")
		ev.NodeInfo.MessageAsOutput = true
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		if len(got) != 1 || got[0] != "start:reviewer" {
			t.Errorf("steps = %v, want one start:reviewer", got)
		}
	})

	t.Run("a root-named workflow node still gets a step", func(t *testing.T) {
		// The root agent gets no step, but a workflow node is a real graph
		// activation even when its agent shares the root's name; suppressing it
		// would hide a node from the client.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, nodeEvent(t, "test-app", "root-node", "working"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		if len(got) != 1 || got[0] != "start:root-node" {
			t.Errorf("steps = %v, want one start:root-node", got)
		}
	})

	t.Run("an open step is closed before an interrupt terminal event", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		intrEv := nodeEvent(t, "reviewer", "review", "")
		intrEv.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
		}
		done, err := l.processEvent(e, intrEv, state, nil)
		if err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		if !done {
			t.Fatal("processEvent() done = false, want true")
		}

		evts := parseSSEEvents(rec.Body.String())
		var finishIdx, terminalIdx = -1, -1
		for i, x := range evts {
			switch x.Type {
			case events.EventTypeStepFinished:
				finishIdx = i
			case events.EventTypeRunFinished:
				terminalIdx = i
			}
		}
		if finishIdx == -1 {
			t.Fatal("no STEP_FINISHED emitted; the open step leaked past the terminal event")
		}
		if finishIdx > terminalIdx {
			t.Errorf("STEP_FINISHED at %d follows RUN_FINISHED at %d", finishIdx, terminalIdx)
		}
		if state.CurrentStepName != "" {
			t.Errorf("state left step %q open after finalization", state.CurrentStepName)
		}
	})
}
