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
// # Identity
//
// Every eval route requires a caller identity, forwarded by the trusted
// upstream as x-alis-identity, and answers 401 without one. Eval sets, cases
// and results are shared by every caller of the app; the identity decides
// whose sessions a route touches:
//
//   - add-session reads the session, and stores the new case, under the
//     caller's ID. A body userId naming anyone else answers 403.
//   - A run executes as the caller, on the agent of the route's app, whatever
//     user and app the stored case names. Its sessions are the caller's.
//   - Reading an eval set, an eval case or an eval result strips credential secrets
//     (OAuth client secrets, tokens, PKCE verifiers, API keys) from the
//     response, as adk-python's dev server does. The stored data keeps them.
//
// Because a run executes as the caller, an agent that reads user:-prefixed
// state or the memory service sees the caller's own data, so the same eval
// set can score differently for different people. Keep evaluated agents off
// user state and memory, seeding what they need in the case's session state,
// or compare results only within one caller's runs.
//
// For the same reason eval runs use no stored OAuth token by default: a tool
// configured in go.alis.build/adk/launchers/tooloauth fails with "<Provider>
// is not available in this run", even for a caller who has connected it, so
// an eval of a tool that writes never acts on their real account.
// [WithStoredOAuthTokens] turns that off; use it with a test account.
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
// A storage URI moves both elsewhere: gs://bucket for Cloud Storage, or
// file:///abs/path for a local directory (created when missing; the host must
// be empty or localhost). Set it with [WithEvalStorageURI] or
// --eval_storage_uri; when neither is set, the ADK_EVAL_STORAGE_URI
// environment variable supplies it, as in adk-python. Any other scheme is an
// error at startup.
//
// Override backends with [WithEvalSetsManager] and [WithEvalSetResultsManager]
// (for example in-memory managers in tests).
//
// # Configuration
//
// Options apply when calling [NewLauncher]:
//
//   - [WithAgentsDir] — local eval set and result storage root.
//   - [WithEvalStorageURI] — storage URI (gs://bucket or file:///abs/path) for
//     sets and results; see Storage.
//   - [WithEvalSetsManager] / [WithEvalSetResultsManager] — custom storage backends.
//   - [WithMetricRegistry] — metric evaluators (default [metrics.DefaultRegistry]).
//   - [WithUserSimulatorProvider] — LLM-backed or static user simulators during inference.
//   - [WithStoredOAuthTokens] — let eval runs use the caller's stored tooloauth
//     tokens (off by default; see Identity).
//   - [WithPathPrefix] — URL prefix before /dev/apps/... (default "/api").
//
// CLI flags (after the "evals" keyword on the web command line):
//
//   - -path_prefix — same as [WithPathPrefix].
//   - -agents_dir — same as [WithAgentsDir]; required unless a storage URI is
//     set (flag, option or ADK_EVAL_STORAGE_URI).
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
//
// A metric result may carry status 4 (INFORMATIONAL): the metric reports a
// value but never passes or fails a case.
//
// # Run request and response
//
// The run body is accepted in adk-web main's snake_case form (eval_case_ids,
// eval_metrics) and in the ADK v2.7.1 bundle's camelCase form (evalIds,
// evalMetrics); evalCaseIds is accepted too.
//
// POST .../eval-sets/{id}/run answers {"runEvalResults": [...]}, as adk-python
// does; the legacy POST .../eval_sets/{id}/run_eval answers a bare array.
// adk-web main types the run response as a bare array, a mismatch it also has
// with adk-python, and its History tab loads results through eval_results.
//
// # Run options
//
// Besides the cases and metrics, a run body may set:
//
//   - parallelism — how many cases run, and are scored, at once (default 4;
//     below 1 answers 400). Go only: adk-python's request has no such field.
//   - live_model_config (liveModelConfig) — run inference live, over a
//     bidirectional stream, as Live API models need. Any object, even {},
//     turns it on; timeout_seconds (timeoutSeconds) bounds each turn, 300 s
//     when 0 or absent. null leaves the run unchanged.
//   - user_simulator_config (userSimulatorConfig) — the user simulator, told
//     apart by "type": llm_backed (the default when type is missing) or
//     llm_audio, which speaks every user turn, static or simulated, through
//     an audio model and streams the speech to the agent. The audio model is
//     resolved by name through ADK's model registry (model.NewLLM), once per
//     request, and an unknown name answers 400. adk-python's default,
//     cloud_tts, has no Go implementation: register a model under that name,
//     or name one, such as a Gemini TTS model, in audio_model.
//
// Every run also reports the informational efficiency metrics
// (tool_call_count_v1, inference_call_count_v1, token_usage_v1,
// invocation_duration_v1) without being asked, as adk-python's dev server
// does.
//
// A case's sessionInput.sessionId pins it to a prepared eval session. The
// run reuses that session, keeping its state and history, only when it
// exists under the caller's ID; otherwise it creates a session with that ID
// for the caller, with the case's state. Another user's session of that ID
// is never read. Only eval session IDs (starting adk-eval-session-, or the
// legacy ___eval___session___) can be pinned, unlike adk-python: a case is
// shared, and pinning someone's real session would make their run write into
// it and publish its state in the results. Any other pin starts a fresh
// session, with a log line. With more than one run per case the pin is
// dropped, so the runs do not share a session.
//
// # Eval sessions
//
// Each run's inference session ID starts with adk-eval-session-, a valid
// Agent Engine session ID. [generator.IsEvalSessionID] recognizes it and the
// legacy ___eval___session___ prefix, so a launcher that lists sessions can
// hide eval runs.
//
// # Run-level session state
//
// POST run_eval / run accepts optional session_state (JSON: session_state or
// sessionState): a map of ADK session bootstrap values applied to every case
// in the run (for example idea_name, account_name). Values merge into each
// case's sessionInput.state before inference; case-level keys override run-level
// keys. When both snake_case and camelCase session maps appear in one body,
// keys merge with snake_case winning on conflict.
package evals
