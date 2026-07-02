package parity_test

import (
	"context"
	"iter"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

// Python reference: tests/unittests/evaluation/test_response_evaluator.py
// TestResponseEvaluator.test_evaluate_invocations_rouge_metric
func TestResponseMatchScoreMatchesPythonRouge(t *testing.T) {
	reg := metrics.DefaultRegistry
	ev, err := reg.GetEvaluator(models.EvalMetric{
		MetricName: models.MetricResponseMatchScore,
		Threshold:  0.8,
	})
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}

	actual := models.Invocation{
		UserContent: genai.NewContentFromText("This is a test query.", genai.RoleUser),
		FinalResponse: genai.NewContentFromText(
			"This is a test candidate response.",
			genai.RoleModel,
		),
	}
	expected := models.Invocation{
		UserContent: genai.NewContentFromText("This is a test query.", genai.RoleUser),
		FinalResponse: genai.NewContentFromText(
			"This is a test reference.",
			genai.RoleModel,
		),
	}

	result, err := ev.EvaluateInvocations(
		context.Background(),
		[]models.Invocation{actual},
		[]models.Invocation{expected},
		nil,
	)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	wantScore := 8.0 / 11.0
	if result.OverallScore == nil || math.Abs(*result.OverallScore-wantScore) > 1e-9 {
		t.Fatalf("overall score = %v, want %v", result.OverallScore, wantScore)
	}
	if result.OverallEvalStatus != models.EvalStatusFailed {
		t.Fatalf("status = %v, want FAILED (score < 0.8 threshold)", result.OverallEvalStatus)
	}
}

func TestGoldenEvalSetEndToEnd(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	set, err := loadGoldenEvalSet(t)
	if err != nil {
		t.Fatalf("loadGoldenEvalSet: %v", err)
	}
	if _, err := sets.CreateEvalSet("golden_app", set.EvalSetID); err != nil {
		t.Fatalf("CreateEvalSet: %v", err)
	}
	for _, c := range set.EvalCases {
		if err := sets.AddEvalCase("golden_app", set.EvalSetID, c); err != nil {
			t.Fatalf("AddEvalCase: %v", err)
		}
	}

	const agentResponse = "This is a test candidate response."
	a, err := agent.New(agent.Config{
		Name: "golden_app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "agent"
				ev.Content = genai.NewContentFromText(agentResponse, genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "golden_app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	svc := &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
	}

	inferences, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName:   "golden_app",
		EvalSetID: set.EvalSetID,
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inferences) != 1 {
		t.Fatalf("inferences = %+v", inferences)
	}

	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inferences,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricResponseMatchScore,
				Threshold:  0.8,
			}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].FinalEvalStatus != models.EvalStatusFailed {
		t.Fatalf("final status = %v, want FAILED", results[0].FinalEvalStatus)
	}
	if len(results[0].OverallEvalMetricResults) == 0 {
		t.Fatal("expected metric results")
	}
	got := results[0].OverallEvalMetricResults[0].Score
	if got == nil || math.Abs(*got-8.0/11.0) > 1e-9 {
		t.Fatalf("score = %v, want %v", got, 8.0/11.0)
	}
}

func loadGoldenEvalSet(t *testing.T) (models.EvalSet, error) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "testdata", "golden_response_match.evalset.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return models.EvalSet{}, err
	}
	return models.ParseEvalSetFile("golden_response_match", data)
}
