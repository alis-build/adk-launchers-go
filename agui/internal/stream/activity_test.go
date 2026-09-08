package stream

import (
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

func TestActivitySnapshotTracking(t *testing.T) {
	t.Run("remembers the last content per message and type", func(t *testing.T) {
		state := &State{}
		state.RecordActivitySnapshot("surface-1", "custom-ui", map[string]any{"step": "one"})

		got, ok := state.LastActivitySnapshot("surface-1", "custom-ui")
		if !ok {
			t.Fatal("LastActivitySnapshot() ok = false, want true")
		}
		if m, _ := got.(map[string]any); m["step"] != "one" {
			t.Errorf("LastActivitySnapshot() = %v, want step one", got)
		}
	})

	t.Run("message id and activity type together form the key", func(t *testing.T) {
		// One surface can carry several activity types, and one type can appear
		// on several surfaces; keying on either alone would cross the streams.
		state := &State{}
		state.RecordActivitySnapshot("surface-1", "custom-ui", "a")
		state.RecordActivitySnapshot("surface-1", "other-ui", "b")
		state.RecordActivitySnapshot("surface-2", "custom-ui", "c")

		for _, tt := range []struct{ msg, typ, want string }{
			{"surface-1", "custom-ui", "a"},
			{"surface-1", "other-ui", "b"},
			{"surface-2", "custom-ui", "c"},
		} {
			got, ok := state.LastActivitySnapshot(tt.msg, tt.typ)
			if !ok || got != tt.want {
				t.Errorf("LastActivitySnapshot(%q, %q) = %v, want %q", tt.msg, tt.typ, got, tt.want)
			}
		}
	})

	t.Run("a later snapshot replaces the earlier one", func(t *testing.T) {
		state := &State{}
		state.RecordActivitySnapshot("surface-1", "custom-ui", "one")
		state.RecordActivitySnapshot("surface-1", "custom-ui", "two")

		if got, _ := state.LastActivitySnapshot("surface-1", "custom-ui"); got != "two" {
			t.Errorf("LastActivitySnapshot() = %v, want two", got)
		}
	})

	t.Run("an unseen pair has no snapshot", func(t *testing.T) {
		state := &State{}
		if _, ok := state.LastActivitySnapshot("surface-1", "custom-ui"); ok {
			t.Error("LastActivitySnapshot() ok = true for a pair never recorded")
		}
	})

	t.Run("finalization clears the tracking", func(t *testing.T) {
		// Snapshots are per run. Carrying them across would let the first
		// activity of a new run emit a delta against a surface the client has
		// already torn down.
		state := &State{}
		state.RecordActivitySnapshot("surface-1", "custom-ui", "one")
		state.ClearActivitySnapshots()

		if _, ok := state.LastActivitySnapshot("surface-1", "custom-ui"); ok {
			t.Error("LastActivitySnapshot() ok = true after ClearActivitySnapshots")
		}
	})

	t.Run("clearing an empty tracker is safe", func(t *testing.T) {
		(&State{}).ClearActivitySnapshots()
	})
}

func TestComputeActivityPatch(t *testing.T) {
	// opsEqual compares patch operations ignoring order, since a patch derived
	// from map iteration has no meaningful order.
	opsEqual := func(t *testing.T, got []events.JSONPatchOperation, want []events.JSONPatchOperation) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("patch = %+v, want %+v", got, want)
		}
		for _, w := range want {
			found := false
			for _, g := range got {
				if g.Op == w.Op && g.Path == w.Path && reflect.DeepEqual(g.Value, w.Value) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("patch missing %+v; got %+v", w, got)
			}
		}
	}

	t.Run("changed field becomes replace", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"step": "one", "shared": "same"},
			map[string]any{"step": "two", "shared": "same"},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "replace", Path: "/step", Value: "two"}})
	})

	t.Run("new field becomes add", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"step": "one"},
			map[string]any{"step": "one", "note": "hi"},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "add", Path: "/note", Value: "hi"}})
	})

	t.Run("dropped field becomes remove", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"step": "one", "note": "hi"},
			map[string]any{"step": "one"},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "remove", Path: "/note"}})
	})

	t.Run("nested objects patch at their own path", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"form": map[string]any{"name": "a", "age": 1}},
			map[string]any{"form": map[string]any{"name": "b", "age": 1}},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "replace", Path: "/form/name", Value: "b"}})
	})

	t.Run("keys with slashes are escaped", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"a/b": 1},
			map[string]any{"a/b": 2},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "replace", Path: "/a~1b", Value: 2}})
	})

	t.Run("a same-length array patches element by element", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"items": []any{"a", "b"}},
			map[string]any{"items": []any{"a", "c"}},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "replace", Path: "/items/1", Value: "c"}})
	})

	t.Run("a resized array is replaced whole", func(t *testing.T) {
		// Element-wise diffing across a length change needs add and remove at
		// shifting indices, which is where hand-rolled patches go wrong. Replace
		// the array instead: correct, and no larger than the array itself.
		got, ok := ComputeActivityPatch(
			map[string]any{"items": []any{"a", "b"}},
			map[string]any{"items": []any{"a"}},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		opsEqual(t, got, []events.JSONPatchOperation{{Op: "replace", Path: "/items", Value: []any{"a"}}})
	})

	t.Run("identical content yields an empty patch", func(t *testing.T) {
		got, ok := ComputeActivityPatch(
			map[string]any{"step": "one"},
			map[string]any{"step": "one"},
		)
		if !ok {
			t.Fatal("ComputeActivityPatch() ok = false, want true")
		}
		if len(got) != 0 {
			t.Errorf("patch = %+v, want empty for identical content", got)
		}
	})

	t.Run("a non-object side is unpatchable", func(t *testing.T) {
		// The patch paths this builds are rooted at object keys, so a scalar or
		// array at the root has nothing to address. The caller falls back to a
		// full snapshot.
		for _, tt := range []struct {
			name       string
			prev, next any
		}{
			{"scalar to object", "text", map[string]any{"a": 1}},
			{"object to scalar", map[string]any{"a": 1}, "text"},
			{"array root", []any{1}, []any{2}},
			{"nil previous", nil, map[string]any{"a": 1}},
		} {
			if _, ok := ComputeActivityPatch(tt.prev, tt.next); ok {
				t.Errorf("%s: ComputeActivityPatch() ok = true, want false", tt.name)
			}
		}
	})
}
