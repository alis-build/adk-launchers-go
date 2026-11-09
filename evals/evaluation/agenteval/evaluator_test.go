package agenteval_test

import (
	"context"
	"iter"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/agenteval"
	"go.alis.build/adk/launchers/evals/evaluation/generator"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestEvaluateEvalSetRequiresService(t *testing.T) {
	_, err := agenteval.EvaluateEvalSet(context.Background(), nil, "app", &models.EvalSet{EvalSetID: "set1"}, models.DefaultEvalConfig(), 1)
	if err == nil {
		t.Fatal("expected error for nil service")
	}
}

// informationalEvaluator reports a value but no verdict, like adk-python's
// efficiency metrics.
type informationalEvaluator struct{ score float64 }

func (e informationalEvaluator) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (metrics.EvaluationResult, error) {
	score := e.score
	res := metrics.EvaluationResult{OverallScore: &score, OverallEvalStatus: models.EvalStatusInformational}
	for _, inv := range actual {
		res.PerInvocationResults = append(res.PerInvocationResults, metrics.PerInvocationResult{
			ActualInvocation: inv, Score: &score, EvalStatus: models.EvalStatusInformational,
		})
	}
	return res, nil
}

// newEvalService builds a LocalEvalService over a real runtime whose agent
// answers "ok", with set1 holding one single-turn case.
func newEvalService(t *testing.T, registry *metrics.Registry) (*service.LocalEvalService, *models.EvalSet) {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: "app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "app"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})
	set, err := sets.GetEvalSet("app", "set1")
	if err != nil {
		t.Fatalf("GetEvalSet: %v", err)
	}
	return &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        sets,
		Registry:    registry,
		SimProvider: simulation.UserSimulatorProvider{},
	}, set
}

// TestEvaluateEvalSetKeepsInformationalMetricOutOfVerdict mirrors adk-python's
// test_agent_evaluator.py case for tool_call_count_v1: an informational value
// is reported but never passes or fails the case.
func TestEvaluateEvalSetKeepsInformationalMetricOutOfVerdict(t *testing.T) {
	registry := metrics.NewDefaultRegistry()
	registry.RegisterEvaluator(models.MetricInfo{MetricName: "tool_call_count_v1"},
		func(models.EvalMetric, *metrics.RegistryConfig) (metrics.Evaluator, error) {
			return informationalEvaluator{score: 3}, nil
		})
	svc, set := newEvalService(t, registry)
	cfg, err := models.UnmarshalEvalConfig([]byte(`{"criteria": {"tool_call_count_v1": 0}}`))
	if err != nil {
		t.Fatalf("UnmarshalEvalConfig: %v", err)
	}

	results, err := agenteval.EvaluateEvalSet(context.Background(), svc, "app", set, cfg, 2)
	if err != nil {
		t.Fatalf("EvaluateEvalSet: %v", err)
	}
	if len(results) != 1 || len(results[0].OverallEvalMetricResults) != 1 {
		t.Fatalf("results = %+v, want one case with one metric", results)
	}
	m := results[0].OverallEvalMetricResults[0]
	if m.Score == nil {
		t.Fatalf("tool_call_count_v1 has no score, want 3")
	}
	if *m.Score != 3 || m.EvalStatus != models.EvalStatusInformational {
		t.Errorf("tool_call_count_v1 = score %v, status %d; want 3, %d", *m.Score, m.EvalStatus, models.EvalStatusInformational)
	}
	if results[0].FinalEvalStatus != models.EvalStatusNotEvaluated {
		t.Errorf("case FinalEvalStatus = %d, want %d (no metric gave a verdict)", results[0].FinalEvalStatus, models.EvalStatusNotEvaluated)
	}
}
