// Package metrics implements eval metric evaluators and the metric registry.
//
// [DefaultRegistry] registers the standard adk-python metrics (trajectory match,
// response match, ROUGE, Vertex evaluators, LLM-judge rubrics, and related
// scores). Call [Registry.GetRegisteredMetrics] or the evals HTTP
// /metrics-info endpoint for metadata. Override or extend metrics with
// [Registry.RegisterEvaluator].
//
// Vertex and LLM-judge evaluators require injectable clients via
// [Registry.SetConfig] and [RegistryConfig].
package metrics
