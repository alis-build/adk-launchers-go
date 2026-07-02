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
type llmJudgeEvaluator struct {
	metric                 models.EvalMetric
	requireExpected        bool
	client                 JudgeClient
	formatPrompt           func(actual models.Invocation, expected *models.Invocation) string
	parseScore             func(response string) (float64, []models.RubricScore, error)
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
			score, rubrics, err := e.parseScore(resp)
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
		parseScore: func(response string) (float64, []models.RubricScore, error) {
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
		parseScore: func(response string) (float64, []models.RubricScore, error) {
			score, err := parseValidInvalidJSON(response)
			return score, nil, err
		},
	}
}

// newRubricBasedEvaluator builds rubric-based LLM-judge evaluators. Multi-turn
// trajectory rubrics are reference-free; other kinds require expected invocations.
func newRubricBasedEvaluator(metric models.EvalMetric, kind rubricKind, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	return &llmJudgeEvaluator{
		metric:          metric,
		requireExpected: kind != rubricKindMultiTurnTrajectory,
		client:          client,
		formatPrompt: func(actual models.Invocation, expected *models.Invocation) string {
			return fmt.Sprintf("Evaluate rubric metric %q for response %q", metric.MetricName, textFromContent(actual.FinalResponse))
		},
		parseScore: func(response string) (float64, []models.RubricScore, error) {
			score, err := parseValidInvalidJSON(response)
			return score, nil, err
		},
	}
}

// newPerTurnSimulatorEvaluator builds the per-turn user simulator quality metric.
func newPerTurnSimulatorEvaluator(metric models.EvalMetric, cfg *RegistryConfig) Evaluator {
	var client JudgeClient
	if cfg != nil {
		client = cfg.JudgeClient
	}
	return &llmJudgeEvaluator{
		metric:          metric,
		requireExpected: false,
		client:          client,
		formatPrompt: func(actual models.Invocation, _ *models.Invocation) string {
			return fmt.Sprintf("Evaluate user simulator turn quality for %q", textFromContent(actual.UserContent))
		},
		parseScore: func(response string) (float64, []models.RubricScore, error) {
			score, err := parseValidInvalidJSON(response)
			return score, nil, err
		},
	}
}
