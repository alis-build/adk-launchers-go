package stream

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
)

// tokenCounts accumulates a run's token usage.
//
// Kept as plain integers with a seen flag rather than the SDK's pointer form,
// so summing does not have to dereference on every event. It converts to
// [events.TokenUsage] once, at the terminal event.
type tokenCounts struct {
	seen              bool
	input             int64
	output            int64
	total             int64
	reasoning         int64
	cachedInput       int64
	negativeInput     bool
	negativeOutput    bool
	negativeTotal     bool
	negativeReasoning bool
	negativeCached    bool
}

// RecordTokenUsage adds an event's reported usage to the run's total.
//
// Streaming partials are skipped. ADK reports usage on the completed response
// (see the openai model path, which sets it on the final), so counting a chunk
// that carried a figure would count the same call twice.
//
// Assumption worth revisiting if counts ever look inflated: this sums one
// figure per completed model call. If a provider path instead reports a
// running total on every event, summing would over-count and the rule should
// become "take the last" for that path.
func (s *State) RecordTokenUsage(ev *session.Event) {
	if ev == nil || ev.Partial || ev.UsageMetadata == nil {
		return
	}
	u := ev.UsageMetadata
	s.tokenUsage.seen = true
	s.tokenUsage.add(&s.tokenUsage.input, &s.tokenUsage.negativeInput, u.PromptTokenCount)
	s.tokenUsage.add(&s.tokenUsage.output, &s.tokenUsage.negativeOutput, u.CandidatesTokenCount)
	s.tokenUsage.add(&s.tokenUsage.total, &s.tokenUsage.negativeTotal, u.TotalTokenCount)
	s.tokenUsage.add(&s.tokenUsage.reasoning, &s.tokenUsage.negativeReasoning, u.ThoughtsTokenCount)
	s.tokenUsage.add(&s.tokenUsage.cachedInput, &s.tokenUsage.negativeCached, u.CachedContentTokenCount)
}

// add folds one reported count in, remembering whether anything negative was
// ever seen for that field.
func (t *tokenCounts) add(into *int64, negative *bool, count int32) {
	if count < 0 {
		*negative = true
		return
	}
	*into += int64(count)
}

// TokenUsage returns the run's usage in the shape the terminal event carries,
// or nil when nothing was reported.
//
// One entry, with no provider or model: ADK surfaces neither at this layer, and
// both are optional in the protocol. A run spanning several models therefore
// arrives summed rather than split, which is a limitation of what ADK tells us
// rather than a choice.
//
// A count ADK reported as zero stays absent rather than becoming an explicit
// zero. ADK types counts as int32 with omitempty, so it cannot distinguish
// "produced none" from "did not report", and asserting a measured zero we never
// observed would be the stronger claim. A field that ever saw a negative is
// dropped too: the SDK's Validate rejects negatives and would take the whole
// terminal event down, and a bad figure is not worth losing the run over.
func (s *State) TokenUsage() []events.TokenUsage {
	t := s.tokenUsage
	if !t.seen {
		return nil
	}

	usage := events.TokenUsage{}
	var populated bool
	for _, f := range []struct {
		into     **int64
		count    int64
		negative bool
	}{
		{&usage.InputTokens, t.input, t.negativeInput},
		{&usage.OutputTokens, t.output, t.negativeOutput},
		{&usage.TotalTokens, t.total, t.negativeTotal},
		{&usage.ReasoningTokens, t.reasoning, t.negativeReasoning},
		{&usage.CachedInputTokens, t.cachedInput, t.negativeCached},
	} {
		if f.negative || f.count == 0 {
			continue
		}
		*f.into = events.TokenCount(f.count)
		populated = true
	}

	// Every count was dropped as zero or negative, and the entry carries no
	// provider or model either, so it would go out as a bare "{}": a claim that
	// usage was reported with nothing in it to read. Absent says the same thing
	// and says it honestly.
	if !populated {
		return nil
	}
	return []events.TokenUsage{usage}
}
