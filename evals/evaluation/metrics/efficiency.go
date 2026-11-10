package metrics

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// The efficiency metrics port adk-python's _efficiency_evaluators.py. Each is
// informational: it reports a value per invocation and their mean overall,
// and never passes or fails a case. Expected invocations and scenarios are
// not used.

// rejectThreshold refuses a threshold or criterion on an efficiency metric,
// as adk-python does: a threshold would have no effect today, and refusing
// it keeps thresholds out of configs, so making these metrics gate later
// cannot silently start failing evals. A configured threshold is a non-zero
// Threshold, since Go's float64 cannot tell an absent one from zero, or any
// criterion.
func rejectThreshold(m models.EvalMetric) error {
	if m.Threshold == 0 && !m.Criterion.IsSet() {
		return nil
	}
	return fmt.Errorf("`%s` does not support a threshold: it reports a value and never passes or fails. Remove it from `criteria` entirely; it is reported automatically on every eval.", m.MetricName)
}

// efficiencyFunc computes one invocation's value; nil means not applicable.
type efficiencyFunc func(models.Invocation) *float64

func (f efficiencyFunc) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (EvaluationResult, error) {
	return evaluateEfficiency(actual, f), nil
}

// evaluateEfficiency reports compute's value for each invocation and their
// mean overall (adk-python's _evaluate_efficiency_metric). The overall is an
// average, not a sum, so it compares across cases with different turn
// counts; nil values are left out, and with none the overall is nil.
func evaluateEfficiency(actual []models.Invocation, compute efficiencyFunc) EvaluationResult {
	if len(actual) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusInformational}
	}
	res := EvaluationResult{OverallEvalStatus: models.EvalStatusInformational}
	var sum float64
	var n int
	for _, inv := range actual {
		v := compute(inv)
		res.PerInvocationResults = append(res.PerInvocationResults, PerInvocationResult{
			ActualInvocation: inv, Score: v, EvalStatus: models.EvalStatusInformational,
		})
		if v != nil {
			sum += *v
			n++
		}
	}
	if n > 0 {
		mean := sum / float64(n)
		res.OverallScore = &mean
	}
	return res
}

// toolCallCount is tool_call_count_v1's value: the invocation's tool calls.
func toolCallCount(inv models.Invocation) *float64 {
	calls, err := models.GetAllToolCalls(inv.IntermediateData)
	if err != nil {
		return nil
	}
	v := float64(len(calls))
	return &v
}

// inferenceCallCount is inference_call_count_v1's value: the invocation's
// model calls, counted as events that carry usage or a model version. It is
// nil unless the invocation recorded its events.
func inferenceCallCount(inv models.Invocation) *float64 {
	events, ok := inv.IntermediateData.AsInvocationEvents()
	if !ok || events == nil {
		return nil
	}
	var n float64
	for _, e := range events.InvocationEvents {
		if e.UsageMetadata != nil || e.ModelVersion != "" {
			n++
		}
	}
	return &n
}

// tokenUsageEvaluator is token_usage_v1: the tokens of all model calls in an
// invocation, with every token type reported alongside the total.
type tokenUsageEvaluator struct{}

func (tokenUsageEvaluator) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (EvaluationResult, error) {
	res := EvaluationResult{OverallEvalStatus: models.EvalStatusInformational}
	if len(actual) == 0 {
		return res, nil
	}
	all := make([]models.TokenUsageDetails, 0, len(actual))
	for _, inv := range actual {
		d := invocationTokenUsage(inv)
		all = append(all, d)
		res.PerInvocationResults = append(res.PerInvocationResults, PerInvocationResult{
			ActualInvocation: inv, Score: d.TotalTokens, EvalStatus: models.EvalStatusInformational, TokenUsageDetails: &d,
		})
	}
	overall := averageTokenUsage(all)
	res.OverallScore = overall.TotalTokens
	res.OverallTokenUsageDetails = &overall
	return res, nil
}

// invocationTokenUsage sums every reported count across the invocation's
// model calls. A count no call reported stays nil, distinct from zero; the
// total is input plus output, set when either was reported, and the
// backend's own total is never read.
func invocationTokenUsage(inv models.Invocation) models.TokenUsageDetails {
	var d models.TokenUsageDetails
	events, ok := inv.IntermediateData.AsInvocationEvents()
	if !ok || events == nil {
		return d
	}
	for _, e := range events.InvocationEvents {
		if e.UsageMetadata != nil {
			addCall(&d, e.UsageMetadata)
		}
	}
	if d.InputTokens != nil || d.OutputTokens != nil {
		total := deref(d.InputTokens) + deref(d.OutputTokens)
		d.TotalTokens = &total
	}
	return d
}

// addCall folds one model call's usage into d, with adk-python telemetry's
// meanings: input is prompt plus tool use, output is candidates plus
// thoughts. genai counts are int32, so a zero count reads as not reported.
func addCall(d *models.TokenUsageDetails, u *genai.GenerateContentResponseUsageMetadata) {
	prompt, toolUse := u.PromptTokenCount, u.ToolUsePromptTokenCount
	candidates, thoughts := u.CandidatesTokenCount, u.ThoughtsTokenCount
	if prompt != 0 || toolUse != 0 {
		d.InputTokens = add(d.InputTokens, prompt+toolUse)
	}
	if candidates != 0 || thoughts != 0 {
		d.OutputTokens = add(d.OutputTokens, candidates+thoughts)
	}
	d.PromptTokens = addReported(d.PromptTokens, prompt)
	d.CachedTokens = addReported(d.CachedTokens, u.CachedContentTokenCount)
	d.ToolUseTokens = addReported(d.ToolUseTokens, toolUse)
	d.CandidatesTokens = addReported(d.CandidatesTokens, candidates)
	d.ReasoningTokens = addReported(d.ReasoningTokens, thoughts)
}

