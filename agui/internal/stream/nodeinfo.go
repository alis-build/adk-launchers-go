package stream

import (
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
)

// NodeProvenance is the workflow-graph attribution carried by one ADK event:
// which node emitted it, whose output it counts as, and where the scheduler
// will go next.
//
// It exists so the stream layer reads ADK's graph fields in one place rather
// than reaching into [session.Event] from every call site.
type NodeProvenance struct {
	// Path is the emitting node's composite path within its activation. Empty
	// for top-level static nodes.
	Path string
	// MessageAsOutput marks this event's content as the node's output.
	MessageAsOutput bool
	// OutputFor lists the node paths this event's output counts for: the
	// emitter plus any delegating ancestors.
	OutputFor []string
	// Routes are the outgoing edges the scheduler will follow.
	Routes []string
}

// NodeProvenanceFrom reads graph attribution off an event. The second result is
// false when the event did not come from a workflow.
//
// Workflow membership is decided by the NodeInfo pointer alone, per ADK's
// documented invariant that readers test the pointer and not its contents. An
// event's Routes are therefore not attribution on their own.
//
// Note the pointer can be non-nil with an empty Path: ADK collapses an all-zero
// NodeInfo to nil only when decoding JSON from another runtime, so an in-process
// event may carry one.
func NodeProvenanceFrom(ev *session.Event) (NodeProvenance, bool) {
	if ev == nil || ev.NodeInfo == nil {
		return NodeProvenance{}, false
	}
	return NodeProvenance{
		Path:            ev.NodeInfo.Path,
		MessageAsOutput: ev.NodeInfo.MessageAsOutput,
		OutputFor:       ev.NodeInfo.OutputFor,
		Routes:          ev.Routes,
	}, true
}

// StepName returns the AG-UI step name for this node, falling back to author
// when the node has no path.
//
// Top-level static nodes carry an empty path, so the agent name is the only
// thing that identifies them to a client.
func (p NodeProvenance) StepName(author string) string {
	if p.Path != "" {
		return p.Path
	}
	return author
}

// OutputPaths returns the node paths this event's output should be recorded
// against.
//
// OutputFor wins when present: one event stands in for a whole delegation
// chain, so the output belongs to every node in it rather than only the
// emitter. A node with neither a path nor OutputFor addresses nothing, and its
// output is dropped rather than recorded under an empty key.
func (p NodeProvenance) OutputPaths() []string {
	if len(p.OutputFor) > 0 {
		return p.OutputFor
	}
	if p.Path == "" {
		return nil
	}
	return []string{p.Path}
}

// NodeOutputValue returns the value an event contributes as its node's output.
// The second result is false when the event carries no output.
//
// An explicit [session.Event.Output] wins. Otherwise MessageAsOutput means the
// event's model text is the output, which is how a node that answers in prose
// rather than structured data reports its result.
func NodeOutputValue(ev *session.Event) (any, bool) {
	prov, ok := NodeProvenanceFrom(ev)
	if !ok {
		return nil, false
	}
	if ev.Output != nil {
		return ev.Output, true
	}
	if !prov.MessageAsOutput {
		return nil, false
	}
	text := modelTextOf(ev)
	if text == "" {
		return nil, false
	}
	return text, true
}

// modelTextOf concatenates the non-thought text parts of an event's content.
// Reasoning parts are excluded: they are how the node got there, not its result.
func modelTextOf(ev *session.Event) string {
	if ev.Content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range ev.Content.Parts {
		if part == nil || part.Thought || part.Text == "" {
			continue
		}
		b.WriteString(part.Text)
	}
	return b.String()
}

// annotateNodeProvenance adds workflow graph attribution to an interrupt raised
// inside a node: which node paused, and where the scheduler would have gone.
//
// This is applied at the collection site rather than inside each handler, so
// every interrupt reason gets attribution without each handler remembering to
// add it.
//
// Interrupt metadata is the only per-event channel the pinned Go SDK offers:
// types.Interrupt.Metadata exists while event-level metadata does not, which is
// why every other event in this track carries provenance in its step name
// instead.
//
// Empty values are omitted rather than written as blanks, so a client can tell
// "no routes" from "routes unknown".
func annotateNodeProvenance(intr *types.Interrupt, ev *session.Event) {
	prov, ok := NodeProvenanceFrom(ev)
	if !ok {
		return
	}
	if prov.Path == "" && len(prov.Routes) == 0 {
		return
	}

	adkMeta, ok := intr.Metadata["adk"].(map[string]any)
	if !ok {
		// Every handler today seeds metadata.adk; this keeps a future one that
		// forgets from silently losing attribution.
		adkMeta = map[string]any{}
		if intr.Metadata == nil {
			intr.Metadata = map[string]any{}
		}
		intr.Metadata["adk"] = adkMeta
	}

	if prov.Path != "" {
		adkMeta["nodePath"] = prov.Path
	}
	if len(prov.Routes) > 0 {
		adkMeta["routes"] = prov.Routes
	}
}
