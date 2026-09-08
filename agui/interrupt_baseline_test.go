package agui

import (
	"encoding/json"
	"testing"

	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// canonicalStream renders the SSE body as stable, indented JSON for golden
// comparison. Timestamps are dropped because they are wall-clock values; every
// other field is compared verbatim, so a reshaped stream shows up as a diff.
func canonicalStream(t *testing.T, body string) string {
	t.Helper()
	evts := parseSSEEvents(body)
	raws := make([]map[string]any, 0, len(evts))
	for _, ev := range evts {
		delete(ev.Raw, "timestamp")
		raws = append(raws, ev.Raw)
	}
	out, err := json.MarshalIndent(raws, "", "  ")
	if err != nil {
		t.Fatalf("canonicalStream: marshal failed: %v", err)
	}
	return string(out)
}

// TestConfirmationInterruptWireBaseline pins the entire adk_request_confirmation
// stream, byte for byte, before the FunctionCall branch is restructured into a
// dispatch table.
//
// The assertions elsewhere in this package check individual fields, which a
// restructure could satisfy while still reordering events, dropping a metadata
// key, or changing the advertised responseSchema. This golden is what makes the
// P2 refactor and spec acceptance criterion 6 (byte-identical tool_call resume)
// checkable: any change to the emitted stream fails here with a readable diff.
//
// A deliberate protocol change means updating this golden in the same commit.
func TestConfirmationInterruptWireBaseline(t *testing.T) {
	t.Run("hint and original call args", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "e-test-invocation"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{
					ID:   "confirm-1",
					Name: toolconfirmation.FunctionCallName,
					Args: map[string]any{
						"toolConfirmation": map[string]any{"hint": "Approve sending email?"},
						"originalFunctionCall": map[string]any{
							"ID":   "orig-fc-1",
							"Name": "send_email",
							"Args": map[string]any{"to": "a@b.com"},
						},
					},
				},
			}},
		}

		done, err := l.processEvent(e, ev, state, nil)
		if err != nil {
			t.Fatalf("processEvent() error = %v, want nil", err)
		}
		if !done {
			t.Fatal("processEvent() done = false, want true")
		}

		const want = `[
  {
    "toolCallId": "orig-fc-1",
    "toolCallName": "send_email",
    "type": "TOOL_CALL_START"
  },
  {
    "delta": "{\"to\":\"a@b.com\"}",
    "toolCallId": "orig-fc-1",
    "type": "TOOL_CALL_ARGS"
  },
  {
    "toolCallId": "orig-fc-1",
    "type": "TOOL_CALL_END"
  },
  {
    "outcome": {
      "interrupts": [
        {
          "id": "confirm-1",
          "message": "Approve sending email?",
          "metadata": {
            "adk": {
              "confirmationCallId": "confirm-1",
              "confirmationCallName": "adk_request_confirmation",
              "invocationId": "e-test-invocation"
            },
            "hitl": {
              "summary": "Approve sending email?"
            }
          },
          "reason": "tool_call",
          "responseSchema": {
            "properties": {
              "approved": {
                "type": "boolean"
              },
              "editedArgs": {
                "description": "Full replacement of the tool args. Not merged.",
                "type": "object"
              }
            },
            "required": [
              "approved"
            ],
            "type": "object"
          },
          "toolCallId": "orig-fc-1"
        }
      ],
      "type": "interrupt"
    },
    "runId": "r1",
    "threadId": "t1",
    "type": "RUN_FINISHED"
  }
]`

		if got := canonicalStream(t, rec.Body.String()); got != want {
			t.Errorf("confirmation stream changed shape.\ngot:\n%s\n\nwant:\n%s", got, want)
		}
	})

	t.Run("typed confirmation with payload and no hint", func(t *testing.T) {
		// Exercises the other two metadata branches: confirmationPayload is
		// carried through, and an empty hint emits no "hitl" summary block.
		//
		// This also pins a pre-existing wart: a tool with no args emits
		// TOOL_CALL_ARGS delta "null" rather than "{}", so a client calling
		// JSON.parse on it gets null instead of an empty object. Pinned as-is
		// because the P2 refactor must not change behaviour; fixing it is a
		// separate decision.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r2", ThreadID: "t2", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv2")
		ev.InvocationID = "inv-2"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{
					ID:   "confirm-2",
					Name: toolconfirmation.FunctionCallName,
					Args: map[string]any{
						"toolConfirmation": &toolconfirmation.ToolConfirmation{
							Payload: map[string]any{"amount": "42.00"},
						},
						"originalFunctionCall": map[string]any{
							"ID":   "orig-fc-2",
							"Name": "charge_card",
						},
					},
				},
			}},
		}

		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v, want nil", err)
		}

		const want = `[
  {
    "toolCallId": "orig-fc-2",
    "toolCallName": "charge_card",
    "type": "TOOL_CALL_START"
  },
  {
    "delta": "null",
    "toolCallId": "orig-fc-2",
    "type": "TOOL_CALL_ARGS"
  },
  {
    "toolCallId": "orig-fc-2",
    "type": "TOOL_CALL_END"
  },
  {
    "outcome": {
      "interrupts": [
        {
          "id": "confirm-2",
          "metadata": {
            "adk": {
              "confirmationCallId": "confirm-2",
              "confirmationCallName": "adk_request_confirmation",
              "confirmationPayload": {
                "amount": "42.00"
              },
              "invocationId": "inv-2"
            }
          },
          "reason": "tool_call",
          "responseSchema": {
            "properties": {
              "approved": {
                "type": "boolean"
              },
              "editedArgs": {
                "description": "Full replacement of the tool args. Not merged.",
                "type": "object"
              }
            },
            "required": [
              "approved"
            ],
            "type": "object"
          },
          "toolCallId": "orig-fc-2"
        }
      ],
      "type": "interrupt"
    },
    "runId": "r2",
    "threadId": "t2",
    "type": "RUN_FINISHED"
  }
]`

		if got := canonicalStream(t, rec.Body.String()); got != want {
			t.Errorf("confirmation stream changed shape.\ngot:\n%s\n\nwant:\n%s", got, want)
		}
	})
}
