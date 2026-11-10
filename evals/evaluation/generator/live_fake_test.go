package generator

import (
	"context"
	"iter"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// fakeLiveAgent is an agent that answers live sessions without a model:
// ADK's llmagent needs a real genai client for live, and the runner only
// needs the root agent to implement RunLive.
type fakeLiveAgent struct {
	agent.Agent
	sess *fakeLiveSession
}

// fakeLiveSession records every request. Each request with content, and each
// end of user activity, gets an output transcription event and then a final
// "reply" event that completes the turn.
type fakeLiveSession struct {
	mu        sync.Mutex
	requests  []agent.LiveRequest
	turns     chan struct{}
	closeOnce sync.Once
}

func (s *fakeLiveSession) Send(req agent.LiveRequest) error {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	if _, end := req.RealtimeInput.(*genai.ActivityEnd); req.Content != nil || end {
		s.turns <- struct{}{}
	}
	return nil
}

func (s *fakeLiveSession) Close() error {
	s.closeOnce.Do(func() { close(s.turns) })
	return nil
}

// recorded returns a copy of the requests sent so far.
func (s *fakeLiveSession) recorded() []agent.LiveRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agent.LiveRequest(nil), s.requests...)
}

func (a *fakeLiveAgent) RunLive(ctx agent.InvocationContext) (agent.LiveSession, iter.Seq2[*session.Event, error], error) {
	events := func(yield func(*session.Event, error) bool) {
		for range a.sess.turns {
			transcript := session.NewEvent(ctx, ctx.InvocationID())
			transcript.Author = "reply-agent"
			transcript.OutputTranscription = &genai.Transcription{Text: "transcribed reply"}
			if !yield(transcript, nil) {
				return
			}
			final := session.NewEvent(ctx, ctx.InvocationID())
			final.Author = "reply-agent"
			final.Content = genai.NewContentFromText("reply", genai.RoleModel)
			final.TurnComplete = true
			if !yield(final, nil) {
				return
			}
		}
	}
	return a.sess, events, nil
}

// newFakeLiveRuntime returns a runtime whose root agent is a fakeLiveAgent,
// and that agent's session.
func newFakeLiveRuntime(t *testing.T) (*adkrun.Runtime, *fakeLiveSession) {
	t.Helper()
	base, err := agent.New(agent.Config{
		Name: "reply-agent",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	sess := &fakeLiveSession{turns: make(chan struct{}, 16)}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(&fakeLiveAgent{Agent: base, sess: sess}),
		SessionService: session.InMemoryService(),
	}, "reply-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt, sess
}

// Proves the seam: a live eval runs end to end against fakeLiveAgent.
func TestGenerateInferencesLiveRunsAgainstFakeLiveAgent(t *testing.T) {
	rt, sess := newFakeLiveRuntime(t)
	sim := simulation.NewStaticUserSimulator([]models.Invocation{
		{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)},
	})
	inv, err := (&Generator{Runtime: rt}).GenerateInferences(context.Background(), InferenceOptions{
		UserSimulator:      sim,
		UseLive:            true,
		LiveTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if len(inv) != 1 || inv[0].FinalResponse == nil || len(inv[0].FinalResponse.Parts) == 0 || inv[0].FinalResponse.Parts[0].Text != "reply" {
		t.Fatalf("invocations = %+v, want one with final response \"reply\"", inv)
	}
	reqs := sess.recorded()
	if len(reqs) != 1 || reqs[0].Content == nil || reqs[0].Content.Parts[0].Text != "Hello" {
		t.Errorf("requests = %+v, want one with content \"Hello\"", reqs)
	}
}
