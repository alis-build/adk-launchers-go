package agui

import (
	"context"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// TestReservedADKStateKey covers the reserved-key half of FR3.
//
// Node outputs travel to the client on the same state channel host application
// state uses. Without reserving the prefix, they would be read back as host
// state on the next turn and written into the ADK session, so the launcher's
// own bookkeeping would slowly become part of the agent's world.
func TestReservedADKStateKey(t *testing.T) {
	t.Run("_adk is internal", func(t *testing.T) {
		if !isInternalStateKey(stream.NodeOutputsStateKey) {
			t.Errorf("isInternalStateKey(%q) = false, want true", stream.NodeOutputsStateKey)
		}
	})

	t.Run("nested _adk paths are internal", func(t *testing.T) {
		for _, key := range []string{"_adk.nodeOutputs", "_adk_anything"} {
			if !isInternalStateKey(key) {
				t.Errorf("isInternalStateKey(%q) = false, want true", key)
			}
		}
	})

	t.Run("host keys stay visible", func(t *testing.T) {
		// The reservation must not swallow ordinary application state, including
		// keys that merely start with an underscore.
		for _, key := range []string{"count", "adk", "_adkish", "ui"} {
			if isInternalStateKey(key) {
				t.Errorf("isInternalStateKey(%q) = true, want false", key)
			}
		}
	})

	t.Run("_adk is stripped from state snapshots", func(t *testing.T) {
		snap := stream.BuildStateSnapshot(nil, map[string]any{
			"count":                    1,
			stream.NodeOutputsStateKey: map[string]any{"nodeOutputs": map[string]any{"review": "ok"}},
		}, isInternalStateKey)

		if _, present := snap[stream.NodeOutputsStateKey]; present {
			t.Errorf("snapshot carries %q, want it filtered as internal", stream.NodeOutputsStateKey)
		}
		if snap["count"] != 1 {
			t.Errorf("snapshot dropped host state: %v", snap)
		}
	})
}

// TestNodeOutputsInInterruptSnapshot proves outbound node outputs reach the
// client on the snapshot even though inbound _adk is stripped.
//
// The two directions are deliberately asymmetric: an _adk value arriving from
// session state or a client request is untrusted and dropped, while the
// launcher's own accumulated map is authoritative and added.
func TestNodeOutputsInInterruptSnapshot(t *testing.T) {
	svc := session.InMemoryService()
	ctx := context.Background()
	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "test-app", UserID: "user-1", SessionID: "t1",
		State: map[string]any{"count": 1},
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
		// An _adk value the client sent back must not survive into the snapshot.
		ReqState: map[string]any{"ui": "panel", "_adk": map[string]any{"nodeOutputs": map[string]any{"spoofed": true}}},
	}

	out := outputEvent(t, "reviewer", "review", "approved")
	if _, err := l.processEvent(e, out, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	intrEv := session.NewEvent(t.Context(), "inv1")
	intrEv.Author = "reviewer"
	intrEv.NodeInfo = &session.NodeInfo{Path: "review"}
	intrEv.Content = &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
	}
	if _, err := l.processEvent(e, intrEv, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	var snap map[string]any
	for _, ev := range parseSSEEvents(rec.Body.String()) {
		if ev.Type == events.EventTypeStateSnapshot {
			snap, _ = ev.Raw["snapshot"].(map[string]any)
		}
	}
	if snap == nil {
		t.Fatal("no STATE_SNAPSHOT emitted at the interrupt boundary")
	}

	adk, _ := snap["_adk"].(map[string]any)
	outputs, _ := adk["nodeOutputs"].(map[string]any)
	if outputs["review"] != "approved" {
		t.Errorf("snapshot _adk.nodeOutputs = %v, want review approved", outputs)
	}
	if _, spoofed := outputs["spoofed"]; spoofed {
		t.Error("client-supplied _adk survived into the snapshot; inbound _adk must be dropped")
	}
	if snap["ui"] != "panel" {
		t.Errorf("snapshot dropped host state: %v", snap)
	}
}
