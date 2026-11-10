package agenteval

import (
	"context"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
)

const defaultNumRuns = 2

// EvaluateEvalSet runs each case several times, evaluates every run, and
// aggregates per case: mean metric scores, and the verdict of each
// cross-run criterion in cfg.MultiRunConfig. The number of runs is numRuns
// when positive, else cfg.MultiRunConfig.NumRuns, else 2, as adk eval's
// --num_runs overrides the config. A pass@k or pass^k criterion looking at
// more runs than that is rejected before any inference.
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
	var criteria []models.CrossRunCriterion
	if cfg.MultiRunConfig != nil {
		criteria = cfg.MultiRunConfig.CrossRunCriteria
	}
	switch {
	case numRuns > 0:
	case cfg.MultiRunConfig != nil && cfg.MultiRunConfig.NumRuns != nil:
		numRuns = *cfg.MultiRunConfig.NumRuns
	default:
		numRuns = defaultNumRuns
	}
	if err := checkKAgainstRuns(criteria, numRuns); err != nil {
		return nil, err
	}
	metricList, err := models.GetEvalMetricsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	metricList = models.AppendDefaultEfficiencyMetrics(metricList)

	inferences, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName:         appName,
		EvalSetID:       evalSet.EvalSetID,
		InferenceConfig: service.InferenceConfig{NumRuns: numRuns},
	})
	if err != nil {
		return nil, err
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
	aggregated := aggregateByMean(evalSet.EvalSetID, results)
	if len(criteria) > 0 {
		runsByCase := make(map[string][]models.EvalCaseResult)
		for _, r := range results {
			runsByCase[r.EvalID] = append(runsByCase[r.EvalID], r)
		}
		for i := range aggregated {
			agg := &aggregated[i]
			agg.CrossRunResults = CrossRunResults(runsByCase[agg.EvalID], criteria)
			// A failed criterion fails the case; otherwise the status stays
			// as the mean left it, so a crash's FAILED is kept.
			for _, r := range agg.CrossRunResults {
				if r.EvalStatus == models.EvalStatusFailed {
					agg.FinalEvalStatus = models.EvalStatusFailed
					break
				}
			}
		}
	}
	return aggregated, nil
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
		// A run that failed without producing any metric result (its
		// inference crashed) fails the case, as in adk-python; otherwise a
		// crash would read as merely not evaluated.
		for _, c := range cases {
			if c.FinalEvalStatus == models.EvalStatusFailed && len(c.OverallEvalMetricResults) == 0 {
				agg.FinalEvalStatus = models.EvalStatusFailed
				break
			}
		}
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
		// informational: some run reported the metric as informational;
		// verdict: some run passed or failed it. The mean is graded only when
		// a run gave a verdict.
		informational bool
		verdict       bool
	}
	byName := make(map[string]*acc)
	for _, c := range cases {
		for _, m := range c.OverallEvalMetricResults {
			a := byName[m.MetricName]
			if a == nil {
				a = &acc{metric: m}
				byName[m.MetricName] = a
			}
			switch m.EvalStatus {
			case models.EvalStatusInformational:
				a.informational = true
			case models.EvalStatusPassed, models.EvalStatusFailed:
				a.verdict = true
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
		// Details (rubric scores, token and duration breakdowns) describe one
		// run, so an average over several drops them, as adk-python's
		// aggregator does, rather than pair run 1's with everyone's mean.
		if len(cases) > 1 {
			m.Details = &models.EvalMetricResultDetails{}
		}
		if a.count > 0 {
			mean := a.total / float64(a.count)
			m.Score = &mean
			if a.informational && !a.verdict {
				m.EvalStatus = models.EvalStatusInformational
			} else {
				m.EvalStatus = statusForMean(mean, m.Threshold)
			}
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
// Informational metrics never pass or fail a case.
func finalStatus(metrics []models.EvalMetricResult) models.EvalStatus {
	status := models.EvalStatusNotEvaluated
	for _, m := range metrics {
		switch m.EvalStatus {
		case models.EvalStatusInformational:
			continue
		case models.EvalStatusPassed:
			status = models.EvalStatusPassed
		case models.EvalStatusFailed:
			return models.EvalStatusFailed
		}
	}
	return status
}
