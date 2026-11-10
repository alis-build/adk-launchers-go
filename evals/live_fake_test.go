package evals

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// fakeLiveAgent answers live sessions without a model (a copy of the
// generator package's, kept DAMP): each request with content, and each end of
// user activity, gets a transcript event and a final "reply". Its non-live
// run also replies "reply". It records every request across sessions.
type fakeLiveAgent struct {
	agent.Agent
	mu       sync.Mutex
	runLives int
	requests []agent.LiveRequest
}

type fakeLiveSession struct {
	a         *fakeLiveAgent
	turns     chan struct{}
	closeOnce sync.Once
}

func (s *fakeLiveSession) Send(req agent.LiveRequest) error {
	s.a.mu.Lock()
	s.a.requests = append(s.a.requests, req)
	s.a.mu.Unlock()
	if _, end := req.RealtimeInput.(*genai.ActivityEnd); req.Content != nil || end {
		s.turns <- struct{}{}
	}
	return nil
}

func (s *fakeLiveSession) Close() error {
	s.closeOnce.Do(func() { close(s.turns) })
	return nil
}

func (a *fakeLiveAgent) RunLive(ctx agent.InvocationContext) (agent.LiveSession, iter.Seq2[*session.Event, error], error) {
	a.mu.Lock()
	a.runLives++
	a.mu.Unlock()
	sess := &fakeLiveSession{a: a, turns: make(chan struct{}, 16)}
	events := func(yield func(*session.Event, error) bool) {
		for range sess.turns {
			transcript := session.NewEvent(ctx, ctx.InvocationID())
			transcript.Author = "my_app"
			transcript.OutputTranscription = &genai.Transcription{Text: "transcribed reply"}
			if !yield(transcript, nil) {
				return
			}
			final := session.NewEvent(ctx, ctx.InvocationID())
			final.Author = "my_app"
			final.Content = genai.NewContentFromText("reply", genai.RoleModel)
			final.TurnComplete = true
			if !yield(final, nil) {
				return
			}
		}
	}
	return sess, events, nil
}

// liveCalls returns how many live sessions ran and the requests they got.
func (a *fakeLiveAgent) liveCalls() (int, []agent.LiveRequest) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runLives, append([]agent.LiveRequest(nil), a.requests...)
}

// fakeAudioModel speaks every request as the audio "WAV".
type fakeAudioModel struct{}

func (fakeAudioModel) Name() string { return "fake-tts" }

func (fakeAudioModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{InlineData: &genai.Blob{Data: []byte("WAV"), MIMEType: "audio/pcm;rate=16000"}}}}}, nil)
	}
}

// resolveFakeTTS resolves "fake-tts" to fakeAudioModel and fails for any other name.
func resolveFakeTTS(_ context.Context, name string) (model.LLM, error) {
	if name != "fake-tts" {
		return nil, errors.New("no such model")
	}
	return fakeAudioModel{}, nil
}

// liveLauncher is a test launcher on a fakeLiveAgent named my_app, whose set1
// holds one static case, "Book me a flight.", with the provider p.
func liveLauncher(t *testing.T, p simulation.UserSimulatorProvider) (*evalsLauncher, *fakeLiveAgent) {
	t.Helper()
	base, err := agent.New(agent.Config{Name: "my_app", Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			ev := session.NewEvent(ctx, ctx.InvocationID())
			ev.Author = "my_app"
			ev.Content = genai.NewContentFromText("reply", genai.RoleModel)
			yield(ev, nil)
		}
	}})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	a := &fakeLiveAgent{Agent: base}
	dir := t.TempDir()
	sets := storage.NewLocalEvalSetsManager(dir)
	l := NewLauncher(WithAgentsDir(dir), WithEvalSetsManager(sets), WithEvalSetResultsManager(storage.NewLocalEvalSetResultsManager(dir)), WithUserSimulatorProvider(p)).(*evalsLauncher)
	l.launcherCfg = &launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}
	if _, err := sets.CreateEvalSet("my_app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	if err := sets.AddEvalCase("my_app", "set1", models.EvalCase{EvalID: "case1", Conversation: []models.Invocation{{
		UserContent: genai.NewContentFromText("Book me a flight.", genai.RoleUser), FinalResponse: genai.NewContentFromText("reply", genai.RoleModel),
	}}}); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}
	return l, a
}
