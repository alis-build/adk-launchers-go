package agui

import (
	"encoding/json"
	"fmt"
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// stableStream renders an SSE body as canonical JSON for golden comparison.
//
// Timestamps are dropped as wall-clock noise, and generated ids (message and
// subagent run alike) are rewritten to msg-1, msg-2 ... in first-appearance
// order. The rewrite is a
// mapping rather than a blanket redaction, so the golden still pins which
// events share a message id: that is what proves partials reuse one message and
// a tool call points at the right parent.
func stableStream(t *testing.T, body string) string {
	t.Helper()

	ids := map[string]string{}
	stableID := func(v any) (string, bool) {
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		if mapped, seen := ids[s]; seen {
			return mapped, true
		}
		mapped := fmt.Sprintf("msg-%d", len(ids)+1)
		ids[s] = mapped
		return mapped, true
	}

	evts := parseSSEEvents(body)
	raws := make([]map[string]any, 0, len(evts))
	for _, ev := range evts {
		delete(ev.Raw, "timestamp")
		for _, key := range []string{"messageId", "parentMessageId", "subagentRunId"} {
			if v, ok := ev.Raw[key]; ok {
				if mapped, ok := stableID(v); ok {
					ev.Raw[key] = mapped
				}
			}
		}
		raws = append(raws, ev.Raw)
	}

	out, err := json.MarshalIndent(raws, "", "  ")
	if err != nil {
		t.Fatalf("stableStream: marshal failed: %v", err)
	}
	return string(out)
}

// plainAgentRun drives one representative non-workflow run through the
// processor and returns the whole SSE stream.
//
// It deliberately spans every mechanism graph attribution will touch: streamed
// text, the author-change step bracketing that already exists, a tool call and
// its result, a state delta, and turn completion.
func plainAgentRun(t *testing.T) string {
	t.Helper()
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	drive := func(mutate func(ev *session.Event)) {
		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-plain"
		ev.Author = "test-app"
		mutate(ev)
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
	}

	drive(func(ev *session.Event) {
		ev.Content = genai.NewContentFromText("Looking", genai.RoleModel)
		ev.Partial = true
	})
	drive(func(ev *session.Event) {
		ev.Content = genai.NewContentFromText(" it up", genai.RoleModel)
		ev.Partial = true
	})
	drive(func(ev *session.Event) {
		ev.Author = "researcher"
		ev.Content = genai.NewContentFromText("Searching", genai.RoleModel)
		ev.Partial = true
	})
	drive(func(ev *session.Event) {
		ev.Author = "researcher"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
				ID: "tc-1", Name: "search", Args: map[string]any{"q": "adk"},
			}}},
		}
	})
	drive(func(ev *session.Event) {
		ev.Author = "researcher"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
				ID: "tc-1", Name: "search", Response: map[string]any{"hits": 2},
			}}},
		}
	})
	drive(func(ev *session.Event) {
		ev.Actions.StateDelta["count"] = 1
	})
	drive(func(ev *session.Event) {
		ev.TurnComplete = true
	})

	return rec.Body.String()
}

// TestPlainAgentStreamBaseline pins the entire stream a non-workflow run
// produces, before graph attribution changes how steps and state are emitted.
//
// Graph attribution adds path-named STEP_* events and a reserved
// _adk.nodeOutputs state key. Both ride channels a plain agent already uses:
// steps today are driven by author changes, and node outputs share the
// STATE_DELTA channel. Without this golden, "nothing changes for plain agents"
// would be asserted only by tests that check individual fields and would pass
// through an extra step event or a stray delta.
//
// Every event here comes from an event with a nil NodeInfo, so this stream must
// be byte-identical when the track is done. The regression-gate todo re-runs it.
func TestPlainAgentStreamBaseline(t *testing.T) {
	// UPDATED AGAIN for subagent events: SUBAGENT_STARTED/FINISHED now bracket
	// each sub-agent activation and subagentRunId attributes their events, so
	// any stream with a sub-agent gains both. Verified first that nothing else
	// moved: with the SUBAGENT_* events dropped and subagentRunId stripped, this
	// golden passed against its previous contents.
	//
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
        "invocationId": "inv-plain"
      }
    },
    "name": "test-app",
    "role": "assistant",
    "type": "TEXT_MESSAGE_START"
  },
  {
    "delta": "Looking",
    "messageId": "msg-1",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-plain"
      }
    },
    "type": "TEXT_MESSAGE_CONTENT"
  },
  {
    "delta": " it up",
    "messageId": "msg-1",
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-plain"
      }
    },
    "type": "TEXT_MESSAGE_CONTENT"
  },
  {
    "messageId": "msg-1",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "type": "TEXT_MESSAGE_END"
  },
  {
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "name": "researcher",
    "subagentRunId": "msg-2",
    "type": "SUBAGENT_STARTED"
  },
  {
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "stepName": "researcher",
    "subagentRunId": "msg-2",
    "type": "STEP_STARTED"
  },
  {
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "name": "researcher",
    "role": "assistant",
    "subagentRunId": "msg-2",
    "type": "TEXT_MESSAGE_START"
  },
  {
    "delta": "Searching",
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "subagentRunId": "msg-2",
    "type": "TEXT_MESSAGE_CONTENT"
  },
  {
    "messageId": "msg-3",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "subagentRunId": "msg-2",
    "type": "TEXT_MESSAGE_END"
  },
  {
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "parentMessageId": "msg-3",
    "subagentRunId": "msg-2",
    "toolCallId": "tc-1",
    "toolCallName": "search",
    "type": "TOOL_CALL_START"
  },
  {
    "delta": "{\"q\":\"adk\"}",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "subagentRunId": "msg-2",
    "toolCallId": "tc-1",
    "type": "TOOL_CALL_ARGS"
  },
  {
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "subagentRunId": "msg-2",
    "toolCallId": "tc-1",
    "type": "TOOL_CALL_END"
  },
  {
    "content": "{\"hits\":2}",
    "messageId": "msg-4",
    "metadata": {
      "adk": {
        "author": "researcher",
        "invocationId": "inv-plain"
      }
    },
    "role": "tool",
    "subagentRunId": "msg-2",
    "toolCallId": "tc-1",
    "type": "TOOL_CALL_RESULT"
  },
  {
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-plain"
      }
    },
    "stepName": "researcher",
    "subagentRunId": "msg-2",
    "type": "STEP_FINISHED"
  },
  {
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-plain"
      }
    },
    "outcome": {
      "type": "success"
    },
    "subagentRunId": "msg-2",
    "type": "SUBAGENT_FINISHED"
  },
  {
    "delta": [
      {
        "op": "add",
        "path": "/count",
        "value": 1
      }
    ],
    "metadata": {
      "adk": {
        "author": "test-app",
        "invocationId": "inv-plain"
      }
    },
    "type": "STATE_DELTA"
  }
]`

	if got := stableStream(t, plainAgentRun(t)); got != want {
		t.Errorf("plain agent stream changed shape.\ngot:\n%s\n\nwant:\n%s", got, want)
	}
}
