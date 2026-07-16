// Package service orchestrates eval inference and metric scoring.
//
// [LocalEvalService] is the primary entry point: it loads eval cases from
// [storage.EvalSetsManager], runs agent inference through [generator.Generator],
// scores results with [metrics.Registry], and optionally persists outcomes via
// [storage.EvalSetResultsManager]. When [LocalEvalService.Sessions] is set,
// evaluated case results include session metadata (id, app, user, state) in
// SessionDetails for parity with adk-python.
//
// Inference and evaluation run with configurable parallelism. Per-case failures
// do not abort sibling cases; operational errors (context cancellation, worker
// panic) are returned separately from per-case FAILED results. Vertex and
// LLM-judge metrics require injectable clients on the registry via
// [metrics.RegistryConfig].
package service
