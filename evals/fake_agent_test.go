package evals

import (
	"context"
	"iter"
	"testing"
	"time"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// gate blocks each caller of enter until released.
type gate struct {
	arrived chan struct{}
	release chan struct{}
}

func newGate() *gate {
	return &gate{arrived: make(chan struct{}, 16), release: make(chan struct{})}
}

func (g *gate) enter() {
	g.arrived <- struct{}{}
	<-g.release
}

// waitArrived waits for one caller to enter, failing after 5 s.
func (g *gate) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-g.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a caller to enter")
	}
}

// assertAlone fails if a second caller enters within 100 ms. With the limit
// honoured none ever can while one is held, so this cannot flake; without
// it, 100 ms is ample for the others to arrive.
func (g *gate) assertAlone(t *testing.T) {
	t.Helper()
	select {
	case <-g.arrived:
		t.Error("a second caller entered while one was held")
	case <-time.After(100 * time.Millisecond):
	}
}

// releaseOne lets one held caller out, failing after 5 s.
func (g *gate) releaseOne(t *testing.T) {
	t.Helper()
	select {
	case g.release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out releasing a caller")
	}
}

// runOneAtATime admits n callers in turn, checking each enters alone.
func (g *gate) runOneAtATime(t *testing.T, n int) {
	t.Helper()
	for range n {
		g.waitArrived(t)
		g.assertAlone(t)
		g.releaseOne(t)
	}
}

// waitDone waits for a background run's result, failing after 5 s.
func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the run to finish")
		return nil
	}
}

// gatedAgentRun is an agent run that waits at g, then replies "ok".
func gatedAgentRun(g *gate, author string) func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			if g != nil {
				g.enter()
			}
			ev := session.NewEvent(ctx, ctx.InvocationID())
			ev.Author = author
			ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
			yield(ev, nil)
		}
	}
}

// gatedEvaluator scores every invocation 1 after waiting at g.
type gatedEvaluator struct{ g *gate }

func (e gatedEvaluator) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (metrics.EvaluationResult, error) {
	e.g.enter()
	one := 1.0
	res := metrics.EvaluationResult{OverallScore: &one, OverallEvalStatus: models.EvalStatusPassed}
	for _, inv := range actual {
		res.PerInvocationResults = append(res.PerInvocationResults, metrics.PerInvocationResult{ActualInvocation: inv, Score: &one, EvalStatus: models.EvalStatusPassed})
	}
	return res, nil
}

// gatedRegistry is the default registry plus a metric, "gated", that scores
// through g.
func gatedRegistry(g *gate) *metrics.Registry {
	r := metrics.NewDefaultRegistry()
	r.RegisterEvaluator(models.MetricInfo{MetricName: "gated"}, func(models.EvalMetric, *metrics.RegistryConfig) (metrics.Evaluator, error) {
		return gatedEvaluator{g}, nil
	})
	return r
}

// gatedLauncher is a test launcher whose agent waits at agentGate (nil for
// none), scoring with registry, with set1 holding three static cases.
func gatedLauncher(t *testing.T, agentGate *gate, registry *metrics.Registry) *evalsLauncher {
	t.Helper()
	dir := t.TempDir()
	sets := storage.NewLocalEvalSetsManager(dir)
	a, err := agent.New(agent.Config{Name: "my_app", Run: gatedAgentRun(agentGate, "agent")})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	l := NewLauncher(WithAgentsDir(dir), WithEvalSetsManager(sets), WithEvalSetResultsManager(storage.NewLocalEvalSetResultsManager(dir)), WithMetricRegistry(registry)).(*evalsLauncher)
	l.launcherCfg = &launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}
	if _, err := sets.CreateEvalSet("my_app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	for _, id := range []string{"case1", "case2", "case3"} {
		if err := sets.AddEvalCase("my_app", "set1", models.EvalCase{EvalID: id, Conversation: []models.Invocation{{
			UserContent: genai.NewContentFromText("hi", genai.RoleUser), FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}}}); err != nil {
			t.Fatalf("AddEvalCase: %v", err)
		}
	}
	return l
}
