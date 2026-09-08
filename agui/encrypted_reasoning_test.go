package agui

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// reasoningEvent builds a thought part, optionally carrying the opaque
// reasoning blob ADK surfaces as ThoughtSignature.
func reasoningEvent(t *testing.T, text string, signature []byte, partial bool) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-enc"
	ev.Author = "test-app"
	ev.Partial = partial
	ev.Content = &genai.Content{
		Role:  string(genai.RoleModel),
		Parts: []*genai.Part{{Text: text, Thought: true, ThoughtSignature: signature}},
	}
	return ev
}

func TestEncryptedReasoning(t *testing.T) {
	t.Run("emitted inside the reasoning bracket", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, reasoningEvent(t, "weighing", []byte("blob"), false), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		var order []events.EventType
		var encrypted sseEvent
		for _, ev := range parseSSEEvents(rec.Body.String()) {
			order = append(order, ev.Type)
			if ev.Type == events.EventTypeReasoningEncryptedValue {
				encrypted = ev
			}
		}

		startIdx, encIdx, endIdx := -1, -1, -1
		for i, typ := range order {
			switch typ {
			case events.EventTypeReasoningStart:
				startIdx = i
			case events.EventTypeReasoningEncryptedValue:
				encIdx = i
			case events.EventTypeReasoningEnd:
				endIdx = i
			}
		}
		if encIdx == -1 {
			t.Fatalf("no REASONING_ENCRYPTED_VALUE emitted; got %v", order)
		}
		if startIdx == -1 || encIdx < startIdx {
			t.Errorf("encrypted value at %d does not follow REASONING_START at %d", encIdx, startIdx)
		}
		if endIdx != -1 && encIdx > endIdx {
			t.Errorf("encrypted value at %d follows REASONING_END at %d", encIdx, endIdx)
		}

		if got := encrypted.str("encryptedValue"); got != base64.StdEncoding.EncodeToString([]byte("blob")) {
			t.Errorf("encryptedValue = %q, want the base64 of the signature", got)
		}
		if got := encrypted.str("subtype"); got != "message" {
			t.Errorf("subtype = %q, want message", got)
		}
	})

	t.Run("nothing is emitted without a signature", func(t *testing.T) {
		// Additive: a model that returns no encrypted reasoning must produce
		// exactly the stream it did before.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, reasoningEvent(t, "weighing", nil, false), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		for _, ev := range parseSSEEvents(rec.Body.String()) {
			if ev.Type == events.EventTypeReasoningEncryptedValue {
				t.Error("REASONING_ENCRYPTED_VALUE emitted with no signature present")
			}
		}
	})

	t.Run("a repeated signature is emitted once", func(t *testing.T) {
		// ADK partials carry accumulated state, so the same blob can arrive on
		// several events for one reasoning message.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		for _, ev := range []*session.Event{
			reasoningEvent(t, "weigh", []byte("blob"), true),
			reasoningEvent(t, "weighing", []byte("blob"), true),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		var n int
		for _, ev := range parseSSEEvents(rec.Body.String()) {
			if ev.Type == events.EventTypeReasoningEncryptedValue {
				n++
			}
		}
		if n != 1 {
			t.Errorf("got %d REASONING_ENCRYPTED_VALUE events, want 1 for one repeated blob", n)
		}
	})

	t.Run("a changed signature is emitted again", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		for _, ev := range []*session.Event{
			reasoningEvent(t, "weigh", []byte("first"), true),
			reasoningEvent(t, "weighing", []byte("second"), true),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		var n int
		for _, ev := range parseSSEEvents(rec.Body.String()) {
			if ev.Type == events.EventTypeReasoningEncryptedValue {
				n++
			}
		}
		if n != 2 {
			t.Errorf("got %d REASONING_ENCRYPTED_VALUE events, want 2 for two distinct blobs", n)
		}
	})
}

// TestEncryptedReasoningSurvivesHistory covers the second half of FR2: the blob
// has to come back on the reconstructed message, or it is lost the moment a
// client reloads a thread and the model loses its reasoning continuity.
func TestEncryptedReasoningSurvivesHistory(t *testing.T) {
	t.Run("assistant message carries the blob", func(t *testing.T) {
		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "test-app"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				{Text: "weighing", Thought: true, ThoughtSignature: []byte("blob")},
				{Text: "Here you go"},
			},
		}

		msgs := convertEventForTest(t, ev)
		want := base64.StdEncoding.EncodeToString([]byte("blob"))
		var found bool
		for _, m := range msgs {
			if m.Role != types.RoleAssistant {
				continue
			}
			found = true
			if m.EncryptedValue != want {
				t.Errorf("assistant message EncryptedValue = %q, want %q", m.EncryptedValue, want)
			}
		}
		if !found {
			t.Fatalf("no assistant message reconstructed; got %+v", msgs)
		}
	})

	t.Run("nothing is added without a signature", func(t *testing.T) {
		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "test-app"
		ev.Content = genai.NewContentFromText("Here you go", genai.RoleModel)

		for _, m := range convertEventForTest(t, ev) {
			if m.EncryptedValue != "" {
				t.Errorf("message carries encryptedValue %q with no signature present", m.EncryptedValue)
			}
		}
	})
}

// convertEventForTest runs one event through the history conversion path.
func convertEventForTest(t *testing.T, ev *session.Event) []types.Message {
	t.Helper()
	msgs, err := convertEvent(context.Background(), ev, &convertConfig{})
	if err != nil {
		t.Fatalf("convertEvent() error = %v", err)
	}
	return msgs
}
