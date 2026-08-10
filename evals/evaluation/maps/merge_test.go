package maps

import "testing"

func TestMergeStringAny(t *testing.T) {
	t.Parallel()

	t.Run("both empty", func(t *testing.T) {
		if got := MergeStringAny(nil, nil); got != nil {
			t.Fatalf("got = %#v, want nil", got)
		}
	})

	t.Run("single side copies without aliasing", func(t *testing.T) {
		base := map[string]any{"k": "v"}
		got := MergeStringAny(base, nil)
		if got["k"] != "v" {
			t.Fatalf("got = %#v", got)
		}
		base["k"] = "mutated"
		if got["k"] != "v" {
			t.Fatalf("merge aliased base map: got = %#v", got)
		}
	})

	t.Run("overlay wins", func(t *testing.T) {
		got := MergeStringAny(
			map[string]any{"a": 1, "b": 2},
			map[string]any{"b": 3, "c": 4},
		)
		want := map[string]any{"a": 1, "b": 3, "c": 4}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s = %v, want %v (got = %#v)", k, got[k], v, got)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("len(got) = %d, want %d", len(got), len(want))
		}
	})
}
