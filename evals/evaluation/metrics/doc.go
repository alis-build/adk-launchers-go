// Package metrics implements eval metric evaluators and the metric registry.
//
// [DefaultRegistry] registers the standard adk-python metrics (trajectory
// match, response match, ROUGE, Vertex evaluators, LLM-judge rubrics, and
// related scores). Call [Registry.GetRegisteredMetrics] or the evals HTTP
// /metrics-info endpoint for metadata. Override or extend metrics with
// [Registry.RegisterEvaluator].
//
// Vertex and LLM-judge evaluators require injectable clients via
// [Registry.SetConfig] and [RegistryConfig].
//
// # Efficiency metrics
//
// Four informational metrics report what a run cost without passing or
// failing it (status [models.EvalStatusInformational]); a threshold on them is
// rejected:
//
//   - tool_call_count_v1 — tool calls per invocation.
//   - inference_call_count_v1 — model calls per invocation.
//   - token_usage_v1 — prompt, completion and total tokens, broken down by
//     token type in [models.TokenUsageDetails].
//   - invocation_duration_v1 — seconds per invocation, split into model and
//     tool time in [models.DurationDetails]. Live inference records no
//     durations, so for live runs this metric reports no value.
//
// The evals launcher adds all four to every run.
//
// # Tool trajectory matching
//
// tool_trajectory_avg_score compares tool calls by name and arguments, with
// the criterion's matchType (EXACT, IN_ORDER or ANY_ORDER; match_type is
// accepted too). With ignoreArgs (or ignore_args) set, only tool names are
// compared, under any match type.
//
// # Rubric-based LLM-judge metrics
//
// Three metrics score a conversation against a caller-supplied
// [models.RubricsBasedCriterion]:
//
//   - rubric_based_final_response_quality_v1 — requires golden expected data;
//     judges the actual final response against the reference.
//   - rubric_based_tool_use_quality_v1 — reference-free; judges the actual
//     tool trajectory.
//   - rubric_based_multi_turn_trajectory_quality_v1 — dedicated evaluator
//     that judges the entire dialogue in a single LLM call and marks the
//     first N-1 turns [models.EvalStatusNotEvaluated] (see
//     multiTurnRubricEvaluator).
//
// The judge is prompted with the adk-python prompt templates (see
// rubric_prompts.go) which render the rubric list, tool declarations, and
// developer instructions from [models.AppDetails]. The judge response is
// parsed in plain text by parseRubricVerdicts using the `Property:` /
// `Rationale:` / `Verdict:` block format from adk-python's
// DefaultAutoRaterResponseParser. Verdicts must match `yes`/`no` exactly
// after wrapping-punctuation strip; anything else scores nil.
//
// When [models.JudgeModelOptions.NumSamples] > 1 the rubric evaluator draws
// multiple samples per invocation and majority-vote-aggregates by rubric ID
// (see majorityVoteAggregate); ties break negative to match Python.
//
// # Per-turn user simulator quality metric
//
// per_turn_user_simulator_quality_v1 uses a dedicated
// perTurnSimulatorEvaluator (not the shared llmJudgeEvaluator) because it
// runs a multi-phase flow taken from adk-python
// PerTurnUserSimulatorQualityV1:
//
//  1. Turn 1: deterministic starting-prompt equality check — no LLM call.
//  2. Turns 2..N: LLM judge (persona-free or persona template) sampled
//     NumSamples times, majority-vote-aggregated per turn.
//  3. Synthetic stop-signal turn: same LLM path with a proxy invocation
//     whose user content is the configured stop signal
//     ([models.DefaultUserSimulatorStopSignal] `</finished>` by default,
//     overridable via [models.LlmBackedUserSimulatorCriterion.StopSignal]).
//  4. If the stop-signal turn fails, the last real turn's Score and
//     EvalStatus are overwritten (the invocation itself is preserved so the
//     `results[i].ActualInvocation == actual[i]` invariant holds).
//  5. Overall = num_valid / num_evaluated across the collected results.
//
// The turns and the stop-signal turn are judged concurrently (see Judge
// clients and safety); results keep turn order, and the first failed judge
// call cancels the others and fails the metric, as in adk-python.
//
// # Judge clients and safety
//
// Metrics that call an LLM judge return [models.EvalStatusNotEvaluated] when
// [RegistryConfig.JudgeClient] is nil, matching the adk-python fallback.
//
// Scoring one case, a metric makes at most
// [models.JudgeModelOptions.ParallelismLimit] judge calls at once, across its
// invocations and samples: one at a time when unset. Cases are scored
// concurrently too (the run's parallelism, 4 by default), so a run can make up
// to parallelism × ParallelismLimit calls at once, and the
// [RegistryConfig.JudgeClient] must be safe for concurrent use. A limit below 1 is rejected when the evaluator is
// built. For the per-invocation judge metrics a failed judge call leaves only
// its own invocation NOT_EVALUATED (logged), not the whole metric.
package metrics
