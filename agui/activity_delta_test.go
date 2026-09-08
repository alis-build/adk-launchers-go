package agui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// activityConverter returns a part converter that turns render_ui responses
// into activity snapshots, which is the only way activity reaches the stream.
func activityConverter(surface string) GenAIPartConverter {
	return func(_ context.Context, _ *session.Event, part *genai.Part) ([]events.Event, error) {
		if part.FunctionResponse == nil || part.FunctionResponse.Name != "render_ui" {
			return nil, nil
		}
		activityType, _ := part.FunctionResponse.Response["activityType"].(string)
		if activityType == "" {
			activityType = "custom-ui"
		}
		content := part.FunctionResponse.Response["content"]
		return []events.Event{events.NewActivitySnapshotEvent(surface, activityType, content)}, nil
	}
}

// driveActivity feeds one activity update through the processor.
func driveActivity(t *testing.T, l *aguiLauncher, sink eventSink, state *streamState, conv GenAIPartConverter, activityType string, content any) {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-activity"
	ev.Author = "test-app"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: "fr-1", Name: "render_ui",
			Response: map[string]any{"activityType": activityType, "content": content},
		}}},
	}
	if _, err := l.processEvent(sink, ev, state, conv); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
}

// activityEvents summarises the activity stream as "snapshot" / "delta:<patch>".
func activityEvents(t *testing.T, evts []sseEvent) []string {
	t.Helper()
	var out []string
	for _, ev := range evts {
		switch ev.Type {
		case events.EventTypeActivitySnapshot:
			out = append(out, "snapshot")
		case events.EventTypeActivityDelta:
			b, err := json.Marshal(ev.Raw["patch"])
			if err != nil {
				t.Fatalf("marshal patch: %v", err)
			}
			out = append(out, "delta:"+string(b))
		}
	}
	return out
}

func TestActivityDelta(t *testing.T) {
	setup := func() (*aguiLauncher, eventSink, *streamState, func() []sseEvent) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
		state.RunCtx = context.Background()
		return l, e, state, func() []sseEvent { return parseSSEEvents(rec.Body.String()) }
	}

	t.Run("the first update for a surface is a snapshot", func(t *testing.T) {
		// A delta needs something to patch against, and the client has nothing
		// for a surface it has not seen.
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})

		got := activityEvents(t, read())
		if len(got) != 1 || got[0] != "snapshot" {
			t.Errorf("activity = %v, want one snapshot", got)
		}
	})

	t.Run("a repeat update for the same surface is a delta", func(t *testing.T) {
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one", "shared": "same"})
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "two", "shared": "same"})

		got := activityEvents(t, read())
		if len(got) != 2 {
			t.Fatalf("activity = %v, want a snapshot then a delta", got)
		}
		if got[0] != "snapshot" {
			t.Errorf("first activity = %q, want snapshot", got[0])
		}
		want := `delta:[{"op":"replace","path":"/step","value":"two"}]`
		if got[1] != want {
			t.Errorf("second activity = %q, want %q", got[1], want)
		}
	})

	t.Run("a different activity type on the same surface starts fresh", func(t *testing.T) {
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})
		driveActivity(t, l, e, state, conv, "other-ui", map[string]any{"step": "one"})

		got := activityEvents(t, read())
		if len(got) != 2 || got[0] != "snapshot" || got[1] != "snapshot" {
			t.Errorf("activity = %v, want two snapshots for two activity types", got)
		}
	})

	t.Run("an unpatchable update falls back to a snapshot", func(t *testing.T) {
		// Content that is not a JSON object has no key paths to address, so a
		// full snapshot is the only correct thing to send.
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})
		driveActivity(t, l, e, state, conv, "custom-ui", "now a bare string")

		got := activityEvents(t, read())
		if len(got) != 2 || got[1] != "snapshot" {
			t.Errorf("activity = %v, want the unpatchable update sent as a snapshot", got)
		}
	})

	t.Run("an unchanged repeat emits nothing", func(t *testing.T) {
		// The SDK rejects a delta whose patch is empty ("patch field must
		// contain at least one operation"), so the protocol will not carry a
		// no-op. The client's state already matches, so a snapshot would say
		// nothing either.
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})

		got := activityEvents(t, read())
		if len(got) != 1 || got[0] != "snapshot" {
			t.Errorf("activity = %v, want just the first snapshot", got)
		}
	})

	t.Run("after a fallback snapshot the next update deltas against it", func(t *testing.T) {
		l, e, state, read := setup()
		conv := activityConverter("surface-1")
		driveActivity(t, l, e, state, conv, "custom-ui", "bare")
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "two"})

		got := activityEvents(t, read())
		want := `delta:[{"op":"replace","path":"/step","value":"two"}]`
		if len(got) != 3 || got[2] != want {
			t.Errorf("activity = %v, want the third update to patch the second", got)
		}
	})

	t.Run("non-activity converter events pass straight through", func(t *testing.T) {
		l, e, state, read := setup()
		conv := func(context.Context, *session.Event, *genai.Part) ([]events.Event, error) {
			return []events.Event{events.NewCustomEvent("ping")}, nil
		}
		driveActivity(t, l, e, state, conv, "custom-ui", map[string]any{"step": "one"})

		var sawCustom bool
		for _, ev := range read() {
			if ev.Type == events.EventTypeCustom {
				sawCustom = true
			}
		}
		if !sawCustom {
			t.Error("a non-activity converter event was swallowed")
		}
	})
}
