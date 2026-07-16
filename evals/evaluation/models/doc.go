// Package models defines evaluation data types aligned with adk-python
// google.adk.evaluation (EvalSet, EvalCase, Invocation, EvalConfig, results,
// rubrics, and metric constants). JSON field names match the DevServer eval
// API and .evalset.json on-disk format.
//
// # Rubric and judge surface
//
//   - [Rubric] / [RubricContent] / [RubricScore] describe rubric-based
//     evaluations. RubricScore carries the optional Rationale text returned
//     by adk-python judges (nil when the judge omitted it).
//   - [RubricsBasedCriterion] holds the caller-supplied rubric list plus
//     judge model options; the criterion may be shared across invocations,
//     with per-invocation rubrics unioned in at evaluation time via
//     Invocation.Rubrics.
//   - [LlmAsAJudgeCriterion] configures generic LLM-judge metrics
//     (final_response_match_v2, hallucinations_v1).
//   - [LlmBackedUserSimulatorCriterion] configures
//     per_turn_user_simulator_quality_v1. StopSignal overrides the default
//     [DefaultUserSimulatorStopSignal] `</finished>` sentinel used to end
//     LLM-driven user simulations.
//
// # AppDetails
//
// [AppDetails] and [AgentDetails] capture per-invocation agent instructions
// and tool declarations. Evaluators use these when rendering judge prompts
// (see the metrics package) so the judge sees the same developer
// instructions and tool set the agent saw. Invocation.AppDetails is populated
// by the generator package and tolerates legacy raw-JSON payloads via a
// lenient [AppDetails.UnmarshalJSON].
//
// # Conversation scenarios
//
// [ConversationScenario] drives LLM-backed user simulation. When a
// [UserPersona] is attached, the evaluator switches to the persona-aware
// prompt template and folds each [PersonaBehavior]'s ViolationRubrics into
// the criteria block sent to the judge. Legacy raw-JSON personas are
// tolerated by [ConversationScenario.UnmarshalJSON] (unknown shapes leave
// UserPersona nil so persisted eval sets keep loading).
package models
