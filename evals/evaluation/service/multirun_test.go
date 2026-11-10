package service_test

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

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

// okService returns a service whose agent answers "ok", over a set holding
// one static case per ID.
func okService(t *testing.T, caseIDs ...string) *service.LocalEvalService {
	t.Helper()
	sessSvc := session.InMemoryService()
	sets := storage.NewInMemoryEvalSetsManager()
	if _, err := sets.CreateEvalSet("app", "set1"); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	for _, id := range caseIDs {
		if err := sets.AddEvalCase("app", "set1", models.EvalCase{
			EvalID: id,
			Conversation: []models.Invocation{{
				UserContent:   genai.NewContentFromText("Hello", genai.RoleUser),
				FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
			}},
		}); err != nil {
			t.Fatalf("AddEvalCase: %v", err)
		}
	}
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
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: sessSvc}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
		Sessions:    sessSvc,
	}
}

// Mirrors adk-python test_local_eval_service.py:280: each case runs
// num_runs times, each run labelled with its repetition index.
func TestPerformInferenceRepeatsEachCaseNumRunsTimes(t *testing.T) {
	svc := okService(t, "case1", "case2")

	inf, err := svc.PerformInference(context.Background(), service.InferenceRequest{
		AppName: "app", EvalSetID: "set1",
		InferenceConfig: service.InferenceConfig{NumRuns: 3, Parallelism: 2},
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 6 {
		t.Fatalf("results = %d, want 6", len(inf))
	}
	reps := map[string][]int{}
	sessions := map[string]bool{}
	for _, r := range inf {
		if r.RepetitionIndex == nil {
			t.Fatalf("result %+v has no repetition index", r)
		}
		reps[r.EvalCaseID] = append(reps[r.EvalCaseID], *r.RepetitionIndex)
		sessions[r.SessionID] = true
	}
	for _, id := range []string{"case1", "case2"} {
		if got := reps[id]; len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
			t.Errorf("%s repetition indexes = %v, want [0 1 2]", id, got)
		}
	}
	if len(sessions) != 6 {
		t.Errorf("distinct session IDs = %d, want 6", len(sessions))
	}
}

func TestPerformInferenceSingleRunHasRepetitionIndexZero(t *testing.T) {
	inf, err := okService(t, "case1").PerformInference(context.Background(), service.InferenceRequest{AppName: "app", EvalSetID: "set1"})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].RepetitionIndex == nil || *inf[0].RepetitionIndex != 0 {
		t.Errorf("results = %+v, want one with repetition index 0", inf)
	}
}

// Acceptance 1 (first half): each run's case result carries its index.
func TestEvaluateCarriesRepetitionIndex(t *testing.T) {
	svc := okService(t, "case1")
	ctx := context.Background()
	inf, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName: "app", EvalSetID: "set1",
		InferenceConfig: service.InferenceConfig{NumRuns: 3},
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}

	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig:   service.EvaluateConfig{EvalMetrics: []models.EvalMetric{{MetricName: models.MetricToolTrajectoryAvgScore, Threshold: 1.0}}},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("case results = %d, want 3", len(results))
	}
	for i, r := range results {
		if r.RepetitionIndex == nil || *r.RepetitionIndex != i {
			t.Errorf("result %d repetition index = %v, want %d", i, r.RepetitionIndex, i)
		}
	}
	raw, err := json.Marshal(results[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"repetitionIndex":0`) {
		t.Errorf("first case result JSON = %s, want repetitionIndex 0", raw)
	}
}

// A run whose inference failed still carries its index on its case result.
func TestEvaluateCarriesRepetitionIndexOnFailedRun(t *testing.T) {
	svc := okService(t, "case1")
	two := 2
	results, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{
			AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s-1",
			Status: service.InferenceStatusFailed, RepetitionIndex: &two,
		}},
		EvaluateConfig: service.EvaluateConfig{EvalMetrics: []models.EvalMetric{{MetricName: models.MetricToolTrajectoryAvgScore, Threshold: 1.0}}},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 || results[0].RepetitionIndex == nil || *results[0].RepetitionIndex != 2 {
		t.Errorf("results = %+v, want one failed result with repetition index 2", results)
	}
}

// A run whose case cannot be evaluated (here, missing from the set) is
// reported as failed with its index.
func TestEvaluateCarriesRepetitionIndexWhenCaseIsMissing(t *testing.T) {
	svc := okService(t, "case1")
	one := 1
	results, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{
			AppName: "app", EvalSetID: "set1", EvalCaseID: "gone", SessionID: "s-1",
			Status: service.InferenceStatusSuccess, Inferences: []models.Invocation{{}}, RepetitionIndex: &one,
		}},
		EvaluateConfig: service.EvaluateConfig{EvalMetrics: []models.EvalMetric{{MetricName: models.MetricToolTrajectoryAvgScore, Threshold: 1.0}}},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusFailed || results[0].RepetitionIndex == nil || *results[0].RepetitionIndex != 1 {
		t.Errorf("results = %+v, want one FAILED result with repetition index 1", results)
	}
}
