package stream

import (
	"reflect"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
)

// subagentKey identifies one sub-agent activation within a run.
//
// Author alone is not enough. ADK's Branch is what keeps peer sub-agents from
// seeing each other's history, so two activations of the same agent on
// different branches are concurrent runs rather than one continuing.
type subagentKey struct {
	Author string
	Branch string
}

// subagentRun is the open activation a run is currently attributing events to.
type subagentRun struct {
	key   subagentKey
	runID string
	name  string
}

// subagentKeyFor returns the activation an event belongs to, and false when the
// event is not from a sub-agent.
//
// The root agent is the run itself, not a sub-agent within it, so its events
// carry no attribution.
func subagentKeyFor(ev *session.Event, rootAppName string) (subagentKey, bool) {
	if ev.Author == "" || ev.Author == rootAppName {
		return subagentKey{}, false
	}
	return subagentKey{Author: ev.Author, Branch: ev.Branch}, true
}

// openSubagent starts an activation if the event belongs to a different one
// than is currently open, closing the previous one first.
//
// Emitted before anything else for the event, so the SUBAGENT_STARTED precedes
// the content it attributes, and the close precedes the start so the SDK's
// sequence validator never sees two runs open at once.
func openSubagent(sink eventSink, state *State, ev *session.Event) {
	key, ok := subagentKeyFor(ev, state.RootAppName)
	if !ok {
		if state.CurrentSubagent != nil {
			closeTextMessage(sink, state)
			closeReasoningMessage(sink, state)
			closeSubagent(sink, state, nil)
		}
		return
	}
	if state.CurrentSubagent != nil && state.CurrentSubagent.key == key {
		return
	}

	// Close the outgoing producer's open content before the handover, whoever
	// it was. Two things go wrong otherwise: an outgoing sub-agent's own
	// TEXT_MESSAGE_END lands after the SUBAGENT_FINISHED meant to close it, and
	// an outgoing root agent's TEXT_MESSAGE_END is emitted after the incoming
	// SUBAGENT_STARTED and so gets stamped with a run id it never belonged to.
	// The step block below closes these too; both calls are idempotent.
	closeTextMessage(sink, state)
	closeReasoningMessage(sink, state)
	closeSubagent(sink, state, nil)
	run := &subagentRun{key: key, runID: events.GenerateRunID(), name: key.Author}
	state.CurrentSubagent = run
	sink.Emit(events.NewSubagentStartedEvent(run.runID, run.name))
}

// closeSubagent finishes the open activation, if any.
//
// interruptIDs names the interrupts the subagent owns; a non-empty list makes
// the outcome "suspended" rather than "success", which is how a client tells a
// branch that finished from one waiting on a human.
func closeSubagent(sink eventSink, state *State, interruptIDs []string) {
	run := state.CurrentSubagent
	if run == nil {
		return
	}
	state.CurrentSubagent = nil

	opt := events.WithSubagentSuccessOutcome()
	if len(interruptIDs) > 0 {
		opt = events.WithSubagentSuspendedOutcome(interruptIDs)
	}
	sink.Emit(events.NewSubagentFinishedEvent(run.runID, opt))
}

// subagentRunIDField caches, per event type, the index of a SubagentRunID field.
//
// The field is declared on each of the two dozen event types that carry it
// rather than on BaseEvent, so there is no interface to set it through. A type
// switch would have to name every one of them, and the first type missed — or
// added upstream later — would be a silent gap. Reflection covers them all and
// keeps covering them; the cache keeps the lookup off the hot path.
var subagentRunIDField sync.Map // reflect.Type -> int (-1 when absent)

// stampSubagentRunID attributes an event to the open sub-agent activation.
//
// A value already set wins: a host part converter that attributed its own event
// knows something the launcher does not.
func stampSubagentRunID(ev events.Event, runID string) {
	if runID == "" {
		return
	}
	v := reflect.ValueOf(ev)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return
	}

	idx := -1
	if cached, ok := subagentRunIDField.Load(elem.Type()); ok {
		idx = cached.(int)
	} else {
		if f, ok := elem.Type().FieldByName("SubagentRunID"); ok && len(f.Index) == 1 && f.Type.Kind() == reflect.String {
			idx = f.Index[0]
		}
		subagentRunIDField.Store(elem.Type(), idx)
	}
	if idx < 0 {
		return
	}

	field := elem.Field(idx)
	if field.CanSet() && field.String() == "" {
		field.SetString(runID)
	}
}

// subagentSink stamps the open activation's run id onto everything emitted
// while it is open.
type subagentSink struct {
	inner eventSink
	state *State
}

// withSubagentAttribution wraps sink so emitted events carry the open
// activation's run id.
//
// The run id is read at emit time rather than captured, because the activation
// opens and closes during the same ProcessEvent call the sink wraps.
func withSubagentAttribution(sink eventSink, state *State) eventSink {
	return &subagentSink{inner: sink, state: state}
}

func (s *subagentSink) Emit(ev events.Event) {
	if run := s.state.CurrentSubagent; run != nil {
		stampSubagentRunID(ev, run.runID)
	}
	s.inner.Emit(ev)
}

func (s *subagentSink) Err() error { return s.inner.Err() }
