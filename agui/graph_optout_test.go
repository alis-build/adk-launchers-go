package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// TestWithoutGraphAttribution covers FR4: hosts whose clients choke on
// unexpected STEP_* events can turn graph attribution off.
func TestWithoutGraphAttribution(t *testing.T) {
	newDisabledLauncher := func() *aguiLauncher {
		l := newTestLauncher("test-app")
		WithoutGraphAttribution()(l.config)
		return l
	}

	t.Run("suppresses node step events", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		for _, x := range parseSSEEvents(rec.Body.String()) {
			if x.Type == events.EventTypeStepStarted && x.str("stepName") == "review" {
				t.Errorf("node step %q emitted with attribution disabled", x.str("stepName"))
			}
		}
	})

	t.Run("suppresses node outputs", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := outputEvent(t, "reviewer", "review", map[string]any{"verdict": "ok"})
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		if got := nodeOutputsFromDeltas(t, parseSSEEvents(rec.Body.String())); got != nil {
			t.Errorf("_adk.nodeOutputs = %v, want none with attribution disabled", got)
		}
	})

	t.Run("suppresses node metadata on interrupts", func(t *testing.T) {
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "reviewer"
		ev.NodeInfo = &session.NodeInfo{Path: "review"}
		ev.Routes = []string{"publish"}
		ev.Content = &genai.Content{
			Role:  string(genai.RoleModel),
			Parts: []*genai.Part{confirmationPart("confirm-1", "Publish?", "orig-1", "publish")},
		}
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		evts := parseSSEEvents(rec.Body.String())
		intr := interruptsFromRunFinished(t, evts[len(evts)-1]).Interrupts[0]
		adkMeta, _ := intr.Metadata["adk"].(map[string]any)
		for _, key := range []string{"nodePath", "routes"} {
			if v, present := adkMeta[key]; present {
				t.Errorf("adk.%s present as %v with attribution disabled", key, v)
			}
		}
	})

	t.Run("author-driven steps still work", func(t *testing.T) {
		// The opt-out turns off graph attribution, not the sub-agent step
		// behaviour the launcher had before this track.
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		ev := session.NewEvent(t.Context(), "inv1")
		ev.Author = "researcher"
		ev.Content = genai.NewContentFromText("Searching", genai.RoleModel)
		ev.Partial = true
		if _, err := l.processEvent(e, ev, state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		got := stepNames(parseSSEEvents(rec.Body.String()))
		if len(got) != 1 || got[0] != "start:researcher" {
			t.Errorf("steps = %v, want one start:researcher", got)
		}
	})

	t.Run("a disabled workflow node still streams its text", func(t *testing.T) {
		// Suppressing attribution must not suppress the agent's actual output.
		l := newDisabledLauncher()
		e, rec := newTestEmitter()
		state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}

		if _, err := l.processEvent(e, nodeEvent(t, "reviewer", "review", "checking"), state, nil); err != nil {
			t.Fatalf("processEvent() error = %v", err)
		}

		var sawText bool
		for _, x := range parseSSEEvents(rec.Body.String()) {
			if x.Type == events.EventTypeTextMessageContent && x.str("delta") == "checking" {
				sawText = true
			}
		}
		if !sawText {
			t.Error("node text was suppressed along with its attribution")
		}
	})

	t.Run("on by default", func(t *testing.T) {
		l := newTestLauncher("test-app")
		if l.config.graphAttributionDisabled {
			t.Error("graph attribution is disabled on a zero config, want it on by default")
		}
	})
}
