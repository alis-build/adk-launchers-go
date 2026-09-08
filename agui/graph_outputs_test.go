package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// nodeOutputsFromDeltas replays every STATE_DELTA in the stream and returns the
// resulting _adk.nodeOutputs map, so tests assert on what a client would hold
// rather than on patch mechanics.
func nodeOutputsFromDeltas(t *testing.T, evts []sseEvent) map[string]any {
	t.Helper()
	var adk map[string]any
	for _, ev := range evts {
		if ev.Type != events.EventTypeStateDelta {
			continue
		}
		ops, _ := ev.Raw["delta"].([]any)
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			if op["path"] != "/_adk" {
				continue
			}
			adk, _ = op["value"].(map[string]any)
		}
	}
	if adk == nil {
		return nil
	}
	out, _ := adk["nodeOutputs"].(map[string]any)
	return out
}

func outputEvent(t *testing.T, author, path string, output any) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-graph"
	ev.Author = author
	ev.NodeInfo = &session.NodeInfo{Path: path}
	ev.Output = output
	return ev
}

func TestGraphNodeOutputs(t *testing.T) {
	t.Run("a node output is recorded under its path", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "reviewer", "review", map[string]any{"verdict": "ok"})
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String()))
		entry, _ := got["review"].(map[string]any)
		if entry["verdict"] != "ok" {
			t.Errorf("_adk.nodeOutputs[review] = %v, want verdict ok", got["review"])
		}
	})

	t.Run("outputs from several nodes accumulate", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		for _, ev := range []*session.Event{
			outputEvent(t, "reviewer", "review", "approved"),
			outputEvent(t, "publisher", "publish", "live"),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String()))
		if got["review"] != "approved" || got["publish"] != "live" {
			t.Errorf("_adk.nodeOutputs = %v, want both nodes recorded", got)
		}
	})

	t.Run("OutputFor fans one output across a delegation chain", func(t *testing.T) {
		// One event stands in for every delegating ancestor, so each records
		// the same result rather than each level re-emitting it.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "worker", "outer/inner@run-1", "done")
		ev.NodeInfo.OutputFor = []string{"outer", "outer/inner@run-1"}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String()))
		if got["outer"] != "done" || got["outer/inner@run-1"] != "done" {
			t.Errorf("_adk.nodeOutputs = %v, want the output under both paths", got)
		}
	})

	t.Run("MessageAsOutput records the model text", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "reviewer", "review", nil)
		ev.NodeInfo.MessageAsOutput = true
		ev.Content = genai.NewContentFromText("looks good to me", genai.RoleModel)
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String()))
		if got["review"] != "looks good to me" {
			t.Errorf("_adk.nodeOutputs[review] = %v, want the model text", got["review"])
		}
	})

	t.Run("a pathless node is keyed by its agent name", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "reviewer", "", "approved")
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String()))
		if got["reviewer"] != "approved" {
			t.Errorf("_adk.nodeOutputs = %v, want it keyed by the agent name", got)
		}
	})

	t.Run("a node with no output emits no _adk delta", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "reviewer", "review", nil)
		ev.Content = genai.NewContentFromText("thinking out loud", genai.RoleModel)
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		if got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String())); got != nil {
			t.Errorf("_adk.nodeOutputs = %v, want no delta at all", got)
		}
	})

	t.Run("an output with nothing to key it on is dropped", func(t *testing.T) {
		// No path, no OutputFor, no author: recording it would put the result
		// under an empty key, which no client could attribute to anything.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "", "", "orphaned")
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		if got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String())); got != nil {
			t.Errorf("_adk.nodeOutputs = %v, want the unattributable output dropped", got)
		}
	})

	t.Run("a non-workflow event never writes _adk", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "test-app"
		ev.Output = map[string]any{"stray": true}
		ev.Actions.StateDelta["count"] = 1
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		if got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String())); got != nil {
			t.Errorf("_adk.nodeOutputs = %v, want nothing for a non-workflow event", got)
		}
	})
}
