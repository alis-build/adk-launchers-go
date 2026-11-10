package agenteval

import (
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// RunStatus returns whether one run of a case passed, failed or was
// undecided, as adk-python's get_run_status does. A crashed run (FAILED with
// no metric results at all) is undecided and left out of every count. A
// failed run is FAILED. A run with any metric not evaluated is undecided,
// even when the others passed, since the missing verdict could have been a
// fail. A passed run is PASSED, and any other, such as one scored only by
// informational metrics, is undecided.
func RunStatus(run models.EvalCaseResult) models.EvalStatus {
	if crashed(run) {
		return models.EvalStatusNotEvaluated
	}
	if run.FinalEvalStatus == models.EvalStatusFailed {
		return models.EvalStatusFailed
	}
	for _, m := range run.OverallEvalMetricResults {
		if m.EvalStatus == models.EvalStatusNotEvaluated {
			return models.EvalStatusNotEvaluated
		}
	}
	if run.FinalEvalStatus == models.EvalStatusPassed {
		return models.EvalStatusPassed
	}
	return models.EvalStatusNotEvaluated
}

// crashed reports whether run failed without producing any metric result,
// which is how a run whose inference failed is recorded.
func crashed(run models.EvalCaseResult) bool {
	if run.FinalEvalStatus != models.EvalStatusFailed || len(run.OverallEvalMetricResults) > 0 {
		return false
	}
	for _, inv := range run.EvalMetricResultPerInvocation {
		if len(inv.EvalMetricResults) > 0 {
			return false
		}
	}
	return true
}

// countDecided returns how many statuses are decided (PASSED or FAILED) and
// how many of those passed.
func countDecided(statuses []models.EvalStatus) (decided, passed int) {
	for _, s := range statuses {
		switch s {
		case models.EvalStatusPassed:
			decided++
			passed++
		case models.EvalStatusFailed:
			decided++
		}
	}
	return decided, passed
}

// PassRate is the fraction of decided runs that passed; nil when none was
// decided.
func PassRate(statuses []models.EvalStatus) *float64 {
	decided, passed := countDecided(statuses)
	if decided == 0 {
		return nil
	}
	v := float64(passed) / float64(decided)
	return &v
}

// PassAtK is the chance that at least one of k decided runs passes, the
// unbiased estimator of Chen et al. (2021): 1 - C(n-c, k)/C(n, k) over n
// decided runs of which c passed. It is nil when fewer than k runs were
// decided, and an error when k < 1.
func PassAtK(statuses []models.EvalStatus, k int) (*float64, error) {
	if k < 1 {
		return nil, fmt.Errorf("k must be at least 1, got %d", k)
	}
	decided, passed := countDecided(statuses)
	if decided < k {
		return nil, nil
	}
	v := 1 - choose(decided-passed, k)/choose(decided, k)
	return &v, nil
}

// PassHatK is the chance that all of k decided runs pass, the unbiased
// estimator of Yao et al. (2024, tau-bench): C(c, k)/C(n, k). It is nil when
// fewer than k runs were decided, and an error when k < 1.
func PassHatK(statuses []models.EvalStatus, k int) (*float64, error) {
	if k < 1 {
		return nil, fmt.Errorf("k must be at least 1, got %d", k)
	}
	decided, passed := countDecided(statuses)
	if decided < k {
		return nil, nil
	}
	v := choose(passed, k) / choose(decided, k)
	return &v, nil
}

// choose is the binomial coefficient C(a, k) as a float64, 0 when a < k,
// computed multiplicatively so it does not overflow for realistic run counts.
func choose(a, k int) float64 {
	if k < 0 || a < k {
		return 0
	}
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(a-k+i) / float64(i)
	}
	return r
}
