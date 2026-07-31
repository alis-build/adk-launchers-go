package storage

import (
	"encoding/json"
	"testing"
)

func TestStringSliceForJSON(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		got := StringSliceForJSON(nil)
		if got == nil || len(got) != 0 {
			t.Fatalf("got = %v, want non-nil empty slice", got)
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if string(data) != "[]" {
			t.Fatalf("JSON = %q, want []", data)
		}
	})

	t.Run("non-empty", func(t *testing.T) {
		in := []string{"a", "b"}
		got := StringSliceForJSON(in)
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("got = %v", got)
		}
	})
}
