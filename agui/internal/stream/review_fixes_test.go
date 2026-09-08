package stream

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
)

// recordingSink collects everything emitted, so a test can inspect the events
// themselves rather than their serialized form.
type recordingSink struct{ emitted []events.Event }

func (s *recordingSink) Emit(ev events.Event) { s.emitted = append(s.emitted, ev) }
func (s *recordingSink) Err() error           { return nil }

// TestActivityUpdateWithReusedContentMap covers the converter shape the delta
// path is most likely to meet in the wild: one running activity object the host
// mutates and re-emits.
//
// Recording that map by reference would leave the stored "previous" snapshot
// aliasing the live one, so every later diff would compare a map with itself,
// produce no operations, and freeze the client's activity block at its first
// value with no error anywhere.
func TestActivityUpdateWithReusedContentMap(t *testing.T) {
	state := &State{}
	sink := &recordingSink{}

	// The same map instance throughout, as a stateful converter would hand it.
	content := map[string]any{"step": "one"}
	emitActivityUpdate(sink, state, events.NewActivitySnapshotEvent("surface-1", "custom-ui", content))

	content["step"] = "two"
	emitActivityUpdate(sink, state, events.NewActivitySnapshotEvent("surface-1", "custom-ui", content))

	if len(sink.emitted) != 2 {
		t.Fatalf("emitted %d events, want a snapshot then a delta", len(sink.emitted))
	}
	delta, ok := sink.emitted[1].(*events.ActivityDeltaEvent)
	if !ok {
		t.Fatalf("second event = %T, want *events.ActivityDeltaEvent", sink.emitted[1])
	}
	if len(delta.Patch) != 1 {
		t.Fatalf("delta operations = %v, want one replace of /step", delta.Patch)
	}
	if got := delta.Patch[0]; got.Path != "/step" || got.Value != "two" {
		t.Errorf("delta op = %+v, want replace /step to two", got)
	}

	t.Run("nested maps and slices are copied too", func(t *testing.T) {
		// A shallow copy would still alias everything below the first level,
		// which is where activity payloads keep the parts that actually change.
		state := &State{}
		sink := &recordingSink{}

		items := []any{map[string]any{"label": "first"}}
		content := map[string]any{"items": items}
		emitActivityUpdate(sink, state, events.NewActivitySnapshotEvent("s", "custom-ui", content))

		items[0].(map[string]any)["label"] = "second"
		emitActivityUpdate(sink, state, events.NewActivitySnapshotEvent("s", "custom-ui", content))

		if len(sink.emitted) != 2 {
			t.Fatalf("emitted %d events, want a snapshot then a delta", len(sink.emitted))
		}
		delta, ok := sink.emitted[1].(*events.ActivityDeltaEvent)
		if !ok {
			t.Fatalf("second event = %T, want a delta", sink.emitted[1])
		}
		if len(delta.Patch) != 1 || delta.Patch[0].Path != "/items/0/label" {
			t.Errorf("delta = %+v, want one op at /items/0/label", delta.Patch)
		}
	})
}

// TestEventMetadataNotSharedAcrossEvents covers the aliasing hazard in the
// metadata sink. MergeMetadata returns the existing map untouched when the
// event carries none — the common case — so without a copy every event emitted
// for one ADK event would point at the same map, and one consumer mutating an
// event's metadata would silently rewrite its siblings.
func TestEventMetadataNotSharedAcrossEvents(t *testing.T) {
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-meta"
	ev.Author = "researcher"

	sink := &recordingSink{}
	wrapped := withEventMetadata(sink, ev, "review/approve")

	wrapped.Emit(events.NewTextMessageStartEvent("msg-1"))
	wrapped.Emit(events.NewTextMessageContentEvent("msg-1", "hello"))
	if len(sink.emitted) != 2 {
		t.Fatalf("emitted %d events, want 2", len(sink.emitted))
	}

	first := sink.emitted[0].GetBaseEvent().Metadata
	second := sink.emitted[1].GetBaseEvent().Metadata
	if first == nil || second == nil {
		t.Fatal("an event was emitted without metadata")
	}

	// A consumer editing one event's block must not reach the other's.
	first["adk"].(map[string]any)["author"] = "tampered"
	first["injected"] = true

	secondADK, _ := second["adk"].(map[string]any)
	if secondADK["author"] != "researcher" {
		t.Errorf("second event adk.author = %v after editing the first; the block is shared", secondADK["author"])
	}
	if _, leaked := second["injected"]; leaked {
		t.Error("a key added to one event's metadata appeared on another")
	}
}

// TestEventMetadataOmitsNodePathWhenEmpty pins the contract that lets the
// launcher's graph opt-out reach this site: the path is supplied by the caller,
// which has already consulted the flag, so an empty string omits the key.
func TestEventMetadataOmitsNodePathWhenEmpty(t *testing.T) {
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-meta"
	// NodeInfo is present, so a site reading provenance itself would still
	// publish the path; only the caller's decision keeps it out.
	ev.NodeInfo = &session.NodeInfo{Path: "review/approve"}

	meta := eventMetadata(ev, "")
	adk, _ := meta[adkMetadataKey].(map[string]any)
	if path, present := adk["nodePath"]; present {
		t.Errorf("adk.nodePath = %v, want the key omitted for an empty path", path)
	}
	if adk["invocationId"] != "inv-meta" {
		t.Errorf("adk.invocationId = %v, want inv-meta", adk["invocationId"])
	}
}
