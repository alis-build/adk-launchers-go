package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// eventMetadata returns the metadata block of the first event of a type.
func eventMetadata(t *testing.T, evts []sseEvent, typ events.EventType) map[string]any {
	t.Helper()
	for _, ev := range evts {
		if ev.Type != typ {
			continue
		}
		meta, _ := ev.Raw["metadata"].(map[string]any)
		return meta
	}
	t.Fatalf("no %s event in the stream", typ)
	return nil
}

func TestEventMetadata(t *testing.T) {
	run := func(t *testing.T, mutate func(ev *session.Event)) []sseEvent {
		t.Helper()
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-meta"
		ev.Author = "researcher"
		ev.Content = genai.NewContentFromText("Here you go", genai.RoleModel)
		ev.Partial = true
		mutate(ev)
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		return parseSSEEvents(rec.Body.String())
	}

	t.Run("every event carries invocation id and author", func(t *testing.T) {
		evts := run(t, func(*session.Event) {})
		if len(evts) == 0 {
			t.Fatal("no events emitted")
		}
		for _, ev := range evts {
			meta, _ := ev.Raw["metadata"].(map[string]any)
			adk, _ := meta["adk"].(map[string]any)
			if adk["invocationId"] != "inv-meta" {
				t.Errorf("%s adk.invocationId = %v, want inv-meta", ev.Type, adk["invocationId"])
			}
			if adk["author"] != "researcher" {
				t.Errorf("%s adk.author = %v, want researcher", ev.Type, adk["author"])
			}
		}
	})

	t.Run("workflow events carry the node path", func(t *testing.T) {
		evts := run(t, func(ev *session.Event) {
			ev.NodeInfo = &session.NodeInfo{Path: "review/approve@run-1"}
		})
		meta := eventMetadata(t, evts, events.EventTypeTextMessageContent)
		adk, _ := meta["adk"].(map[string]any)
		if adk["nodePath"] != "review/approve@run-1" {
			t.Errorf("adk.nodePath = %v, want review/approve@run-1", adk["nodePath"])
		}
	})

	t.Run("non-workflow events carry no node path", func(t *testing.T) {
		evts := run(t, func(*session.Event) {})
		meta := eventMetadata(t, evts, events.EventTypeTextMessageContent)
		adk, _ := meta["adk"].(map[string]any)
		if v, present := adk["nodePath"]; present {
			t.Errorf("adk.nodePath present as %v on a non-workflow event", v)
		}
	})

	t.Run("token usage rides our own adk key in the canonical shape", func(t *testing.T) {
		// Field names and the array container match the TokenUsage type the
		// TypeScript, Python and .NET SDKs already publish, even though the Go
		// SDK has no such type yet and no `usage` field to put it on. When Go
		// gains them, this moves across as-is instead of being rewritten.
		evts := run(t, func(ev *session.Event) {
			ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:        11,
				CandidatesTokenCount:    22,
				TotalTokenCount:         33,
				ThoughtsTokenCount:      7,
				CachedContentTokenCount: 5,
			}
		})
		meta := eventMetadata(t, evts, events.EventTypeTextMessageContent)
		adk, _ := meta["adk"].(map[string]any)
		usage, _ := adk["tokenUsage"].([]any)
		if len(usage) != 1 {
			t.Fatalf("metadata.adk.tokenUsage = %v, want a one-entry array", adk["tokenUsage"])
		}
		entry, _ := usage[0].(map[string]any)
		want := map[string]float64{
			"inputTokens":       11,
			"outputTokens":      22,
			"totalTokens":       33,
			"reasoningTokens":   7,
			"cachedInputTokens": 5,
		}
		for key, wantVal := range want {
			if entry[key] != wantVal {
				t.Errorf("tokenUsage[0].%s = %v, want %v", key, entry[key], wantVal)
			}
		}
		for _, gone := range []string{"promptTokens", "completionTokens"} {
			if _, present := entry[gone]; present {
				t.Errorf("tokenUsage[0] still carries the non-canonical %q", gone)
			}
		}
	})

	t.Run("zero counts are omitted", func(t *testing.T) {
		// A model that reports no reasoning or cache tokens should not have
		// zeroes invented for it.
		evts := run(t, func(ev *session.Event) {
			ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{TotalTokenCount: 33}
		})
		meta := eventMetadata(t, evts, events.EventTypeTextMessageContent)
		adk, _ := meta["adk"].(map[string]any)
		usage, _ := adk["tokenUsage"].([]any)
		entry, _ := usage[0].(map[string]any)
		for _, key := range []string{"reasoningTokens", "cachedInputTokens", "inputTokens", "outputTokens"} {
			if _, present := entry[key]; present {
				t.Errorf("tokenUsage[0].%s present as %v with a zero count", key, entry[key])
			}
		}
		if entry["totalTokens"] != float64(33) {
			t.Errorf("tokenUsage[0].totalTokens = %v, want 33", entry["totalTokens"])
		}
	})

	t.Run("nothing is written under the reserved ag-ui key", func(t *testing.T) {
		// types.AGUIMetadataKey is reserved for AG-UI's own use and every other
		// key is user space, so launcher data stays out of it entirely. Writing
		// there would collide the day the protocol defines a field of its own.
		evts := run(t, func(ev *session.Event) {
			ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{TotalTokenCount: 33}
		})
		for _, ev := range evts {
			meta, _ := ev.Raw["metadata"].(map[string]any)
			if v, present := meta[types.AGUIMetadataKey]; present {
				t.Errorf("%s wrote %v under the reserved %q key", ev.Type, v, types.AGUIMetadataKey)
			}
		}
	})

	t.Run("no usage means no tokenUsage key", func(t *testing.T) {
		evts := run(t, func(*session.Event) {})
		meta := eventMetadata(t, evts, events.EventTypeTextMessageContent)
		adk, _ := meta["adk"].(map[string]any)
		if v, present := adk["tokenUsage"]; present {
			t.Errorf("adk.tokenUsage present as %v with no usage reported", v)
		}
	})

	t.Run("an event with nothing to say carries no metadata", func(t *testing.T) {
		// Additive: an ADK event with no invocation id, author or node stays
		// exactly as it was before metadata existed.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = ""
		ev.Author = ""
		ev.Content = genai.NewContentFromText("bare", genai.RoleModel)
		ev.Partial = true
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		for _, x := range parseSSEEvents(rec.Body.String()) {
			if v, present := x.Raw["metadata"]; present {
				t.Errorf("%s carries metadata %v with nothing to report", x.Type, v)
			}
		}
	})
}
