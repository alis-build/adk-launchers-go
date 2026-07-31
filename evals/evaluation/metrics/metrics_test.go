package metrics_test

import (
	"context"
	"math"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

func TestCalculateRouge1Scores(t *testing.T) {
	candidate := "This is a test candidate response."
	reference := "This is a test reference."
	score := metrics.CalculateRouge1Scores(candidate, reference)
	if math.Abs(score.Precision-2.0/3.0) > 1e-9 {
		t.Fatalf("precision = %v", score.Precision)
	}
	if math.Abs(score.Recall-4.0/5.0) > 1e-9 {
		t.Fatalf("recall = %v", score.Recall)
	}
	if math.Abs(score.FMeasure-8.0/11.0) > 1e-9 {
		t.Fatalf("fmeasure = %v", score.FMeasure)
	}
}

func TestTrajectoryEvaluatorExactMatch(t *testing.T) {
	reg := metrics.NewDefaultRegistry()
	ev, err := reg.GetEvaluator(models.EvalMetric{
		MetricName: models.MetricToolTrajectoryAvgScore,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.ToolTrajectoryCriterion{
			BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			MatchType:     models.ToolTrajectoryMatchExact,
		}),
	})
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	call := &genai.FunctionCall{Name: "test_func", Args: map[string]any{"arg1": "val1"}}
	makeInv := func() models.Invocation {
		return models.Invocation{
			UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
			IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{call}}),
		}
	}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{makeInv()}, []models.Invocation{makeInv()}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if result.OverallScore == nil || *result.OverallScore != 1 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}

func TestTrajectoryEvaluatorAnyOrder(t *testing.T) {
	reg := metrics.NewDefaultRegistry()
	ev, err := reg.GetEvaluator(models.EvalMetric{
		MetricName: models.MetricToolTrajectoryAvgScore,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.ToolTrajectoryCriterion{
			BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			MatchType:     models.ToolTrajectoryMatchAnyOrder,
		}),
	})
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	c1 := &genai.FunctionCall{Name: "test_func1", Args: map[string]any{}}
	c2 := &genai.FunctionCall{Name: "test_func2", Args: map[string]any{}}
	actual := models.Invocation{
		UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
		IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{c1, c2}}),
	}
	expected := models.Invocation{
		UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
		IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{c2, c1}}),
	}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if result.OverallScore == nil || *result.OverallScore != 1 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}

func TestResponseMatchEvaluator(t *testing.T) {
	reg := metrics.NewDefaultRegistry()
	ev, err := reg.GetEvaluator(models.EvalMetric{
		MetricName: models.MetricResponseMatchScore,
		Threshold:  0.8,
	})
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("q", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hello world", genai.RoleModel),
	}
	expected := models.Invocation{
		UserContent:   genai.NewContentFromText("q", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hello world", genai.RoleModel),
	}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if result.OverallScore == nil || *result.OverallScore != 1 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}

func TestDefaultRegistryHas13Metrics(t *testing.T) {
	infos := metrics.DefaultRegistry.GetRegisteredMetrics()
	if len(infos) != 13 {
		t.Fatalf("len = %d, want 13", len(infos))
	}
}

func TestRegistryMissingMetric(t *testing.T) {
	_, err := metrics.DefaultRegistry.GetEvaluator(models.EvalMetric{MetricName: "unknown_metric"})
	if err == nil {
		t.Fatal("expected error")
	}
}

type stubJudge struct {
	score float64
}

func (s stubJudge) GenerateJudgeResponse(context.Context, string, models.JudgeModelOptions) (string, error) {
	return `{"is_the_agent_response_valid":"valid"}`, nil
}

func TestLLMJudgeEvaluatorWithClient(t *testing.T) {
	reg := metrics.NewDefaultRegistry()
	reg.SetConfig(metrics.RegistryConfig{JudgeClient: stubJudge{}})
	ev, err := reg.GetEvaluator(models.EvalMetric{
		MetricName: models.MetricFinalResponseMatchV2,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.LlmAsAJudgeCriterion{
			BaseCriterion: models.BaseCriterion{Threshold: 0.5},
		}),
	})
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("q", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("a", genai.RoleModel),
	}
	expected := models.Invocation{
		FinalResponse: genai.NewContentFromText("a", genai.RoleModel),
	}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if result.OverallScore == nil || *result.OverallScore != 1 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}
