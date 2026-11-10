package agenteval

import (
	"fmt"
	"slices"

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

// CrossRunResults judges one case's runs against each criterion, as
// adk-python's _judge_cross_run_criteria does. A criterion without a
// threshold only reports (INFORMATIONAL); one whose value cannot be computed
// is NOT_EVALUATED; otherwise it PASSED when the value reaches the threshold.
// Each result notes how many runs passed.
func CrossRunResults(runs []models.EvalCaseResult, criteria []models.CrossRunCriterion) []models.CrossRunResult {
	if len(criteria) == 0 {
		return nil
	}
	statuses := make([]models.EvalStatus, len(runs))
	crashedRuns := 0
	for i, run := range runs {
		statuses[i] = RunStatus(run)
		if crashed(run) {
			crashedRuns++
		}
	}
	out := make([]models.CrossRunResult, 0, len(criteria))
	for _, c := range criteria {
		var value *float64
		switch c.Method {
		case models.CrossRunPassRate:
			value = PassRate(statuses)
		case models.CrossRunPassAtK:
			value, _ = PassAtK(statuses, *c.K)
		case models.CrossRunPassHatK:
			value, _ = PassHatK(statuses, *c.K)
		}
		res := models.CrossRunResult{
			Name:      c.DisplayName(),
			Value:     value,
			Threshold: c.Threshold,
			Note:      passCountNote(statuses, value, crashedRuns),
		}
		switch {
		case c.Threshold == nil:
			res.EvalStatus = models.EvalStatusInformational
		case value == nil:
			res.EvalStatus = models.EvalStatusNotEvaluated
		case *value >= *c.Threshold:
			res.EvalStatus = models.EvalStatusPassed
		default:
			res.EvalStatus = models.EvalStatusFailed
		}
		out = append(out, res)
	}
	return out
}

// passCountNote says how many runs passed behind a cross-run value, as
// adk-python's _pass_count_note: "2 of 3 runs passed", with ", 1 not
// evaluated" and ", 1 crashed" when non-zero. Without a value it says how
// many runs passed or failed instead, since there were too few to give one.
func passCountNote(statuses []models.EvalStatus, value *float64, crashedRuns int) string {
	passed := countOf(statuses, models.EvalStatusPassed)
	decided := passed + countOf(statuses, models.EvalStatusFailed)
	crashedClause := ""
	if crashedRuns > 0 {
		crashedClause = fmt.Sprintf(", %d crashed", crashedRuns)
	}
	if value == nil {
		return fmt.Sprintf("%d of %d runs passed or failed", decided, len(statuses)) + crashedClause
	}
	note := fmt.Sprintf("%d of %d runs passed", passed, len(statuses))
	if notEvaluated := len(statuses) - decided - crashedRuns; notEvaluated > 0 {
		note += fmt.Sprintf(", %d not evaluated", notEvaluated)
	}
	return note + crashedClause
}

// countOf counts the statuses equal to s.
func countOf(statuses []models.EvalStatus, s models.EvalStatus) int {
	n := 0
	for _, st := range statuses {
		if st == s {
			n++
		}
	}
	return n
}

// checkKAgainstRuns rejects a pass@k or pass^k criterion that looks at more
// runs than each case gets, before any inference, as adk-python's
// check_pass_k_criteria_against_num_runs does. It also rejects one with no
// k, which a config built in Go rather than decoded can carry.
func checkKAgainstRuns(criteria []models.CrossRunCriterion, runs int) error {
	for i, c := range criteria {
		if !slices.Contains([]string{models.CrossRunPassAtK, models.CrossRunPassHatK}, c.Method) {
			continue
		}
		if c.K == nil {
			return fmt.Errorf("multi_run_config.cross_run_criteria[%d]: %q needs k, the number of runs it looks at together", i, c.Method)
		}
		if *c.K > runs {
			return fmt.Errorf("multi_run_config.cross_run_criteria[%d]: k=%d is more than the %d run(s) of each eval case. Lower k, or raise num_runs.", i, *c.K, runs)
		}
	}
	return nil
}
