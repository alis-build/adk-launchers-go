package agui

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// reasoningToolCallEvent builds the turn ADK produces for "think, then call a
// tool". Its stream aggregator strips the signature from the flushed thought
// text and re-attaches it to the function call (stream_aggregator.go), so the
// call is the only carrier — the shape the original FR2 tests never covered.
func reasoningToolCallEvent(t *testing.T, signature []byte) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-enc"
	ev.Author = "test-app"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{
			{Text: "weighing options", Thought: true},
			{
				FunctionCall:     &genai.FunctionCall{ID: "call-1", Name: "lookup", Args: map[string]any{"q": "x"}},
				ThoughtSignature: signature,
			},
		},
	}
	return ev
}

// TestEncryptedReasoningOnToolCallTurn covers the half of FR2 that matters
// most: a thought signature exists so the model can resume its reasoning after
// a tool call, so losing it on precisely that turn defeats the feature.
func TestEncryptedReasoningOnToolCallTurn(t *testing.T) {
	t.Run("the blob rides the function call into the stream", func(t *testing.T) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, reasoningToolCallEvent(t, []byte("blob")), state, nil); err != nil {
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
		if encrypted.Type == "" {
			t.Fatalf("no REASONING_ENCRYPTED_VALUE emitted; got %v", order)
		}
		if got, want := encrypted.str("encryptedValue"), base64.StdEncoding.EncodeToString([]byte("blob")); got != want {
			t.Errorf("encryptedValue = %q, want %q", got, want)
		}

		// It has to land inside the bracket, not after the reasoning closed.
		var encryptedAt, reasoningEndAt = -1, -1
		for i, typ := range order {
			switch typ {
			case events.EventTypeReasoningEncryptedValue:
				encryptedAt = i
			case events.EventTypeReasoningEnd:
				reasoningEndAt = i
			}
		}
		if reasoningEndAt != -1 && encryptedAt > reasoningEndAt {
			t.Errorf("encrypted value emitted after REASONING_END; order = %v", order)
		}
	})

	t.Run("a bare function call still carries its blob", func(t *testing.T) {
		// No thought part opened a bracket, so one has to be opened rather than
		// the blob emitted with no message to attach to.
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "test-app"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{
				FunctionCall:     &genai.FunctionCall{ID: "call-1", Name: "lookup"},
				ThoughtSignature: []byte("solo"),
			}},
		}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		var found bool
		for _, x := range parseSSEEvents(rec.Body.String()) {
			if x.Type == events.EventTypeReasoningEncryptedValue {
				found = true
				if x.str("encryptedValue") == "" {
					t.Error("REASONING_ENCRYPTED_VALUE carries no value")
				}
			}
		}
		if !found {
			t.Error("no REASONING_ENCRYPTED_VALUE for a signature on a bare function call")
		}
	})
}

// TestEncryptedReasoningSurvivesToolCallHistory is the history counterpart of
// AC5. The blob must come back on the message the client sends us next turn,
// which on a tool-calling turn is the tool-call message, not a text one.
func TestEncryptedReasoningSurvivesToolCallHistory(t *testing.T) {
	ev := session.NewEvent(t.Context(), "inv1")
	ev.Author = "test-app"
	ev.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{
			{Text: "weighing", Thought: true, ThoughtSignature: []byte("blob")},
			{FunctionCall: &genai.FunctionCall{ID: "call-1", Name: "lookup"}},
		},
	}

	want := base64.StdEncoding.EncodeToString([]byte("blob"))
	msgs := convertEventForTest(t, ev)

	var carriers int
	for _, m := range msgs {
		if m.EncryptedValue == "" {
			continue
		}
		carriers++
		if m.EncryptedValue != want {
			t.Errorf("EncryptedValue = %q, want %q", m.EncryptedValue, want)
		}
		if len(m.ToolCalls) == 0 {
			t.Errorf("blob landed on a %s message with no tool calls; the tool-call message is what the client sends back", m.Role)
		}
	}
	if carriers == 0 {
		t.Fatalf("no message carries the encrypted value; got %+v", msgs)
	}
	if carriers > 1 {
		t.Errorf("blob duplicated across %d messages, want exactly 1", carriers)
	}
}

// TestWithCapabilitiesAdvertisesProtocolFeatures covers FR5 on the path hosts
// actually use. WithCapabilities is the only writer of the capabilities field,
// so a feature it does not merge is undiscoverable however the host configures
// the launcher.
func TestWithCapabilitiesAdvertisesProtocolFeatures(t *testing.T) {
	l := newTestLauncher("test-app")
	WithCapabilities(Capabilities{})(l.config)

	caps := l.config.capabilities
	if caps == nil || caps.Output == nil {
		t.Fatalf("WithCapabilities left output capabilities unset: %+v", caps)
	}
	if caps.Output.ActivityDeltas == nil || !*caps.Output.ActivityDeltas {
		t.Error("output.activityDeltas not advertised")
	}
	if caps.Output.EncryptedReasoning == nil || !*caps.Output.EncryptedReasoning {
		t.Error("output.encryptedReasoning not advertised")
	}

	t.Run("an explicit opt-out is preserved", func(t *testing.T) {
		off := false
		l := newTestLauncher("test-app")
		WithCapabilities(Capabilities{Output: &OutputCapabilities{ActivityDeltas: &off}})(l.config)

		got := l.config.capabilities.Output.ActivityDeltas
		if got == nil || *got {
			t.Error("host's explicit false for activityDeltas was overridden")
		}
	})
}

