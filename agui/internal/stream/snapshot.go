package stream

import (
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
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

// FinalizeRun closes everything a run can leave open before its terminal event:
// the text, reasoning and step lifecycles, and the sub-agent activation.
//
// The activation is why this exists separately from [FinalizeLifecycle]. A run
// whose last producer is a sub-agent — the ordinary shape of an ADK transfer,
// where the sub-agent gives the final answer — has no following root event to
// trigger the handover close, so without this its SUBAGENT_STARTED never gets a
// SUBAGENT_FINISHED and a client's branch view stays open for good.
//
// The sink is wrapped so the closing step event is still attributed to the
// activation that owned it. When sub-agent attribution is disabled there is
// never an open activation, so both the wrap and the close are no-ops.
func FinalizeRun(sink Sink, state *State) {
	attributed := withSubagentAttribution(sink, state)
	finalizeLifecycle(attributed, state)
	closeSubagent(attributed, state, nil)
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