// add adds n to a running total that may not have started yet.
func add(total *float64, n int32) *float64 {
	v := deref(total) + float64(n)
	return &v
}

// addReported adds n to total when n was reported (non-zero).
func addReported(total *float64, n int32) *float64 {
	if n == 0 {
		return total
	}
	return add(total, n)
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// averageTokenUsage averages each count over the invocations whose total was
// reported. Within those, an unreported count counts as zero (a backend
// omits a count a call did not spend), so the denominator is shared and the
// average total stays input plus output; a count none of them reported stays
// nil.
func averageTokenUsage(all []models.TokenUsageDetails) models.TokenUsageDetails {
	var reported []models.TokenUsageDetails
	for _, d := range all {
		if d.TotalTokens != nil {
			reported = append(reported, d)
		}
	}
	avg := func(get func(models.TokenUsageDetails) *float64) *float64 {
		var sum float64
		present := false
		for _, d := range reported {
			if v := get(d); v != nil {
				sum += *v
				present = true
			}
		}
		if !present {
			return nil
		}
		v := sum / float64(len(reported))
		return &v
	}
	return models.TokenUsageDetails{
		TotalTokens:      avg(func(d models.TokenUsageDetails) *float64 { return d.TotalTokens }),
		InputTokens:      avg(func(d models.TokenUsageDetails) *float64 { return d.InputTokens }),
		PromptTokens:     avg(func(d models.TokenUsageDetails) *float64 { return d.PromptTokens }),
		CachedTokens:     avg(func(d models.TokenUsageDetails) *float64 { return d.CachedTokens }),
		ToolUseTokens:    avg(func(d models.TokenUsageDetails) *float64 { return d.ToolUseTokens }),
		OutputTokens:     avg(func(d models.TokenUsageDetails) *float64 { return d.OutputTokens }),
		CandidatesTokens: avg(func(d models.TokenUsageDetails) *float64 { return d.CandidatesTokens }),
		ReasoningTokens:  avg(func(d models.TokenUsageDetails) *float64 { return d.ReasoningTokens }),
	}
}

// durationEvaluator is invocation_duration_v1: each invocation's measured
// seconds, split into model, tool and framework time.
type durationEvaluator struct{}

func (durationEvaluator) EvaluateInvocations(_ context.Context, actual, _ []models.Invocation, _ *models.ConversationScenario) (EvaluationResult, error) {
	res := EvaluationResult{OverallEvalStatus: models.EvalStatusInformational}
	if len(actual) == 0 {
		return res, nil
	}
	all := make([]models.DurationDetails, 0, len(actual))
	for _, inv := range actual {
		d := invocationDuration(inv)
		all = append(all, d)
		res.PerInvocationResults = append(res.PerInvocationResults, PerInvocationResult{
			ActualInvocation: inv, Score: d.TotalSeconds, EvalStatus: models.EvalStatusInformational, DurationDetails: &d,
		})
	}
	overall := averageDuration(all)
	res.OverallScore = overall.TotalSeconds
	res.OverallDurationDetails = &overall
	return res, nil
}

// invocationDuration is one invocation's measured time. An untimed turn
// reports nothing, not zero. Framework time is what is left once model and
// tool time are out, never negative, and nil when either part was not
// measured, so a turn timed before the split is not read as pure overhead.
func invocationDuration(inv models.Invocation) models.DurationDetails {
	if inv.Duration == nil {
		return models.DurationDetails{}
	}
	d := models.DurationDetails{TotalSeconds: inv.Duration, ModelSeconds: inv.ModelDuration, ToolSeconds: inv.ToolDuration}
	if inv.ModelDuration != nil && inv.ToolDuration != nil {
		f := round3(math.Max(*inv.Duration-*inv.ModelDuration-*inv.ToolDuration, 0))
		d.FrameworkSeconds = &f
	}
	return d
}

// averageDuration averages each part over the invocations that measured it,
// rounded to milliseconds.
func averageDuration(all []models.DurationDetails) models.DurationDetails {
	avg := func(get func(models.DurationDetails) *float64) *float64 {
		var sum float64
		var n int
		for _, d := range all {
			if v := get(d); v != nil {
				sum += *v
				n++
			}
		}
		if n == 0 {
			return nil
		}
		v := round3(sum / float64(n))
		return &v
	}
	return models.DurationDetails{
		TotalSeconds:     avg(func(d models.DurationDetails) *float64 { return d.TotalSeconds }),
		ModelSeconds:     avg(func(d models.DurationDetails) *float64 { return d.ModelSeconds }),
		ToolSeconds:      avg(func(d models.DurationDetails) *float64 { return d.ToolSeconds }),
		FrameworkSeconds: avg(func(d models.DurationDetails) *float64 { return d.FrameworkSeconds }),
	}
}

// round3 rounds to 3 decimal places as adk-python's round(x, 3) does: by the
// exact stored value, half to even. Scaling by 1000 first would create
// halves the value does not hold (10.1235 averaged from 10.123 and 10.124
// rounds down in Python, up with math.Round(x*1000)/1000).
func round3(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// efficiencyFactory builds an efficiency evaluator after refusing any
// threshold on the metric.
func efficiencyFactory(ev Evaluator) func(models.EvalMetric, *RegistryConfig) (Evaluator, error) {
	return func(m models.EvalMetric, _ *RegistryConfig) (Evaluator, error) {
		if err := rejectThreshold(m); err != nil {
			return nil, err
		}
		return ev, nil
	}
}
