package generator

import (
	"context"
	"iter"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

// countingSummarizer records that compaction reached the summarizer.
type countingSummarizer struct {
	mu    sync.Mutex
	calls int
}

func (c *countingSummarizer) SummarizeEvents(context.Context, []*session.Event) (compaction.SummarizeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return compaction.SummarizeResult{Content: genai.NewContentFromText("a summary", genai.RoleModel)}, nil
}

func (c *countingSummarizer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestNewEvalRunnerActuallyRunsCompaction pins that the compaction config on
// [launcher.Config] reaches the runner the eval generator builds.
//
// The generator builds its own runner.Config rather than going through
// adkrun.RunSSE, so propagating the field in adkrun alone leaves eval inference
// uncompacted while every test still passes. The runner is driven through Run
// rather than RunLive because compaction is post-invocation bookkeeping that
// does not depend on the live transport, and RunLive would need a real model.
func TestNewEvalRunnerActuallyRunsCompaction(t *testing.T) {
	const appName = "compaction-agent"

	a, err := agent.New(agent.Config{
		Name: appName,
		Run: func(ictx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ictx, ictx.InvocationID())
				ev.Author = appName
				ev.Content = genai.NewContentFromText("an answer", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	summarizer := &countingSummarizer{}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		Compaction:     &compaction.Config{CompactionInterval: 1, Summarizer: summarizer},
	}, appName)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	interceptor, err := NewRequestInterceptor()
	if err != nil {
		t.Fatalf("NewRequestInterceptor: %v", err)
	}

	gen := &Generator{Runtime: rt}
	r, err := gen.newEvalRunner(appName, interceptor)
	if err != nil {
		t.Fatalf("newEvalRunner: %v", err)
	}

	const sessionID = "compaction-session"
	for range 2 {
		msg := genai.NewContentFromText("hello", genai.RoleUser)
		for _, evErr := range r.Run(t.Context(), "user-1", sessionID, msg, agent.RunConfig{}) {
			if evErr != nil {
				t.Fatalf("run event error: %v", evErr)
			}
		}
	}

	if summarizer.count() == 0 {
		t.Error("the summarizer was never called, so eval inference accepts a compaction config and does nothing with it")
	}
}
