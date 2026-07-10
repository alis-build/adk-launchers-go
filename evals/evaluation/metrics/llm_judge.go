package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// JudgeClient runs LLM-as-judge prompts for eval metrics.
type JudgeClient interface {
	GenerateJudgeResponse(ctx context.Context, prompt string, opts models.JudgeModelOptions) (string, error)
}

// llmJudgeEvaluator scores invocations by prompting an LLM judge. Behavior is
// customized per metric via formatPrompt, parseScore, and optional
// aggregatePerInvocation hooks wired by the new*Evaluator factory functions.
//
// parseScore receives the actual invocation so rubric evaluators can thread
// per-invocation rubrics (unioned with the criterion set) through to the
// parser; simpler metrics ignore the argument.
type llmJudgeEvaluator struct {
	metric                 models.EvalMetric
	requireExpected        bool
	client                 JudgeClient
	formatPrompt           func(actual models.Invocation, expected *models.Invocation) string
	parseScore             func(response string, actual models.Invocation) (float64, []models.RubricScore, error)
	aggregatePerInvocation func([]PerInvocationResult) PerInvocationResult
}

// EvaluateInvocations runs the LLM judge for each actual invocation and
// returns per-invocation scores plus a mean overall score.
func (e *llmJudgeEvaluator) EvaluateInvocations(
	ctx context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	_ *models.ConversationScenario,
) (EvaluationResult, error) {
	// Without an injected client the metric cannot run; callers treat this as
	// NotEvaluated rather than a hard error (ADK Python parity).
	if e.client == nil {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	// Metrics that compare against golden data must receive expected invocations.
	if e.requireExpected && len(expected) == 0 {
		return EvaluationResult{}, errExpectedRequired(e.metric.MetricName)
	}

	opts := judgeOptions(e.metric)
	var per []PerInvocationResult

	// Score each actual invocation independently; expected is paired by index.
	for i := range actual {
		var exp *models.Invocation
		if i < len(expected) {
			exp = &expected[i]
		} else if e.requireExpected {
			// Golden conversation ended before actual; skip unmatched turns.
			continue
		}

		// NumSamples controls judge variance; default to a single sample.
		// Samples run sequentially to respect LLM rate limits and keep eval cost predictable.
		samples := opts.NumSamples
		if samples <= 0 {
			samples = 1
		}

		var sampleResults []PerInvocationResult
		for range samples {
			resp, err := e.client.GenerateJudgeResponse(ctx, e.formatPrompt(actual[i], exp), opts)
			if err != nil {
				return EvaluationResult{}, err
			}
			score, rubrics, err := e.parseScore(resp, actual[i])
			if err != nil {
				return EvaluationResult{}, err
			}
			s := score
			sampleResults = append(sampleResults, PerInvocationResult{
				ActualInvocation:   actual[i],
				ExpectedInvocation: exp,
				Score:              &s,
				EvalStatus:         statusForScore(score, e.metric.Threshold),
				RubricScores:       rubrics,
			})
		}

		// When NumSamples > 1, aggregatePerInvocation (when set) collapses
		// multiple judge runs into one per-invocation result.
		result := sampleResults[0]
		if e.aggregatePerInvocation != nil && len(sampleResults) > 1 {
			result = e.aggregatePerInvocation(sampleResults)
		}
		per = append(per, result)
	}

	if len(per) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}

	// Overall score is the arithmetic mean of per-invocation scores.
	overall := avgScore(per)
	status := models.EvalStatusNotEvaluated
	if overall != nil {
		status = statusForScore(*overall, e.metric.Threshold)
	}
	return EvaluationResult{
		OverallScore:         overall,
		OverallEvalStatus:    status,
		PerInvocationResults: per,
		OverallRubricScores:  aggregatePerInvocationRubrics(per),
	}, nil
}

