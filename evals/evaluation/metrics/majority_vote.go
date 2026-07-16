package metrics

import (
	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// majorityVoteAggregate collapses NumSamples per-invocation samples into one
// PerInvocationResult by taking the majority verdict for each rubric ID. Ports
// adk-python MajorityVotePerInvocationResultsAggregator at
// rubric_based_evaluator.py:112-200.
//
// EvalStatus is recomputed from the aggregated score against threshold, so a
// mixed [Failed, Passed, Passed] sample set reports the majority verdict
// consistently in both Score and EvalStatus.
//
// When samples carry no RubricScores (non-rubric evaluators like
// final_response_match_v2 and hallucinations_v1), the aggregator degrades to
// picking samples[0] so behavior is unchanged for those metrics.
func majorityVoteAggregate(samples []PerInvocationResult, threshold float64) PerInvocationResult {
	if len(samples) == 0 {
		return PerInvocationResult{}
	}
	// Non-rubric samples: preserve the first sample as-is.
	hasRubrics := false
	for _, s := range samples {
		if len(s.RubricScores) > 0 {
			hasRubrics = true
			break
		}
	}
	if !hasRubrics {
		return samples[0]
	}

	type buckets struct {
		none []models.RubricScore
		pos  []models.RubricScore
		neg  []models.RubricScore
	}
	byID := make(map[string]*buckets)
	order := make([]string, 0)
	for _, s := range samples {
		for _, rs := range s.RubricScores {
			b, ok := byID[rs.RubricID]
			if !ok {
				b = &buckets{}
				byID[rs.RubricID] = b
				order = append(order, rs.RubricID)
			}
			switch {
			case rs.Score == nil:
				b.none = append(b.none, rs)
			case *rs.Score == 1.0:
				b.pos = append(b.pos, rs)
			default:
				b.neg = append(b.neg, rs)
			}
		}
	}

	var out []models.RubricScore
	var total float64
	var counted int
	for _, id := range order {
		b := byID[id]
		var pick models.RubricScore
		switch {
		case len(b.pos) == 0 && len(b.neg) == 0:
			if len(b.none) > 0 {
				pick = b.none[0]
			} else {
				pick = models.RubricScore{RubricID: id}
			}
		case len(b.pos) > len(b.neg):
			pick = b.pos[0]
		default:
			// Ties break negative (Python does the same).
			pick = b.neg[0]
		}
		out = append(out, pick)
		if pick.Score != nil {
			total += *pick.Score
			counted++
		}
	}

	var mean *float64
	status := models.EvalStatusNotEvaluated
	if counted > 0 {
		v := total / float64(counted)
		mean = &v
		status = statusForScore(v, threshold)
	}

	return PerInvocationResult{
		ActualInvocation:   samples[0].ActualInvocation,
		ExpectedInvocation: samples[0].ExpectedInvocation,
		Score:              mean,
		RubricScores:       out,
		EvalStatus:         status,
	}
}

// simulatorSampleAggregate collapses NumSamples per-turn simulator samples
// using majority vote (positives > negatives → positive, ties break negative).
// Ports adk-python PerTurnUserSimulatorQualityV1._aggregate_samples at
// per_turn_user_simulator_quality_v1.py:249-265.
func simulatorSampleAggregate(samples []PerInvocationResult) PerInvocationResult {
	if len(samples) == 0 {
		return PerInvocationResult{}
	}
	var positives, negatives []PerInvocationResult
	for _, s := range samples {
		if s.Score == nil {
			continue
		}
		if *s.Score == 1.0 {
			positives = append(positives, s)
		} else if *s.Score == 0.0 {
			negatives = append(negatives, s)
		}
	}
	if len(positives) == 0 && len(negatives) == 0 {
		return samples[0]
	}
	if len(positives) > len(negatives) {
		return positives[0]
	}
	return negatives[0]
}
