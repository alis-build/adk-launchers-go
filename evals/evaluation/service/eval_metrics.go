package service

import (
	"errors"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// PrepareEvalMetrics returns the metrics a run scores: requested, then each
// default efficiency metric (see [models.AppendDefaultEfficiencyMetrics])
// that reg provides, so a registry without them adds nothing that would
// score NOT_EVALUATED and leave every run undecided. A requested efficiency
// metric reg cannot build, such as one given a threshold, is an error before
// anything runs, as adk-python refuses it. A nil reg adds nothing.
func PrepareEvalMetrics(reg *metrics.Registry, requested []models.EvalMetric) ([]models.EvalMetric, error) {
	if reg == nil {
		return requested, nil
	}
	for _, m := range requested {
		if !models.IsDefaultEfficiencyMetric(m.MetricName) {
			continue
		}
		var notFound *metrics.ErrMetricNotFound
		if _, err := reg.GetEvaluator(m); err != nil && !errors.As(err, &notFound) {
			return nil, fmt.Errorf("metric %q: %w", m.MetricName, err)
		}
	}
	all := models.AppendDefaultEfficiencyMetrics(requested)
	out := all[:len(requested):len(requested)]
	for _, m := range all[len(requested):] {
		if _, err := reg.GetEvaluator(m); err == nil {
			out = append(out, m)
		}
	}
	return out, nil
}
