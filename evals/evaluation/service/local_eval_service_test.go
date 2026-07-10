package service_test

import (
	"context"
	"iter"
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

// stubRubricJudge returns a fixed judge response for every prompt.
type stubRubricJudge struct{ response string }

func (s stubRubricJudge) GenerateJudgeResponse(context.Context, string, models.JudgeModelOptions) (string, error) {
	return s.response, nil
}

func TestLocalEvalServiceEvaluate(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	call := &genai.FunctionCall{Name: "lookup", Args: map[string]any{}}
	expected := models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse:    genai.NewContentFromText("ok", genai.RoleModel),
			IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{call}}),
		}},
	}
	_ = sets.AddEvalCase("app", "set1", expected)

	a, _ := agent.New(agent.Config{
		Name: "agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "agent"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	rt, _ := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "agent")

	svc := &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	inf, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName: "app", EvalSetID: "set1",
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].Status != service.InferenceStatusSuccess {
		t.Fatalf("inf = %+v", inf)
	}

	actualCall := &genai.FunctionCall{Name: "lookup", Args: map[string]any{}}
	inf[0].Inferences[0].IntermediateData = models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{actualCall}})

	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricToolTrajectoryAvgScore,
				Threshold:  0.5,
				Criterion: models.CriterionField(models.ToolTrajectoryCriterion{
					BaseCriterion: models.BaseCriterion{Threshold: 0.5},
				}),
			}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("results = %+v", results)
	}
}

// TestEvaluatePartialFailureNoZeroResults ensures that when an inference
// references a missing eval case, Evaluate does not emit zero-valued
// EvalCaseResult entries (which would bucket under EvalSetID=="" and cause
// SaveEvalSetResult to fail silently).
func TestEvaluatePartialFailureNoZeroResults(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})

	// Two inferences: one references the existing case, the other references
	// a missing case so evaluateSingle returns an error.
	inf := []service.InferenceResult{
		{AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1", Status: service.InferenceStatusSuccess, Inferences: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}}},
		{AppName: "app", EvalSetID: "set1", EvalCaseID: "missing", SessionID: "s2", Status: service.InferenceStatusSuccess, Inferences: []models.Invocation{}},
	}

	svc := &service.LocalEvalService{
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	results, _ := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricResponseMatchScore,
				Threshold:  0.5,
			}},
		},
	})
	// Missing eval case yields a FAILED result; persistence/save errors may
	// also populate err without dropping the successful case.
	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per inference), got %d: %+v", len(results), results)
	}
	if results[0].EvalID != "case1" || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("case1 result = %+v", results[0])
	}
	if results[1].EvalID != "missing" || results[1].FinalEvalStatus != models.EvalStatusFailed {
		t.Fatalf("missing case result = %+v", results[1])
	}
}

// TestEvaluateSingleTurnRubricPopulatesOverallDetails is the wire-side
// regression guard for the "rubric: []" bug: computeMetricResults populates
// EvalMetricResult.Details.RubricScores only when the evaluator surfaces
// OverallRubricScores. Before the aggregatePerInvocationRubrics fix,
// single-turn rubric evaluators (final_response, tool_use) left it nil.
func TestEvaluateSingleTurnRubricPopulatesOverallDetails(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		}},
	})

	textProp := "be helpful"
	rubric := models.Rubric{
		RubricID:      "r1",
		RubricContent: models.RubricContent{TextProperty: &textProp},
	}
	reg := metrics.NewDefaultRegistry()
	reg.SetConfig(metrics.RegistryConfig{
		JudgeClient: stubRubricJudge{response: "Property: be helpful\nRationale: ok\nVerdict: yes\n"},
	})

	svc := &service.LocalEvalService{
		Sets:        sets,
		Registry:    reg,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	inf := []service.InferenceResult{{
		AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1",
		Status: service.InferenceStatusSuccess,
		Inferences: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		}},
	}}
	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricRubricBasedFinalResponseQualityV1,
				Threshold:  0.5,
				Criterion: models.CriterionField(models.RubricsBasedCriterion{
					BaseCriterion: models.BaseCriterion{Threshold: 0.5},
					Rubrics:       []models.Rubric{rubric},
				}),
			}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	overall := results[0].OverallEvalMetricResults
	if len(overall) != 1 {
		t.Fatalf("overall = %+v", overall)
	}
	if overall[0].Details == nil {
		t.Fatal("overall.Details is nil; wire renders rubric: [] (regression)")
	}
	if len(overall[0].Details.RubricScores) != 1 {
		t.Fatalf("overall.Details.RubricScores = %+v", overall[0].Details.RubricScores)
	}
	got := overall[0].Details.RubricScores[0]
	if got.RubricID != "r1" || got.Score == nil || *got.Score != 1.0 {
		t.Fatalf("rubric score = %+v", got)
	}
}
