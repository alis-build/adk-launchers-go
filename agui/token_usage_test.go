package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// usageFromTerminal returns the usage array on the run's terminal event.
func usageFromTerminal(t *testing.T, evts []sseEvent) []any {
	t.Helper()
	for _, ev := range evts {
		switch ev.Type {
		case events.EventTypeRunFinished, events.EventTypeRunError:
			usage, _ := ev.Raw["usage"].([]any)
			return usage
		}
	}
	return nil
}

// TestTokenUsageOnTerminalEvent covers the canonical home for usage: a `usage`
// array on RUN_FINISHED, summed across the run rather than repeated per event.
func TestTokenUsageOnTerminalEvent(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	spend := func(prompt, candidates, total int32) *session.Event {
		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-usage"
		ev.Author = "researcher"
		ev.Content = genai.NewContentFromText("working", genai.RoleModel)
		ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount: prompt, CandidatesTokenCount: candidates, TotalTokenCount: total,
		}
		return ev
	}

	// Two completed model calls, then an interrupt ends the run.
	for _, ev := range []*session.Event{spend(10, 4, 14), spend(20, 6, 26)} {
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
	}
	intrEv := session.NewEvent(t.Context(), "inv1")
	intrEv.Author = "researcher"
	intrEv.Content = &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: []*genai.Part{confirmationPart("confirm-1", "Send?", "orig-1", "send_email")},
	}
	if _, err := l.processEvent(e, intrEv, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	usage := usageFromTerminal(t, parseSSEEvents(rec.Body.String()))
	if len(usage) != 1 {
		t.Fatalf("terminal usage = %v, want one entry", usage)
	}
	entry, _ := usage[0].(map[string]any)
	for key, want := range map[string]float64{"inputTokens": 30, "outputTokens": 10, "totalTokens": 40} {
		if entry[key] != want {
			t.Errorf("usage[0].%s = %v, want %v", key, entry[key], want)
		}
	}
}

// TestTokenUsageAbsentWhenUnreported keeps the field off a run nothing reported
// usage for, rather than emitting an empty array.
func TestTokenUsageAbsentWhenUnreported(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	ev := session.NewEvent(t.Context(), "inv1")
	ev.Author = "researcher"
	ev.Content = &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: []*genai.Part{confirmationPart("confirm-1", "Send?", "orig-1", "send_email")},
	}
	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	for _, x := range parseSSEEvents(rec.Body.String()) {
		if x.Type != events.EventTypeRunFinished {
			continue
		}
		if v, present := x.Raw["usage"]; present {
			t.Errorf("RUN_FINISHED carries usage %v with none reported", v)
		}
	}
}
