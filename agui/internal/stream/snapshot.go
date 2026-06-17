package stream

import (
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/session"
)

// BuildStateSnapshot merges persisted session state with optional request state,
// omitting keys matched by isInternal.
func BuildStateSnapshot(sess session.Session, reqState map[string]any, isInternal func(string) bool) map[string]any {
	out := make(map[string]any)
	if sess != nil {
		for key, val := range sess.State().All() {
			if isInternal == nil || !isInternal(key) {
				out[key] = val
			}
		}
	}
	for key, val := range reqState {
		if isInternal == nil || !isInternal(key) {
			out[key] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EmitStateSnapshotIfNonEmpty emits a StateSnapshotEvent when snapshot has keys.
func EmitStateSnapshotIfNonEmpty(sink Sink, snapshot map[string]any) {
	if len(snapshot) > 0 {
		sink.Emit(events.NewStateSnapshotEvent(snapshot))
	}
}

// EmitMessagesSnapshotIfNonEmpty emits a MessagesSnapshotEvent when messages exist.
func EmitMessagesSnapshotIfNonEmpty(sink Sink, messages []types.Message) {
	if len(messages) > 0 {
		sink.Emit(events.NewMessagesSnapshotEvent(messages))
	}
}

// IsPendingProxyResponse reports client proxy tool "pending" LRO responses.
func IsPendingProxyResponse(response map[string]any) bool {
	if response == nil {
		return false
	}
	status, _ := response["status"].(string)
	return status == "pending"
}

// FinalizeLifecycle closes open text, reasoning, and step lifecycle events.
func FinalizeLifecycle(sink Sink, state *State) {
	finalizeLifecycle(sink, state)
}

// EventCollector wraps a sink and records events emitted during one ProcessEvent call.
type EventCollector struct {
	inner   Sink
	Emitted []events.Event
}

func NewEventCollector(inner Sink) *EventCollector {
	return &EventCollector{inner: inner}
}

func (c *EventCollector) Emit(event events.Event) {
	c.Emitted = append(c.Emitted, event)
	c.inner.Emit(event)
}

func (c *EventCollector) Err() error {
	return c.inner.Err()
}

// DefaultIsInternalStateKey matches launcher-managed _agui_* session keys.
func DefaultIsInternalStateKey(key string) bool {
	return strings.HasPrefix(key, "_agui_")
}
