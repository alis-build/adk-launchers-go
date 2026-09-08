package agui

import (
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// protoBaselineRun drives a run that uses none of the three features this track
// adds: no activity updates, no encrypted reasoning, no event metadata.
//
// It still exercises the surfaces those features attach to. Encrypted reasoning
// emits inside the reasoning bracket, and metadata would ride every event, so a
// run with reasoning and text is where an accidental non-additive change would
// show up.
func protoBaselineRun(t *testing.T) string {
	t.Helper()
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	drive := func(mutate func(ev *session.Event)) {
		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-proto"
		ev.Author = "test-app"
		mutate(ev)
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
	}

	drive(func(ev *session.Event) {
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{{Text: "weighing options", Thought: true}},
		}
		ev.Partial = true
	})
	drive(func(ev *session.Event) {
		ev.Content = genai.NewContentFromText("Here you go", genai.RoleModel)
		ev.Partial = true
	})
	drive(func(ev *session.Event) { ev.TurnComplete = true })

	return rec.Body.String()
}

// TestProtoFeatureFreeStreamBaseline pins a stream that uses none of this
// track's features, so the additive-only claim is checked rather than assumed.
//
// All three changes are meant to add events or fields only where the underlying
// data exists: no encrypted value means no REASONING_ENCRYPTED_VALUE, no
// activity means no ACTIVITY_*, and metadata attaches only what ADK supplies.
// A run without any of them must therefore be byte-identical when the track is
// done, which the phase12-regression-gate todo re-checks.
func TestProtoFeatureFreeStreamBaseline(t *testing.T) {
	// UPDATED for aguiproto_20260908 FR3, which attaches metadata to EVERY
	// event by design, so byte-identity across that change was never possible.
	// Before regenerating, the rest of the stream was proven unchanged: with the
	// metadata key stripped, this golden passed against its previous contents.
	// That is the only edit this golden has had.
	const want = `[
  {
    "messageId": "msg-1",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "REASONING_START"
  },
  {
    "messageId": "msg-2",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "role": "reasoning",
    "type": "REASONING_MESSAGE_START"
  },
  {
    "delta": "weighing options",
    "messageId": "msg-2",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "REASONING_MESSAGE_CONTENT"
  },
  {
    "messageId": "msg-2",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "REASONING_MESSAGE_END"
  },
  {
    "messageId": "msg-1",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "REASONING_END"
  },
  {
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "name": "test-app",
    "role": "assistant",
    "type": "TEXT_MESSAGE_START"
  },
  {
    "delta": "Here you go",
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "TEXT_MESSAGE_CONTENT"
  },
  {
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-proto"
      }
    },
    "type": "TEXT_MESSAGE_END"
  }
]`

	if got := stableStream(t, protoBaselineRun(t)); got != want {
		t.Errorf("feature-free stream changed shape.\ngot:\n%s\n\nwant:\n%s", got, want)
	}
}
