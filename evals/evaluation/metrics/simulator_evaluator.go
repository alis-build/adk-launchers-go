package metrics

import (
	"context"
	"fmt"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// perTurnSimulatorEvaluator is the dedicated evaluator for
// per_turn_user_simulator_quality_v1. It ports adk-python
// PerTurnUserSimulatorQualityV1's full flow:
//
//  1. Turn 1: deterministic starting-prompt equality check (no LLM call).
//  2. Turns 2..N: LLM judge, sample NumSamples times, majority-vote aggregate.
//  3. Synthetic stop-signal turn: same LLM path with a proxy invocation whose
//     user content is the configured stop signal.
//  4. If the stop-signal turn fails, overwrite the last real turn's result
//     with the failing outcome (Python's overwrite semantics).
//  5. Overall = num_valid / num_evaluated where num_valid sums the scores of
//     turns whose EvalStatus is PASSED (Python's _aggregate_conversation_results).
type perTurnSimulatorEvaluator struct {
	metric     models.EvalMetric
	client     JudgeClient
	stopSignal string
	opts       models.JudgeModelOptions
}

// newPerTurnSimulatorEvaluator constructs the per-turn simulator evaluator.
func newPerTurnSimulatorEvaluator(metric models.EvalMetric, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	stop := models.DefaultUserSimulatorStopSignal
	if c, ok := metric.Criterion.AsUserSimulator(); ok && strings.TrimSpace(c.StopSignal) != "" {
		stop = c.StopSignal
	}
	return &perTurnSimulatorEvaluator{
		metric:     metric,
		client:     client,
		stopSignal: stop,
		opts:       judgeOptions(metric),
	}
}

// EvaluateInvocations runs the full Python flow.
func (e *perTurnSimulatorEvaluator) EvaluateInvocations(
	ctx context.Context,
	actual []models.Invocation,
	_ []models.Invocation,
	scenario *models.ConversationScenario,
) (EvaluationResult, error) {
	if e.client == nil {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}
	if scenario == nil {
		return EvaluationResult{}, fmt.Errorf("conversation_scenario is required by %s", e.metric.MetricName)
	}
	if len(actual) == 0 {
		return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
	}

	results := make([]PerInvocationResult, 0, len(actual)+1)
	results = append(results, e.evaluateFirstTurn(actual[0], scenario))

	for i := 1; i < len(actual); i++ {
		r, err := e.evaluateIntermediateTurn(ctx, actual[i], actual[:i], scenario)
		if err != nil {
			return EvaluationResult{}, err
		}
		results = append(results, r)
	}

	// Synthetic stop-signal turn (Python _evaluate_stop_signal_turn).
	stop := e.stopSignalInvocation()
	stopResult, err := e.evaluateIntermediateTurn(ctx, stop, actual, scenario)
	if err != nil {
		return EvaluationResult{}, err
	}
	if stopResult.EvalStatus == models.EvalStatusFailed && len(results) > 0 {
		// Copy only Score/EvalStatus from the stop-signal outcome so the last
		// real turn's ActualInvocation/ExpectedInvocation stay intact (Python
		// overwrites the whole entry but the proxy invocation would violate
		// the results[i].ActualInvocation == actual[i] invariant on our side).
		last := &results[len(results)-1]
		last.Score = stopResult.Score
		last.EvalStatus = stopResult.EvalStatus
	}

	return e.aggregateConversationResults(results), nil
}

// evaluateFirstTurn implements Python's _evaluate_first_turn.
func (e *perTurnSimulatorEvaluator) evaluateFirstTurn(
	inv models.Invocation,
	scenario *models.ConversationScenario,
) PerInvocationResult {
	if inv.UserContent == nil {
		return PerInvocationResult{ActualInvocation: inv, EvalStatus: models.EvalStatusNotEvaluated}
	}
	got := strings.TrimSpace(textFromContent(inv.UserContent))
	want := strings.TrimSpace(scenario.StartingPrompt)
	var s float64
	if got == want {
		s = 1.0
	}
	return PerInvocationResult{
		ActualInvocation: inv,
		Score:            &s,
		EvalStatus:       statusForScore(s, e.metric.Threshold),
	}
}

// evaluateIntermediateTurn implements Python's _evaluate_intermediate_turn:
// build a prompt, sample NumSamples times, majority-vote aggregate.
func (e *perTurnSimulatorEvaluator) evaluateIntermediateTurn(
	ctx context.Context,
	inv models.Invocation,
	history []models.Invocation,
	scenario *models.ConversationScenario,
) (PerInvocationResult, error) {
	prompt := formatPerTurnSimulatorPrompt(
		textFromContent(inv.UserContent),
		history,
		scenario,
		e.stopSignal,
	)
	n := e.opts.NumSamples
	if n <= 0 {
		n = 1
	}
	samples := make([]PerInvocationResult, 0, n)
	for range n {
		resp, err := e.client.GenerateJudgeResponse(ctx, prompt, e.opts)
		if err != nil {
			return PerInvocationResult{}, err
		}
		score, perr := parseIsValidJSON(resp)
		var s *float64
		st := models.EvalStatusNotEvaluated
		if perr == nil {
			v := score
			s = &v
			st = statusForScore(score, e.metric.Threshold)
		}
		samples = append(samples, PerInvocationResult{
			ActualInvocation: inv,
			Score:            s,
			EvalStatus:       st,
		})
	}
	if len(samples) == 0 {
		return PerInvocationResult{
			ActualInvocation: inv,
			EvalStatus:       models.EvalStatusNotEvaluated,
		}, nil
	}
	if len(samples) == 1 {
		return samples[0], nil
	}
	return simulatorSampleAggregate(samples), nil
}

// stopSignalInvocation returns the proxy invocation Python uses for the
// stop-signal turn (invocation_id "stop_signal_proxy_invocation").
func (e *perTurnSimulatorEvaluator) stopSignalInvocation() models.Invocation {
	return models.Invocation{
		InvocationID: "stop_signal_proxy_invocation",
		UserContent: &genai.Content{
			Parts: []*genai.Part{{Text: e.stopSignal}},
		},
	}
}

// aggregateConversationResults implements Python's
// _aggregate_conversation_results: overall = num_valid / num_evaluated where
// num_valid sums the scores of turns whose EvalStatus is PASSED.
func (e *perTurnSimulatorEvaluator) aggregateConversationResults(results []PerInvocationResult) EvaluationResult {
	var valid float64
	evaluated := 0
	for _, r := range results {
		if r.EvalStatus == models.EvalStatusPassed && r.Score != nil {
			valid += *r.Score
		}
		evaluated++
	}
	if evaluated == 0 {
		return EvaluationResult{PerInvocationResults: results}
	}
	overall := valid / float64(evaluated)
	return EvaluationResult{
		OverallScore:         &overall,
		OverallEvalStatus:    statusForScore(overall, e.metric.Threshold),
		PerInvocationResults: results,
	}
}
