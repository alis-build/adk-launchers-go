package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// subagentTrace summarises the subagent stream as "start:<name>" and
// "finish:<outcome>" entries in emission order.
func subagentTrace(t *testing.T, evts []sseEvent) []string {
	t.Helper()
	var out []string
	for _, ev := range evts {
		switch ev.Type {
		case events.EventTypeSubagentStarted:
			out = append(out, "start:"+ev.str("name"))
		case events.EventTypeSubagentFinished:
			outcome, _ := ev.Raw["outcome"].(map[string]any)
			typ, _ := outcome["type"].(string)
			out = append(out, "finish:"+typ)
		}
	}
	return out
}

// agentEvent builds a streamed text event from a named agent on a branch.
func agentEvent(t *testing.T, author, branch, text string) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "inv-sub"
	ev.Author = author
	ev.Branch = branch
	ev.Content = genai.NewContentFromText(text, genai.RoleModel)
	ev.Partial = true
	return ev
}

func TestSubagentEvents(t *testing.T) {
	setup := func() (*aguiLauncher, eventSink, *streamState, func() []sseEvent) {
		l := newTestLauncher("test-app")
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
		return l, e, state, func() []sseEvent { return parseSSEEvents(rec.Body.String()) }
	}

	t.Run("a sub-agent taking over starts a subagent run", func(t *testing.T) {
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "searching"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := subagentTrace(t, read())
		if len(got) != 1 || got[0] != "start:researcher" {
			t.Errorf("subagent trace = %v, want one start:researcher", got)
		}
	})

	t.Run("the root agent is not a subagent", func(t *testing.T) {
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "test-app", "", "thinking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		if got := subagentTrace(t, read()); len(got) != 0 {
			t.Errorf("subagent trace = %v, want none for the root agent", got)
		}
	})

	t.Run("consecutive events from one sub-agent do not restart it", func(t *testing.T) {
		l, e, state, read := setup()
		for i := 0; i < 3; i++ {
			if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "tick"), state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}
		if got := subagentTrace(t, read()); len(got) != 1 {
			t.Errorf("subagent trace = %v, want a single start", got)
		}
	})

	t.Run("peer sub-agents on different branches are different runs", func(t *testing.T) {
		// Branch is what ADK uses to keep peer sub-agents from seeing each
		// other's history, so two activations of the same agent on different
		// branches are distinct runs rather than one continuing.
		l, e, state, read := setup()
		for _, ev := range []*session.Event{
			agentEvent(t, "researcher", "b1", "one"),
			agentEvent(t, "researcher", "b2", "two"),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		got := subagentTrace(t, read())
		want := []string{"start:researcher", "finish:success", "start:researcher"}
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("subagent trace = %v, want %v", got, want)
		}

		var ids []string
		for _, ev := range read() {
			if ev.Type == events.EventTypeSubagentStarted {
				ids = append(ids, ev.str("subagentRunId"))
			}
		}
		if len(ids) != 2 || ids[0] == ids[1] {
			t.Errorf("subagent run ids = %v, want two distinct ids", ids)
		}
	})

	t.Run("handing back to the root finishes the subagent", func(t *testing.T) {
		l, e, state, read := setup()
		for _, ev := range []*session.Event{
			agentEvent(t, "researcher", "b1", "searching"),
			agentEvent(t, "test-app", "", "here you go"),
		} {
			if _, err := l.processEvent(e, ev, state, nil); err != nil {
				t.Fatalf("processEvent() error = %v", err)
			}
		}

		got := subagentTrace(t, read())
		if len(got) != 2 || got[1] != "finish:success" {
			t.Errorf("subagent trace = %v, want start then finish:success", got)
		}
	})

	t.Run("events carry the run id of the subagent that produced them", func(t *testing.T) {
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "searching"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		var runID string
		for _, ev := range read() {
			if ev.Type == events.EventTypeSubagentStarted {
				runID = ev.str("subagentRunId")
			}
		}
		if runID == "" {
			t.Fatal("no SUBAGENT_STARTED emitted")
		}
		for _, ev := range read() {
			if ev.Type != events.EventTypeTextMessageContent {
				continue
			}
			if got := ev.str("subagentRunId"); got != runID {
				t.Errorf("TEXT_MESSAGE_CONTENT subagentRunId = %q, want %q", got, runID)
			}
		}
	})

	t.Run("root-agent events carry no run id", func(t *testing.T) {
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "test-app", "", "thinking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		for _, ev := range read() {
			if v, present := ev.Raw["subagentRunId"]; present {
				t.Errorf("%s carries subagentRunId %v from the root agent", ev.Type, v)
			}
		}
	})

	t.Run("a subagent paused by an interrupt finishes as suspended", func(t *testing.T) {
		// The SDK's suspended outcome names the interrupts the subagent owns,
		// which is what lets a client show which branch is waiting on a human.
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.InvocationID = "inv-sub"
		ev.Author = "researcher"
		ev.Branch = "b1"
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Send?", "orig-1", "send_email")},
		}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := read()
		got := subagentTrace(t, evts)
		if len(got) != 2 || got[1] != "finish:suspended" {
			t.Errorf("subagent trace = %v, want start then finish:suspended", got)
		}

		for _, x := range evts {
			if x.Type != events.EventTypeSubagentFinished {
				continue
			}
			outcome, _ := x.Raw["outcome"].(map[string]any)
			ids, _ := outcome["interruptIds"].([]any)
			if len(ids) != 1 || ids[0] != "confirm-1" {
				t.Errorf("suspended outcome interruptIds = %v, want [confirm-1]", ids)
			}
		}
	})

	t.Run("the subagent finish precedes the terminal event", func(t *testing.T) {
		l, e, state, read := setup()
		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "researcher"
		ev.Branch = "b1"
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Send?", "orig-1", "send_email")},
		}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		finishIdx, terminalIdx := -1, -1
		for i, x := range read() {
			switch x.Type {
			case events.EventTypeSubagentFinished:
				finishIdx = i
			case events.EventTypeRunFinished:
				terminalIdx = i
			}
		}
		if finishIdx == -1 || terminalIdx == -1 {
			t.Fatal("expected both SUBAGENT_FINISHED and RUN_FINISHED")
		}
		if finishIdx > terminalIdx {
			t.Errorf("SUBAGENT_FINISHED at %d follows RUN_FINISHED at %d", finishIdx, terminalIdx)
		}
	})
}

