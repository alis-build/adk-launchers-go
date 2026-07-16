package agenteval

import (
	"context"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
)

const defaultNumRuns = 2

// EvaluateEvalSet runs inference and evaluation numRuns times and aggregates mean metric scores per case.
func EvaluateEvalSet(
	ctx context.Context,
	svc *service.LocalEvalService,
	appName string,
	evalSet *models.EvalSet,
	cfg models.EvalConfig,
	numRuns int,
) ([]models.EvalCaseResult, error) {
	if svc == nil || evalSet == nil {
		return nil, fmt.Errorf("service and eval set are required")
	}
	if numRuns <= 0 {
		numRuns = defaultNumRuns
	}
	metricList, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	var inferences []service.InferenceResult
	for range numRuns {
		batch, err := svc.PerformInference(ctx, service.InferenceRequest{
			AppName:   appName,
			EvalSetID: evalSet.EvalSetID,
		})
		if err != nil {
			return nil, err
		}
		inferences = append(inferences, batch...)
	}

	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inferences,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: metricList,
			Parallelism: 4,
		},
	})
	if err != nil {
		return nil, err
	}
	return aggregateByMean(evalSet.EvalSetID, results), nil
}

// aggregateByMean groups repeated run results by eval case and averages metric scores.
func aggregateByMean(evalSetID string, results []models.EvalCaseResult) []models.EvalCaseResult {
	byCase := make(map[string][]models.EvalCaseResult)
	for _, r := range results {
		byCase[r.EvalID] = append(byCase[r.EvalID], r)
	}
	out := make([]models.EvalCaseResult, 0, len(byCase))
	for evalID, cases := range byCase {
		agg := models.EvalCaseResult{
			EvalSetID:                evalSetID,
			EvalID:                   evalID,
			OverallEvalMetricResults: meanMetricResults(cases),
		}
		agg.FinalEvalStatus = finalStatus(agg.OverallEvalMetricResults)
		out = append(out, agg)
	}
	return out
}

// meanMetricResults averages overall metric scores across multiple runs of one case.
func meanMetricResults(cases []models.EvalCaseResult) []models.EvalMetricResult {
	type acc struct {
		total  float64
		count  int
		metric models.EvalMetricResult
	}
	byName := make(map[string]*acc)
	for _, c := range cases {
		for _, m := range c.OverallEvalMetricResults {
			a := byName[m.MetricName]
			if a == nil {
				a = &acc{metric: m}
				byName[m.MetricName] = a
			}
			if m.Score != nil {
				a.total += *m.Score
				a.count++
			}
		}
	}
	out := make([]models.EvalMetricResult, 0, len(byName))
	for _, a := range byName {
		m := a.metric
		if a.count > 0 {
			mean := a.total / float64(a.count)
			m.Score = &mean
			m.EvalStatus = statusForMean(mean, m.Threshold)
		}
		out = append(out, m)
	}
	return out
}

// statusForMean maps a mean score to Passed or Failed using the metric threshold.
func statusForMean(score, threshold float64) models.EvalStatus {
	if score >= threshold {
		return models.EvalStatusPassed
	}
	return models.EvalStatusFailed
}

// finalStatus derives case status from aggregated metric results.
func finalStatus(metrics []models.EvalMetricResult) models.EvalStatus {
	status := models.EvalStatusNotEvaluated
	for _, m := range metrics {
		switch m.EvalStatus {
		case models.EvalStatusPassed:
			status = models.EvalStatusPassed
		case models.EvalStatusFailed:
			return models.EvalStatusFailed
		}
	}
	return status
}
