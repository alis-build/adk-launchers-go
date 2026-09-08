package agui

import (
	"context"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"google.golang.org/adk/v2/session"
)

// TestClientStateCannotPlantInternalKeys covers the inbound half of acceptance
// criterion 7: _adk must be absent from state written back to the ADK session.
//
// Snapshots send _adk to clients, so a client echoing state back is the normal
// case rather than an attack. On a thread's first request that state becomes the
// session's initial state, which is permanent and visible to the agent, so the
// filter has to happen before the session is created.
func TestClientStateCannotPlantInternalKeys(t *testing.T) {
	svc := session.InMemoryService()
	l := newTestLauncher("test-app", svc)
	ctx := context.Background()

	reqState := map[string]any{
		"ui":                       "panel",
		stream.NodeOutputsStateKey: map[string]any{"nodeOutputs": map[string]any{"spoofed": true}},
		"_agui_pending":            "junk",
	}

	sess, err := l.ensureSessionForSnapshot(ctx, "test-app", "user-1", "new-thread", withoutInternalKeys(reqState))
	if err != nil {
		t.Fatalf("ensureSessionForSnapshot() error = %v", err)
	}
	if sess == nil {
		t.Fatal("ensureSessionForSnapshot() returned no session")
	}

	got := map[string]any{}
	for k, v := range sess.State().All() {
		got[k] = v
	}
	for _, key := range []string{stream.NodeOutputsStateKey, "_agui_pending"} {
		if v, present := got[key]; present {
			t.Errorf("client-supplied %q reached ADK session state as %v", key, v)
		}
	}
	if got["ui"] != "panel" {
		t.Errorf("host state was dropped: %v", got)
	}
}

func TestWithoutInternalKeys(t *testing.T) {
	t.Run("nil in, nil out", func(t *testing.T) {
		if got := withoutInternalKeys(nil); got != nil {
			t.Errorf("withoutInternalKeys(nil) = %v, want nil", got)
		}
	})

	t.Run("returns the same map when nothing is internal", func(t *testing.T) {
		// Avoids allocating a copy on the common path, where a client sends
		// only its own state.
		in := map[string]any{"ui": "panel"}
		got := withoutInternalKeys(in)
		if len(got) != 1 || got["ui"] != "panel" {
			t.Errorf("withoutInternalKeys() = %v, want the input preserved", got)
		}
	})

	t.Run("does not mutate the caller's map", func(t *testing.T) {
		in := map[string]any{"ui": "panel", stream.NodeOutputsStateKey: "junk"}
		withoutInternalKeys(in)
		if _, present := in[stream.NodeOutputsStateKey]; !present {
			t.Error("withoutInternalKeys mutated its argument; the caller still owns that map")
		}
	})
}

// TestEmittedNodeOutputsAreSnapshots proves an emitted state delta keeps the
// values it was emitted with.
//
// The launcher hands emitted events to hosts through AfterEventCallback, so an
// event that mutates after emission shows a host history it never emitted.
func TestEmittedNodeOutputsAreSnapshots(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	collected := newEventCollector(e)
	if _, err := l.processEvent(collected, outputEvent(t, "reviewer", "review", "approved"), state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	var firstDelta *events.StateDeltaEvent
	for _, ev := range collected.Emitted {
		if d, ok := ev.(*events.StateDeltaEvent); ok {
			firstDelta = d
		}
	}
	if firstDelta == nil {
		t.Fatal("no STATE_DELTA emitted for the first node output")
	}
	held, _ := firstDelta.Delta[0].Value.(map[string]any)["nodeOutputs"].(map[string]any)
	if len(held) != 1 {
		t.Fatalf("first delta holds %d outputs, want 1", len(held))
	}

	// A second node reports; the already-emitted event must not change.
	if _, err := l.processEvent(e, outputEvent(t, "publisher", "publish", "live"), state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	if len(held) != 1 {
		t.Errorf("the first delta grew to %v after a later node reported", held)
	}

	_ = rec
}
