// Package agenteval provides high-level helpers for running full eval sets.
//
// [EvaluateEvalSet] runs inference and metric evaluation multiple times,
// aggregates mean scores per eval case, and returns [models.EvalCaseResult]
// values suitable for persistence or reporting. It wraps [service.LocalEvalService]
// for callers that need batch evaluation outside the HTTP launcher.
package agenteval
