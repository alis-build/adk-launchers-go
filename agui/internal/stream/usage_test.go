package stream

import (
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func usageEvent(partial bool, prompt, candidates, total int32) *session.Event {
	ev := &session.Event{}
	ev.Partial = partial
	ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     prompt,
		CandidatesTokenCount: candidates,
		TotalTokenCount:      total,
	}
	return ev
}

func TestRecordTokenUsage(t *testing.T) {
	t.Run("sums across completed model calls", func(t *testing.T) {
		state := &State{}
		state.RecordTokenUsage(usageEvent(false, 10, 4, 14))
		state.RecordTokenUsage(usageEvent(false, 20, 6, 26))

		got := state.TokenUsage()
		if len(got) != 1 {
			t.Fatalf("TokenUsage() = %v, want one entry", got)
		}
		if *got[0].InputTokens != 30 || *got[0].OutputTokens != 10 || *got[0].TotalTokens != 40 {
			t.Errorf("counts = in %d out %d total %d, want 30/10/40",
				*got[0].InputTokens, *got[0].OutputTokens, *got[0].TotalTokens)
		}
	})

	t.Run("streaming partials are not counted", func(t *testing.T) {
		// ADK reports usage on the completed response; a streaming chunk that
		// carries a figure would be the same call counted twice.
		state := &State{}
		state.RecordTokenUsage(usageEvent(true, 10, 4, 14))
		state.RecordTokenUsage(usageEvent(false, 10, 4, 14))

		got := state.TokenUsage()
		if len(got) != 1 || *got[0].TotalTokens != 14 {
			t.Errorf("TokenUsage() = %v, want a single call counted once", got)
		}
	})

	t.Run("an event without usage contributes nothing", func(t *testing.T) {
		state := &State{}
		state.RecordTokenUsage(&session.Event{})
		if got := state.TokenUsage(); got != nil {
			t.Errorf("TokenUsage() = %v, want nil", got)
		}
	})

	t.Run("no usage at all yields no entry", func(t *testing.T) {
		if got := (&State{}).TokenUsage(); got != nil {
			t.Errorf("TokenUsage() = %v, want nil", got)
		}
	})

	t.Run("counts ADK reported as zero stay absent", func(t *testing.T) {
		// ADK types counts as int32 with omitempty, so it cannot tell "produced
		// none" from "did not report". Claiming a measured zero we never
		// observed would be worse than saying nothing.
		state := &State{}
		state.RecordTokenUsage(usageEvent(false, 0, 0, 14))

		got := state.TokenUsage()
		if len(got) != 1 {
			t.Fatalf("TokenUsage() = %v, want one entry", got)
		}
		if got[0].InputTokens != nil || got[0].OutputTokens != nil {
			t.Errorf("zero counts materialised as %v / %v, want nil",
				got[0].InputTokens, got[0].OutputTokens)
		}
		if *got[0].TotalTokens != 14 {
			t.Errorf("totalTokens = %d, want 14", *got[0].TotalTokens)
		}
	})

	t.Run("reasoning and cached counts carry through", func(t *testing.T) {
		state := &State{}
		ev := usageEvent(false, 10, 4, 14)
		ev.UsageMetadata.ThoughtsTokenCount = 7
		ev.UsageMetadata.CachedContentTokenCount = 5
		state.RecordTokenUsage(ev)

		got := state.TokenUsage()
		if *got[0].ReasoningTokens != 7 || *got[0].CachedInputTokens != 5 {
			t.Errorf("reasoning %d cached %d, want 7/5", *got[0].ReasoningTokens, *got[0].CachedInputTokens)
		}
	})

	t.Run("a negative count is dropped rather than failing the run", func(t *testing.T) {
		// The SDK's Validate rejects a negative count and would take the whole
		// terminal event down with it. A bad figure is not worth losing the run.
		state := &State{}
		state.RecordTokenUsage(usageEvent(false, -5, 4, 14))

		got := state.TokenUsage()
		if len(got) != 1 {
			t.Fatalf("TokenUsage() = %v, want one entry", got)
		}
		if got[0].InputTokens != nil {
			t.Errorf("negative inputTokens survived as %d", *got[0].InputTokens)
		}
		if *got[0].OutputTokens != 4 {
			t.Errorf("outputTokens = %d, want the good count kept", *got[0].OutputTokens)
		}
	})
}

// TestTokenUsageAllZeroReportsNothing pins the empty-entry case.
//
// Every count dropped as zero leaves an entry with no counts, no provider and
// no model, which serializes as a bare "{}" — a claim that usage was reported
// with nothing in it to read.
func TestTokenUsageAllZeroReportsNothing(t *testing.T) {
	state := &State{}
	state.RecordTokenUsage(usageEvent(false, 0, 0, 0))

	if got := state.TokenUsage(); got != nil {
		t.Errorf("TokenUsage() = %v, want nil for an all-zero report", got)
	}
}