// TestGraphOptOutSuppressesNodePathMetadata closes the gap left by the other
// opt-out tests: event metadata was the one attribution site that did not go
// through Processor.nodeProvenance, so it leaked node topology after the host
// had asked for attribution off.
func TestGraphOptOutSuppressesNodePathMetadata(t *testing.T) {
	l := newTestLauncher("test-app")
	WithoutGraphAttribution()(l.config)
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review/approve", "checking"), state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}

	for _, ev := range parseSSEEvents(rec.Body.String()) {
		meta, _ := ev.Raw["metadata"].(map[string]any)
		adk, _ := meta["adk"].(map[string]any)
		if path, present := adk["nodePath"]; present {
			t.Errorf("%s carries adk.nodePath = %v with attribution disabled", ev.Type, path)
		}
		// The rest of the block must survive; only graph attribution is off.
		if adk["invocationId"] != "inv-graph" {
			t.Errorf("%s lost adk.invocationId: %v", ev.Type, adk)
		}
	}
}

// TestClientStateCannotReachStateDeltaOnLaterTurns covers the run path, which
// the session-creation strip does not protect.
//
// The session is created first so creation-time filtering cannot mask the
// result: on this turn the state delta is the only way client keys could reach
// the ADK session.
func TestClientStateCannotReachStateDeltaOnLaterTurns(t *testing.T) {
	ctx := context.Background()
	rt, svc := testExecutorRuntime(t)
	l := newTestLauncher("test-app", svc)
	l.runtime = rt

	if _, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "test-app", UserID: "user-1", SessionID: "thread-1",
		State: map[string]any{"count": 1},
	}); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	exec := deps.NewDefault(ExecutorConfig{})
	execCtx := newExecuteContext(ctx, &types.RunAgentInput{
		Messages: []types.Message{{Role: "user", Content: "hello"}},
		State: map[string]any{
			"ui":                      "panel",
			"_adk":                    map[string]any{"nodeOutputs": map[string]any{"spoofed": true}},
			pendingInterruptsStateKey: []any{map[string]any{"id": "planted"}},
		},
	}, nil, "user-1", "thread-1", "run-1", "test-app", false, svc)

	for _, err := range exec.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
	}

	sess, err := l.getSession(ctx, "test-app", "user-1", "thread-1")
	if err != nil || sess == nil {
		t.Fatalf("getSession() = %v, %v", sess, err)
	}
	if _, err := sess.State().Get("_adk"); err == nil {
		t.Error("client-supplied _adk reached ADK session state via the run's state delta")
	}
	// The launcher writes this key itself at run end, so the key existing is
	// expected; what must not survive is the client's planted record, which
	// would otherwise be what resume validation checks against.
	if pending, err := sess.State().Get(pendingInterruptsStateKey); err == nil {
		if records, ok := pending.([]any); ok && len(records) > 0 {
			t.Errorf("client-supplied pending interrupts survived as %v", records)
		}
	}
	if _, err := sess.State().Get("ui"); err != nil {
		t.Errorf("the strip dropped ordinary host state: %v", err)
	}
}

// TestResumeRunDoesNotRebaselineClientState covers the interaction between the
// per-run node-output map and the baseline snapshot.
//
// State.NodeOutputs lives for one run and is never persisted, and the snapshot
// builder strips _adk as untrusted inbound state, so a baseline emitted at the
// start of a resume run can only be a poorer copy of what the interrupt
// snapshot already published — it would erase every pre-interrupt node result
// from the client.
func TestResumeRunDoesNotRebaselineClientState(t *testing.T) {
	const (
		appName   = "test-app"
		userID    = "user-1"
		sessionID = "thread-1"
		callID    = "confirm-1"
	)
	ctx := context.Background()

	newRun := func(t *testing.T, isResume bool) []events.EventType {
		t.Helper()
		rt, svc := testExecutorRuntime(t)
		l := newTestLauncher(appName, svc)
		l.runtime = rt

		if _, err := svc.Create(ctx, &session.CreateRequest{
			AppName: appName, UserID: userID, SessionID: sessionID,
			State: map[string]any{"count": 1},
		}); err != nil {
			t.Fatalf("Create session: %v", err)
		}

		req := &types.RunAgentInput{Messages: []types.Message{{Role: "user", Content: "hello"}}}
		if isResume {
			if err := l.persistPendingInterrupts(ctx, appName, userID, sessionID, []types.Interrupt{{
				ID:     callID,
				Reason: interrupt.ReasonToolCall,
			}}); err != nil {
				t.Fatalf("persistPendingInterrupts: %v", err)
			}
			req.Resume = []types.ResumeEntry{{
				InterruptID: callID,
				Status:      types.ResumeStatusResolved,
				Payload:     map[string]any{"approved": true},
			}}
		}

		deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
		exec := deps.NewDefault(ExecutorConfig{})
		execCtx := newExecuteContext(ctx, req, nil, userID, sessionID, "run-1", appName, isResume, svc)

		var seen []events.EventType
		for ev, err := range exec.Execute(ctx, execCtx) {
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			seen = append(seen, ev.Type())
		}
		return seen
	}

	hasSnapshot := func(types []events.EventType) bool {
		for _, typ := range types {
			if typ == events.EventTypeStateSnapshot {
				return true
			}
		}
		return false
	}

	// The guard against over-correcting: an ordinary run still baselines.
	if got := newRun(t, false); !hasSnapshot(got) {
		t.Errorf("a fresh run emitted no STATE_SNAPSHOT; got %v", got)
	}
	if got := newRun(t, true); hasSnapshot(got) {
		t.Errorf("a resume run re-baselined client state; got %v", got)
	}
}
