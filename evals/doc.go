// Package evals provides a dev-only evaluation HTTP sublauncher with parity to
// adk-python DevServer eval endpoints (eval sets, run eval, metrics-info).
//
// # Role in the ADK web launcher
//
// The ADK web launcher composes one or more sublaunchers, each activated by a CLI
// keyword. This package registers the keyword "evals" and mounts dev eval routes on
// go.alis.build/mux via [go.alis.build/adk/launchers/web.HostRouteSetup]. Routes
// are dev-only; production deployments typically omit this sublauncher.
//
// # Authentication
//
// Eval routes do not enforce caller identity. Access control is delegated to a
// trusted upstream (gateway or BFF); do not expose this sublauncher directly on
// the public internet without that boundary.
//
// # Usage
//
// Register with the web launcher and enable the keyword at runtime:
//
//	web.NewLauncher(evals.NewLauncher(evals.WithAgentsDir("./agents")))
//
//	adk web --port 8080 evals -agents_dir=./agents
//
// Routes live under {pathPrefix}/dev/apps/{app_name}/... (default pathPrefix
// /api, matching adk-web -api_server_address=/api). Override with [WithPathPrefix]
// or --path_prefix.
//
// # Routes
//
// Canonical paths use hyphens; legacy underscore paths remain for adk-web.
//
//	POST   /api/dev/apps/{app}/eval-sets
//	GET    /api/dev/apps/{app}/eval-sets
//	GET    /api/dev/apps/{app}/eval-sets/{id}
//	DELETE /api/dev/apps/{app}/eval-sets/{id}
//	POST   /api/dev/apps/{app}/eval-sets/{id}/add-session
//	POST   /api/dev/apps/{app}/eval-sets/{id}/run
//	GET    /api/dev/apps/{app}/eval-sets/{id}/eval-cases
//	GET    /api/dev/apps/{app}/eval-sets/{id}/eval-cases/{caseId}
//	PUT    /api/dev/apps/{app}/eval-sets/{id}/eval-cases/{caseId}
//	DELETE /api/dev/apps/{app}/eval-sets/{id}/eval-cases/{caseId}
//	GET    /api/dev/apps/{app}/eval-results
//	GET    /api/dev/apps/{app}/eval-results/{resultId}
//	GET    /api/dev/apps/{app}/metrics-info
//
// Legacy equivalents: eval_sets, add_session, run_eval, evals (case list/CRUD),
// eval_results.
//
// # Storage
//
// Local mode (default): eval sets at {agentsDir}/{app}/{id}.evalset.json and
// results at {agentsDir}/{app}/.adk/eval_history/. Set via [WithAgentsDir] or
// --agents_dir.
//
// GCS mode: pass [WithEvalStorageURI] or --eval_storage_uri=gs://bucket.
//
// Override backends with [WithEvalSetsManager] and [WithEvalSetResultsManager]
// (for example in-memory managers in tests).
//
// # Configuration
//
// Options apply when calling [NewLauncher]:
//
//   - [WithAgentsDir] — local eval set and result storage root.
//   - [WithEvalStorageURI] — GCS bucket for sets and results.
//   - [WithEvalSetsManager] / [WithEvalSetResultsManager] — custom storage backends.
//   - [WithMetricRegistry] — metric evaluators (default [metrics.DefaultRegistry]).
//   - [WithUserSimulatorProvider] — LLM-backed or static user simulators during inference.
//   - [WithPathPrefix] — URL prefix before /dev/apps/... (default "/api").
//
// CLI flags (after the "evals" keyword on the web command line):
//
//   - -path_prefix — same as [WithPathPrefix].
//   - -agents_dir — same as [WithAgentsDir]; required for local storage unless GCS is set.
//   - -eval_storage_uri — same as [WithEvalStorageURI].
//
// # Evaluation engine
//
// Run eval loads the eval set, runs [service.LocalEvalService] inference and
// scoring via [go.alis.build/adk/launchers/internal/adkrun.Runtime], and persists
// results when a results manager is configured. Inference uses
// [generator.Generator] and [simulation.UserSimulator] implementations; scoring
// uses [metrics.Registry]. See the evaluation/ subpackages for programmatic use
// without the HTTP launcher.
package evals