// judgeOptions extracts JudgeModelOptions from the metric criterion payload.
func judgeOptions(metric models.EvalMetric) models.JudgeModelOptions {
	if c, ok := metric.Criterion.AsLlmJudge(); ok {
		return c.JudgeModelOptions
	}
	if c, ok := metric.Criterion.AsRubrics(); ok {
		return c.JudgeModelOptions
	}
	if c, ok := metric.Criterion.AsHallucinations(); ok {
		return c.JudgeModelOptions
	}
	return models.JudgeModelOptions{}
}

// parseValidInvalidJSON maps judge JSON {"is_the_agent_response_valid":"valid"|"invalid"}
// to scores 1.0 and 0.0 respectively.
func parseValidInvalidJSON(response string) (float64, error) {
	var payload struct {
		IsValid string `json:"is_the_agent_response_valid"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(response)), &payload); err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(payload.IsValid)) {
	case "valid":
		return 1, nil
	case "invalid":
		return 0, nil
	default:
		return 0, fmt.Errorf("unexpected judge label %q", payload.IsValid)
	}
}

var jsonObjectRe = regexp.MustCompile(`\{[\s\S]*\}`)

// extractJSONObject pulls the first JSON object from free-form judge output so
// parseValidInvalidJSON can tolerate surrounding prose.
func extractJSONObject(text string) string {
	match := jsonObjectRe.FindString(text)
	if match == "" {
		return text
	}
	return match
}

// rubricKind selects which rubric-based LLM-judge evaluator variant to build.
type rubricKind int

const (
	rubricKindFinalResponse rubricKind = iota
	rubricKindToolUse
	rubricKindMultiTurnTrajectory
)

// newFinalResponseMatchV2Evaluator builds an LLM judge that compares final
// responses against golden text (final_response_match_v2).
func newFinalResponseMatchV2Evaluator(metric models.EvalMetric, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	return &llmJudgeEvaluator{
		metric:          metric,
		requireExpected: true,
		client:          client,
		formatPrompt:    formatFinalResponseMatchV2Prompt,
		parseScore: func(response string, _ models.Invocation) (float64, []models.RubricScore, error) {
			score, err := parseValidInvalidJSON(response)
			return score, nil, err
		},
	}
}

// formatFinalResponseMatchV2Prompt builds the judge prompt for final-response
// comparison metrics.
func formatFinalResponseMatchV2Prompt(actual models.Invocation, expected *models.Invocation) string {
	golden := ""
	if expected != nil {
		golden = textFromContent(expected.FinalResponse)
	}
	return fmt.Sprintf(`Rate whether the agent response matches the reference.
User prompt: %q
Agent response: %q
Reference response: %q
Respond with JSON: {"reasoning":"...", "is_the_agent_response_valid":"valid" or "invalid"}`,
		textFromContent(actual.UserContent),
		textFromContent(actual.FinalResponse),
		golden,
	)
}

// newHallucinationsEvaluator builds a reference-free LLM judge for
// hallucinations_v1 (no golden expected invocation required).
func newHallucinationsEvaluator(metric models.EvalMetric, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	return &llmJudgeEvaluator{
		metric:          metric,
		requireExpected: false,
		client:          client,
		formatPrompt: func(actual models.Invocation, _ *models.Invocation) string {
			return fmt.Sprintf(`Detect hallucinations in the agent response for user query %q. Response: %q. Reply JSON {"is_the_agent_response_valid":"valid" or "invalid"}`,
				textFromContent(actual.UserContent),
				textFromContent(actual.FinalResponse),
			)
		},
		parseScore: func(response string, _ models.Invocation) (float64, []models.RubricScore, error) {
			score, err := parseValidInvalidJSON(response)
			return score, nil, err
		},
	}
}

// rubricsFromCriterion pulls the rubric list off the metric criterion, returning
// nil when the criterion isn't a RubricsBasedCriterion.
func rubricsFromCriterion(m models.EvalMetric) []models.Rubric {
	if c, ok := m.Criterion.AsRubrics(); ok {
		return c.Rubrics
	}
	return nil
}

// newRubricBasedEvaluator builds rubric-based LLM-judge evaluators.
//
// Final-response rubrics require golden data (Python's evaluator requires
// expected_invocation for the response comparison). Tool-use rubrics are
// reference-free — Python's tool-use evaluator only inspects the actual
// invocation. Multi-turn trajectory rubrics use a dedicated evaluator that
// judges the full dialogue in a single LLM call.
func newRubricBasedEvaluator(metric models.EvalMetric, kind rubricKind, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	critRubrics := rubricsFromCriterion(metric)

	if kind == rubricKindMultiTurnTrajectory {
		return &multiTurnRubricEvaluator{
			metric:           metric,
			client:           client,
			criterionRubrics: critRubrics,
		}
	}

	return &llmJudgeEvaluator{
		metric:          metric,
		requireExpected: kind == rubricKindFinalResponse,
		client:          client,
		formatPrompt: func(actual models.Invocation, expected *models.Invocation) string {
			eff, err := effectiveRubrics(critRubrics, actual.Rubrics)
			if err != nil {
				// Duplicate rubric IDs across criterion + invocation scope: fall
				// back to criterion-only rubrics so we can still produce a prompt.
				eff = critRubrics
			}
			switch kind {
			case rubricKindFinalResponse:
				return formatRubricFinalResponsePrompt(actual, expected, eff)
			case rubricKindToolUse:
				return formatRubricToolUsePrompt(actual, eff)
			}
			return ""
		},
		parseScore: func(resp string, actual models.Invocation) (float64, []models.RubricScore, error) {
			eff, err := effectiveRubrics(critRubrics, actual.Rubrics)
			if err != nil {
				eff = critRubrics
			}
			return parseRubricVerdicts(resp, eff)
		},
		aggregatePerInvocation: func(s []PerInvocationResult) PerInvocationResult {
			return majorityVoteAggregate(s, metric.Threshold)
		},
	}
}

// multiTurnRubricEvaluator is the dedicated evaluator for
// rubric_based_multi_turn_trajectory_quality_v1. It marks the first N-1 turns
// as NOT_EVALUATED and issues a single judge call over the full dialogue on the
// last turn, matching adk-python
// RubricBasedMultiTurnTrajectoryEvaluator.evaluate_invocations.
type multiTurnRubricEvaluator struct {
	metric           models.EvalMetric
	client           JudgeClient
	criterionRubrics []models.Rubric
}

// EvaluateInvocations runs one judge call across the full conversation.
func (e *multiTurnRubricEvaluator) EvaluateInvocations(
	ctx context.Context,
	actual []models.Invocation,
	expected []models.Invocation,
	_ *models.ConversationScenario,
) (EvaluationResult, error) {
	if e.client == nil {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	if len(actual) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}

	// Pre-populate first N-1 turns as NOT_EVALUATED.
	per := make([]PerInvocationResult, 0, len(actual))
	for i := 0; i < len(actual)-1; i++ {
		var exp *models.Invocation
		if i < len(expected) {
			exp = &expected[i]
		}
		per = append(per, PerInvocationResult{
			ActualInvocation:   actual[i],
			ExpectedInvocation: exp,
			EvalStatus:         models.EvalStatusNotEvaluated,
		})
	}

	// Union criterion rubrics with rubrics attached to any invocation.
	invRubrics := make([]models.Rubric, 0)
	seen := make(map[string]struct{})
	for _, inv := range actual {
		for _, r := range inv.Rubrics {
			if _, ok := seen[r.RubricID]; ok {
				continue
			}
			seen[r.RubricID] = struct{}{}
			invRubrics = append(invRubrics, r)
		}
	}
	eff, err := effectiveRubrics(e.criterionRubrics, invRubrics)
	if err != nil {
		eff = e.criterionRubrics
	}

	opts := judgeOptions(e.metric)
	samples := opts.NumSamples
	if samples <= 0 {
		samples = 1
	}
	prompt := formatRubricMultiTurnPrompt(actual, eff)

	var sampleResults []PerInvocationResult
	last := actual[len(actual)-1]
	var lastExp *models.Invocation
	if len(actual)-1 < len(expected) {
		lastExp = &expected[len(actual)-1]
	}
	for range samples {
		resp, err := e.client.GenerateJudgeResponse(ctx, prompt, opts)
		if err != nil {
			return EvaluationResult{}, err
		}
		score, rubrics, err := parseRubricVerdicts(resp, eff)
		if err != nil {
			return EvaluationResult{}, err
		}
		s := score
		sampleResults = append(sampleResults, PerInvocationResult{
			ActualInvocation:   last,
			ExpectedInvocation: lastExp,
			Score:              &s,
			EvalStatus:         statusForScore(score, e.metric.Threshold),
			RubricScores:       rubrics,
		})
	}
	lastResult := sampleResults[0]
	if len(sampleResults) > 1 {
		lastResult = majorityVoteAggregate(sampleResults, e.metric.Threshold)
	}
	per = append(per, lastResult)

	// Aggregate overall rubric scores by ID across the sole scored turn.
	overallRubrics := aggregateOverallRubrics(lastResult.RubricScores)

	return EvaluationResult{
		OverallScore:         lastResult.Score,
		OverallEvalStatus:    lastResult.EvalStatus,
		PerInvocationResults: per,
		OverallRubricScores:  overallRubrics,
	}, nil
}

// aggregatePerInvocationRubrics collapses per-invocation RubricScores into an
// overall slot keyed by RubricID for the single-turn rubric evaluators
// (final_response and tool_use). Scores are the arithmetic mean across
// invocations where the rubric appears; a nil per-invocation score is
// skipped. Rubric IDs preserve first-seen ordering across invocations, then
// first-seen ordering within each invocation. Returns nil when no scored
// rubric appears in any invocation.
//
// This backs [EvaluationResult.OverallRubricScores], which downstream
// consumers (see local_eval_service.computeMetricResults) surface via
// EvalMetricResult.Details.RubricScores on the overall metric result.
// Without it, wire-side rubric arrays render empty even when the per-turn
// judge scored every rubric.
func aggregatePerInvocationRubrics(per []PerInvocationResult) []models.RubricScore {
	order := make([]string, 0)
	sums := make(map[string]float64)
	counts := make(map[string]int)
	for _, p := range per {
		for _, r := range p.RubricScores {
			if r.Score == nil {
				continue
			}
			if _, seen := sums[r.RubricID]; !seen {
				order = append(order, r.RubricID)
			}
			sums[r.RubricID] += *r.Score
			counts[r.RubricID]++
		}
	}
	if len(order) == 0 {
		return nil
	}
	agg := "This is an aggregated score derived from individual entries. Please refer to individual entries in each invocation for actual rationale from the model."
	out := make([]models.RubricScore, 0, len(order))
	for _, id := range order {
		mean := sums[id] / float64(counts[id])
		m := mean
		r := agg
		out = append(out, models.RubricScore{
			RubricID:  id,
			Score:     &m,
			Rationale: &r,
		})
	}
	return out
}

// aggregateOverallRubrics copies per-invocation rubric scores into the overall
// slot with an aggregated-score rationale, matching Python's
// MeanInvocationResultsSummarizer semantics for the single-turn evaluated case.
func aggregateOverallRubrics(rubrics []models.RubricScore) []models.RubricScore {
	if len(rubrics) == 0 {
		return nil
	}
	out := make([]models.RubricScore, 0, len(rubrics))
	agg := "This is an aggregated score derived from individual entries. Please refer to individual entries in each invocation for actual rationale from the model."
	for _, r := range rubrics {
		out = append(out, models.RubricScore{
			RubricID:  r.RubricID,
			Score:     r.Score,
			Rationale: &agg,
		})
	}
	return out
}
