package stream

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
)

// aguiMetadataKey is the only protocol-owned metadata namespace. Everything
// ADK-specific lives under adkMetadataKey so the two cannot collide as either
// side grows.
const (
	aguiMetadataKey = "ag-ui"
	adkMetadataKey  = "adk"
)

// eventMetadata builds the metadata block for everything emitted while
// processing one ADK event, or nil when the event has nothing to report.
//
// Returning nil matters: an event with no invocation id, author or node must
// serialize exactly as it did before metadata existed, since the field is
// omitempty.
func eventMetadata(ev *session.Event) types.Metadata {
	adk := map[string]any{}
	if ev.InvocationID != "" {
		adk["invocationId"] = ev.InvocationID
	}
	if ev.Author != "" {
		adk["author"] = ev.Author
	}
	// Reads the path aguigraph already plumbs onto NodeInfo rather than
	// re-deriving provenance here.
	if prov, ok := NodeProvenanceFrom(ev); ok && prov.Path != "" {
		adk["nodePath"] = prov.Path
	}

	meta := types.Metadata{}
	if len(adk) > 0 {
		meta[adkMetadataKey] = adk
	}
	if usage := tokenUsage(ev); usage != nil {
		meta[aguiMetadataKey] = map[string]any{"tokenUsage": usage}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// tokenUsage maps ADK's usage report onto the token-usage shape the first-party
// AG-UI SDKs publish. Returns nil when the model reported no usage.
func tokenUsage(ev *session.Event) map[string]any {
	u := ev.UsageMetadata
	if u == nil {
		return nil
	}
	return map[string]any{
		"promptTokens":     u.PromptTokenCount,
		"completionTokens": u.CandidatesTokenCount,
		"totalTokens":      u.TotalTokenCount,
	}
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
// sink is returned unwrapped when there is nothing to attach.
func withEventMetadata(sink eventSink, ev *session.Event) eventSink {
	meta := eventMetadata(ev)
	if meta == nil {
		return sink
	}
	return &metadataSink{inner: sink, meta: meta}
}

// Emit attaches the metadata and forwards. A value already on the event wins:
// a host part converter that set its own metadata knows something the launcher
// does not.
func (s *metadataSink) Emit(ev events.Event) {
	if base := ev.GetBaseEvent(); base != nil {
		base.Metadata = types.MergeMetadata(s.meta, base.Metadata)
	}
	s.inner.Emit(ev)
}

func (s *metadataSink) Err() error { return s.inner.Err() }
