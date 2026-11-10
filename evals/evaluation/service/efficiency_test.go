package service_test

import (
	"context"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
	"google.golang.org/genai"
)

// scoreOne evaluates one successful inference for case1 with metrics.
func scoreOne(t *testing.T, svc *service.LocalEvalService, invs []models.Invocation, metricNames ...string) models.EvalCaseResult {
	t.Helper()
	var ms []models.EvalMetric
	for _, n := range metricNames {
		m := models.EvalMetric{MetricName: n}
		if n == models.MetricToolTrajectoryAvgScore {
			m.Threshold = 1.0
		}
		ms = append(ms, m)
	}
	results, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s-1", Status: service.InferenceStatusSuccess, Inferences: invs}},
		EvaluateConfig:   service.EvaluateConfig{EvalMetrics: ms},
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("Evaluate = %+v, %v", results, err)
	}
	return results[0]
}

func metricNamed(t *testing.T, ms []models.EvalMetricResult, name string) models.EvalMetricResult {
	t.Helper()
	for _, m := range ms {
		if m.MetricName == name {
			return m
		}
	}
	t.Fatalf("no %s in %+v", name, ms)
	return models.EvalMetricResult{}
}

func modelTurn(total, model, tool float64, prompt, candidates int32) models.Invocation {
	return models.Invocation{
		UserContent:   genai.NewContentFromText("Hello", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		IntermediateData: models.InvocationEventsField(models.InvocationEvents{InvocationEvents: []models.InvocationEvent{
			{Author: "app", ModelVersion: "gemini-test", UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: prompt, CandidatesTokenCount: candidates}},
		}}),
		Duration: &total, ModelDuration: &model, ToolDuration: &tool,
	}
}

func TestEvaluateKeepsCaseStatusWithInformationalMetric(t *testing.T) {
	res := scoreOne(t, okService(t, "case1"), []models.Invocation{modelTurn(1, 0.5, 0, 10, 5)}, models.MetricToolTrajectoryAvgScore, models.MetricToolCallCountV1)
	if res.FinalEvalStatus != models.EvalStatusPassed {
		t.Errorf("case status = %d, want PASSED", res.FinalEvalStatus)
	}
	if m := metricNamed(t, res.OverallEvalMetricResults, models.MetricToolCallCountV1); m.EvalStatus != models.EvalStatusInformational {
		t.Errorf("tool_call_count_v1 status = %d, want 4", m.EvalStatus)
	}
}

func TestEvaluateReportsTokenUsageDetails(t *testing.T) {
	inv := modelTurn(1, 0.5, 0, 300, 50)
	res := scoreOne(t, okService(t, "case1"), []models.Invocation{inv}, models.MetricTokenUsageV1)
	m := metricNamed(t, res.OverallEvalMetricResults, models.MetricTokenUsageV1)
	if m.Details == nil || m.Details.TokenUsageDetails == nil || m.Details.TokenUsageDetails.TotalTokens == nil || *m.Details.TokenUsageDetails.TotalTokens != 350 {
		t.Fatalf("overall details = %+v, want total 350", m.Details)
	}
	per := metricNamed(t, res.EvalMetricResultPerInvocation[0].EvalMetricResults, models.MetricTokenUsageV1)
	if per.Details == nil || per.Details.TokenUsageDetails == nil || *per.Details.TokenUsageDetails.TotalTokens != 350 {
		t.Errorf("per-invocation details = %+v, want total 350", per.Details)
	}
}

func TestEvaluateReportsDurationDetails(t *testing.T) {
	res := scoreOne(t, okService(t, "case1"), []models.Invocation{modelTurn(10, 6, 3, 1, 1)}, models.MetricInvocationDurationV1)
	m := metricNamed(t, res.OverallEvalMetricResults, models.MetricInvocationDurationV1)
	if m.Details == nil || m.Details.DurationDetails == nil || m.Details.DurationDetails.FrameworkSeconds == nil || *m.Details.DurationDetails.FrameworkSeconds != 1 {
		t.Fatalf("overall details = %+v, want framework 1", m.Details)
	}
	per := metricNamed(t, res.EvalMetricResultPerInvocation[0].EvalMetricResults, models.MetricInvocationDurationV1)
	if per.Details == nil || per.Details.DurationDetails == nil || *per.Details.DurationDetails.FrameworkSeconds != 1 {
		t.Errorf("per-invocation details = %+v, want framework 1", per.Details)
	}
}

// shortInformational reports one per-invocation result whatever it is given.
type shortInformational struct{}

func (shortInformational) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (metrics.EvaluationResult, error) {
	one := 1.0
	return metrics.EvaluationResult{OverallScore: &one, OverallEvalStatus: models.EvalStatusInformational,
		PerInvocationResults: []metrics.PerInvocationResult{{ActualInvocation: actual[0], Score: &one, EvalStatus: models.EvalStatusInformational}}}, nil
}

func TestEvaluateKeepsNullEntriesForMisalignedInformationalMetric(t *testing.T) {
	svc := okService(t, "case2turns")
	svc.Registry = metrics.NewDefaultRegistry()
	svc.Registry.RegisterEvaluator(models.MetricInfo{MetricName: "short_info"}, func(models.EvalMetric, *metrics.RegistryConfig) (metrics.Evaluator, error) {
		return shortInformational{}, nil
	})
	if err := svc.Sets.AddEvalCase("app", "set1", models.EvalCase{EvalID: "case1", Conversation: []models.Invocation{
		{UserContent: genai.NewContentFromText("a", genai.RoleUser)}, {UserContent: genai.NewContentFromText("b", genai.RoleUser)},
	}}); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}

	res := scoreOne(t, svc, []models.Invocation{modelTurn(1, 0, 0, 1, 1), modelTurn(1, 0, 0, 1, 1)}, "short_info")

	if len(res.EvalMetricResultPerInvocation) != 2 {
		t.Fatalf("per-invocation = %d, want 2", len(res.EvalMetricResultPerInvocation))
	}
	for i, inv := range res.EvalMetricResultPerInvocation {
		m := metricNamed(t, inv.EvalMetricResults, "short_info")
		// adk-python's placeholder PerInvocationResult defaults to NOT_EVALUATED.
		if m.Score != nil || m.EvalStatus != models.EvalStatusNotEvaluated {
			t.Errorf("invocation %d entry = %+v, want nil score not evaluated", i, m)
		}
	}
}
