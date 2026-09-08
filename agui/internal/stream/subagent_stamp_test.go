package stream

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// TestStampSubagentRunID covers the reflection helper directly.
//
// It reaches a field declared on two dozen event types rather than on
// BaseEvent, so the cases that matter are the ones a type switch would have had
// to enumerate: a type that carries the field, one that does not, and a value
// already set.
func TestStampSubagentRunID(t *testing.T) {
	t.Run("sets the field on an event that has one", func(t *testing.T) {
		ev := events.NewTextMessageContentEvent("msg-1", "hello")
		stampSubagentRunID(ev, "run-1")
		if ev.SubagentRunID != "run-1" {
			t.Errorf("SubagentRunID = %q, want run-1", ev.SubagentRunID)
		}
	})

	t.Run("uses the cached field index on a repeat", func(t *testing.T) {
		// Second call of the same type takes the cache path rather than the
		// reflect lookup; both must set the field.
		for _, want := range []string{"run-1", "run-2"} {
			ev := events.NewTextMessageContentEvent("msg-1", "hello")
			stampSubagentRunID(ev, want)
			if ev.SubagentRunID != want {
				t.Errorf("SubagentRunID = %q, want %q", ev.SubagentRunID, want)
			}
		}
	})

	t.Run("an existing value wins", func(t *testing.T) {
		// A host part converter that attributed its own event knows something
		// the launcher does not.
		ev := events.NewTextMessageContentEvent("msg-1", "hello")
		ev.SubagentRunID = "host-set"
		stampSubagentRunID(ev, "run-1")
		if ev.SubagentRunID != "host-set" {
			t.Errorf("SubagentRunID = %q, want the host's value kept", ev.SubagentRunID)
		}
	})

	t.Run("an event without the field is left alone", func(t *testing.T) {
		// SUBAGENT_STARTED names the run it opens; it does not carry an
		// attribution field, and reflection must not invent one.
		ev := events.NewSubagentStartedEvent("run-1", "researcher")
		stampSubagentRunID(ev, "run-2")
		if ev.SubagentRunID != "run-1" {
			t.Errorf("SubagentRunID = %q, want run-1 untouched", ev.SubagentRunID)
		}
	})

	t.Run("an empty run id stamps nothing", func(t *testing.T) {
		ev := events.NewTextMessageContentEvent("msg-1", "hello")
		stampSubagentRunID(ev, "")
		if ev.SubagentRunID != "" {
			t.Errorf("SubagentRunID = %q, want empty", ev.SubagentRunID)
		}
	})

	t.Run("a nil event is safe", func(t *testing.T) {
		var ev *events.TextMessageContentEvent
		stampSubagentRunID(ev, "run-1")
	})
}
