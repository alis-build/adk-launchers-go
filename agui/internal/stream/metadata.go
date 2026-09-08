package stream

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
)

// adkMetadataKey namespaces everything this launcher writes.
//
// [types.AGUIMetadataKey] ("ag-ui") is reserved for AG-UI's own use and every
// other key is user space, so the launcher writes nothing there. Token usage in
// particular looks like it belongs to the protocol, but nothing in the SDK
// defines a shape for it, and claiming the reserved key would collide the day
// AG-UI does.
const adkMetadataKey = "adk"

// eventMetadata builds the metadata block for everything emitted while
// processing one ADK event, or nil when the event has nothing to report.
//
// Returning nil matters: an event with no invocation id, author or node must
// serialize exactly as it did before metadata existed, since the field is
// omitempty.
//
// nodePath is supplied by the caller rather than read here, because resolving
// it locally would bypass [Processor.nodeProvenance] and leave this the one
// attribution site the launcher's opt-out cannot switch off. An empty string
// means either no node or attribution disabled; both omit the key.
func eventMetadata(ev *session.Event, nodePath string) types.Metadata {
	adk := map[string]any{}
	if ev.InvocationID != "" {
		adk["invocationId"] = ev.InvocationID
	}
	if ev.Author != "" {
		adk["author"] = ev.Author
	}
	if nodePath != "" {
		adk["nodePath"] = nodePath
	}

	if usage := tokenUsage(ev); usage != nil {
		adk["tokenUsage"] = usage
	}

	if len(adk) == 0 {
		return nil
	}
	return types.Metadata{adkMetadataKey: adk}
}

// tokenUsage maps ADK's usage report onto the TokenUsage shape the TypeScript,
// Python and .NET AG-UI SDKs publish. Returns nil when the model reported none.
//
// Those SDKs carry it as a `usage` array on the terminal event, one entry per
// provider and model. The Go SDK has neither the type nor the field, so this
// travels under metadata.adk instead — but in the canonical shape, array and
// all, so that adopting `usage` later is a move rather than a rewrite.
//
// Getting the names right now matters more than it looks: TypeScript's
// TokenUsageSchema strips keys it does not know, so a non-canonical name would
// vanish on parse with no error, while Python would keep it. Same payload,
// different outcome per client.
//
// provider and model are absent because ADK does not surface either on the
// event. Zero counts are omitted rather than reported as zero, so "not measured"
// stays distinguishable from "measured none".
func tokenUsage(ev *session.Event) []map[string]any {
	u := ev.UsageMetadata
	if u == nil {
		return nil
	}

	entry := map[string]any{}
	for name, count := range map[string]int32{
		"inputTokens":       u.PromptTokenCount,
		"outputTokens":      u.CandidatesTokenCount,
		"totalTokens":       u.TotalTokenCount,
		"reasoningTokens":   u.ThoughtsTokenCount,
		"cachedInputTokens": u.CachedContentTokenCount,
	} {
		if count > 0 {
			entry[name] = count
		}
	}
	if len(entry) == 0 {
		return nil
	}
	return []map[string]any{entry}
}

// metadataSink stamps event metadata onto everything emitted while processing
// one ADK event.
//
// Wrapping the sink is what makes "every event in a run" achievable: the
// alternative is remembering to pass metadata at each of the couple of dozen
// emit sites, where the first one forgotten is a silent gap.
type metadataSink struct {
	inner eventSink
	meta  types.Metadata
}

// withEventMetadata wraps sink so events emitted for ev carry its metadata. The
// sink is returned unwrapped when there is nothing to attach. nodePath is the
// caller's already-opt-out-checked graph path; see [eventMetadata].
func withEventMetadata(sink eventSink, ev *session.Event, nodePath string) eventSink {
	meta := eventMetadata(ev, nodePath)
	if meta == nil {
		return sink
	}
	return &metadataSink{inner: sink, meta: meta}
}

// Emit attaches a private copy of the metadata and forwards. A value already on
// the event wins: a host part converter that set its own metadata knows
// something the launcher does not.
//
// The copy is what keeps events independent. MergeMetadata returns the existing
// map untouched when the event carries none — the common case — so sharing
// s.meta directly would leave every event emitted for one ADK event aliasing a
// single map. An OnEmit interceptor, or a host holding earlier events from an
// AfterEventCallback, would then mutate all of them at once.
func (s *metadataSink) Emit(ev events.Event) {
	if base := ev.GetBaseEvent(); base != nil {
		base.Metadata = types.MergeMetadata(cloneMetadata(s.meta), base.Metadata)
	}
	s.inner.Emit(ev)
}

// cloneMetadata copies the metadata block and the namespace maps inside it.
// Values within a namespace are scalars and maps the launcher builds once per
// ADK event and never mutates, so they are shared rather than deep-copied.
func cloneMetadata(meta types.Metadata) types.Metadata {
	out := make(types.Metadata, len(meta))
	for key, val := range meta {
		ns, ok := val.(map[string]any)
		if !ok {
			out[key] = val
			continue
		}
		nsCopy := make(map[string]any, len(ns))
		for nsKey, nsVal := range ns {
			nsCopy[nsKey] = nsVal
		}
		out[key] = nsCopy
	}
	return out
}

func (s *metadataSink) Err() error { return s.inner.Err() }