// TestSubagentHandoverDoesNotMisattribute guards the boundary itself.
//
// The outgoing producer's message has to close before the incoming
// SUBAGENT_STARTED, or the close is emitted while the new activation is open
// and gets stamped with a run id it never belonged to.
func TestSubagentHandoverDoesNotMisattribute(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

	for _, ev := range []*session.Event{
		agentEvent(t, "test-app", "", "root speaking"),
		agentEvent(t, "researcher", "b1", "sub speaking"),
	} {
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
	}

	evts := parseSSEEvents(rec.Body.String())
	var startIdx = -1
	for i, x := range evts {
		if x.Type == events.EventTypeSubagentStarted {
			startIdx = i
		}
	}
	if startIdx == -1 {
		t.Fatal("no SUBAGENT_STARTED emitted")
	}

	// The root's own close must land before the handover and carry no run id.
	for i, x := range evts {
		if x.Type != events.EventTypeTextMessageEnd {
			continue
		}
		if i > startIdx {
			continue // the sub-agent's own message, closed later
		}
		if v, present := x.Raw["subagentRunId"]; present {
			t.Errorf("the root agent's TEXT_MESSAGE_END was attributed to %v", v)
		}
	}
}

// TestWithoutSubagentAttribution covers the opt-out.
//
// Subagent brackets change the wire for every existing multi-agent stream, and
// the Go SDK's EventDecoder errors on an event type it does not know rather
// than skipping it. A consumer on an older SDK needs a way off without pinning
// the launcher back.
func TestWithoutSubagentAttribution(t *testing.T) {
	newDisabledLauncher := func() *aguiLauncher {
		l := newTestLauncher("test-app")
		WithoutSubagentAttribution()(l.config)
		return l
	}

	t.Run("suppresses the bracket events", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "searching"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		if got := subagentTrace(t, parseSSEEvents(rec.Body.String())); len(got) != 0 {
			t.Errorf("subagent trace = %v, want none with attribution disabled", got)
		}
	})

	t.Run("suppresses the run id on events", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "searching"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		for _, ev := range parseSSEEvents(rec.Body.String()) {
			if v, present := ev.Raw["subagentRunId"]; present {
				t.Errorf("%s carries subagentRunId %v with attribution disabled", ev.Type, v)
			}
		}
	})

	t.Run("suppresses the suspended bracket on an interrupt too", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "researcher"
		ev.Branch = "b1"
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Send?", "orig-1", "send_email")},
		}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}
		if got := subagentTrace(t, parseSSEEvents(rec.Body.String())); len(got) != 0 {
			t.Errorf("subagent trace = %v, want none with attribution disabled", got)
		}
	})

	t.Run("sub-agent steps and text still work", func(t *testing.T) {
		// The opt-out drops subagent identity, not the sub-agent's output or
		// the step bracketing that predates it.
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, agentEvent(t, "researcher", "b1", "searching"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		if got := stepNames(evts); len(got) != 1 || got[0] != "start:researcher" {
			t.Errorf("steps = %v, want one start:researcher", got)
		}
		var sawText bool
		for _, ev := range evts {
			if ev.Type == events.EventTypeTextMessageContent && ev.str("delta") == "searching" {
				sawText = true
			}
		}
		if !sawText {
			t.Error("the sub-agent's text was suppressed along with its attribution")
		}
	})

	t.Run("on by default", func(t *testing.T) {
		if newTestLauncher("test-app").config.subagentAttributionDisabled {
			t.Error("subagent attribution disabled on a zero config, want it on by default")
		}
	})
}
