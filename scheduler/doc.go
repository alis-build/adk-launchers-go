// Package scheduler is an ADK web sublauncher for the AG-UI scheduler extension
// (go.alis.build/agui/scheduler).
//
// It registers two HTTP surfaces on go.alis.build/mux (via [go.alis.build/adk/launchers/web]):
//
//   - JSON-RPC at /alis.agui.scheduler.v1.SchedulerService for cron CRUD (POST + OPTIONS).
//   - Cloud Tasks callback at /alis.agui.scheduler.v1.SchedulerService/handler for cron execution.
//
// When Cloud Tasks invokes the handler, this package runs the configured ADK app
// in-process through [go.alis.build/adk/launchers/internal/adkrun] instead of
// looping back over the stock scheduler extension gRPC handler.
//
// Callers upgrading from the A2A scheduler should read the "Migration from the
// A2A scheduler" section below for the required import, proto, and Cloud Tasks
// URL changes.
//
// # Authentication and authorization
//
// The JSON-RPC surface is registered without authentication: the launcher
// authorizes but does not authenticate. The caller identity is expected to be in
// the request context already (the web launcher's authorization gateway resolves
// it from the upstream x-alis-identity header), and SchedulerService enforces
// authorization with iam.MustFromContext + authz.
//
// The Cloud Tasks callback is different. It is privileged — it runs agents and
// impersonates the cron owner — so by default it is authenticated in-launcher
// with alismux.SystemPost, which validates the inbound Google ID token and
// requires the environment service account. Use [WithoutSystemAuth] to delegate
// that check to a trusted upstream when the callback is not directly reachable.
//
// # Host responsibilities
//
// The sublauncher does not construct infrastructure. The host must:
//
//   - Build [schedulerservice.SchedulerService] (Spanner, Cloud Tasks queue, TargetUrl, etc.).
//   - Pass the service and ADK app name to [NewLauncher].
//   - Mount native gRPC on the host mux (hostmux.HandleGRPC or SystemHandleGRPC).
//   - Register SchedulerService on the host grpc.Server via [WithGRPCRegistrar], or
//     schedulerext.RegisterGRPC(grpcServer, l.SchedulerService()) manually (not both).
//   - Add [schedulerservice.UnaryServerInterceptor] to the grpc.Server so caller
//     identity (iam/v3) is available to SchedulerService methods.
//   - Compose the launcher: launchersweb.NewLauncher(..., scheduler.NewLauncher(...)).
//   - Provide [WithCronIdentity] (recommended) or set ALIS_OS_PROJECT for the default SA.
//
// # Execution model
//
// On each cron tick the handler:
//
//  1. Uses a system IAM identity for GetCron and UpdateCron.
//  2. Impersonates the cron owner for ADK runs (user id + cron email).
//  3. Optionally runs initial_prompt once for new recurring crons, then prompt.
//  4. Persists thread (threads/{session_id}), last_run_time, and archives TYPE_AT crons.
//     Failed runs persist last_failure_time and last_failure_message; TYPE_AT failures
//     are archived so they do not remain active without a backing Cloud Task.
//
// Default HTTP behavior matches the stock extension: return 200 immediately and run
// asynchronously. [WithSynchronousExecution] blocks until the ADK run completes;
// agent failures return 500 (Cloud Tasks may retry), but cron persist failures
// (UpdateCron) are logged and return 200 to prevent duplicate agent execution.
//
// Unlike the stock extension handler, this package applies stricter validation:
// cron prompt and initial_prompt are trimmed before use (whitespace-only is rejected),
// and owner must have an explicit "users/" prefix.
//
// # Options
//
//   - [WithCronIdentity] — system principal for SchedulerService RPCs in the handler.
//   - [WithJSONRPCOptions] — forwarded to the extension JSON-RPC handler (e.g. CORS).
//   - [WithSynchronousExecution] — sync ADK run; 500 on agent failure, 200 on persist failure.
//   - [WithCronObserver] — lifecycle hooks around in-process execution.
//   - [WithCronRunInterceptor] / [WithCronRunInterceptors] — hooks around each ADK invocation.
//   - [WithThreadService] — upsert thread metadata on cron runs (same instance as AGUI launcher).
//   - [WithGRPCRegistrar] — register SchedulerService on the host grpc.Server during setup.
//   - [WithoutSystemAuth] — delegate cron callback authentication to a trusted upstream.
//
// # Example
//
//	import (
//	    schedulerservice "go.alis.build/agui/scheduler/service"
//	    "go.alis.build/adk/launchers/scheduler"
//	    launchersweb "go.alis.build/adk/launchers/web"
//	    "go.alis.build/iam/v3"
//	    hostmux "go.alis.build/mux"
//	    "google.golang.org/grpc"
//	)
//
//	grpcServer := grpc.NewServer(
//	    grpc.UnaryInterceptor(schedulerservice.UnaryServerInterceptor()),
//	)
//	sched := scheduler.NewLauncher("my.agent", svc,
//	    scheduler.WithCronIdentity(&iam.Identity{
//	        ID:    "alis-build@my-project.iam.gserviceaccount.com",
//	        Email: "alis-build@my-project.iam.gserviceaccount.com",
//	        Type:  iam.ServiceAccount,
//	    }),
//	    scheduler.WithGRPCRegistrar(grpcServer),
//	)
//	launchersweb.NewLauncher(webapi.NewLauncher(), sched)
//	hostmux.HandleGRPC(grpcServer)
//
// CLI: adk web --port 8080 api scheduler -app_name=my.agent
//
// # Multi-agent crons
//
// Set cron.agent_id to run a specific ADK app; when empty, the app name passed to
// [NewLauncher] is used. The resolved app name is validated against the launcher's
// AgentLoader when one is configured, so unknown agent ids fail fast without
// invoking the runtime.
//
// # Thread history integration
//
// [WithThreadService] wires a shared [historyservice.ThreadService] instance into
// the cron handler. On every tick the handler upserts thread metadata (display
// name, most recent user message) so scheduled runs appear alongside interactive
// /run_sse runs in the AGUI history listing.
//
// Pass the same *ThreadService value as [go.alis.build/adk/launchers/agui.WithThreadService];
// otherwise scheduled and interactive runs will not coexist in the listing (they
// will write to different service instances that may not share a backend view of
// the same threads). The upsert runs both before the ADK invocation (using the
// pre-run session id) and after a successful invocation (using the id returned by
// ADK, which is what future ticks reuse via cron.thread).
//
// # Run interceptors
//
// [WithCronRunInterceptor] and [WithCronRunInterceptors] register
// [CronRunInterceptor] implementations that fire around each ADK invocation.
// This is a finer granularity than [WithCronObserver]: an observer wraps the
// whole cron tick, while an interceptor wraps each ADK call inside it. Recurring
// crons with a non-empty initial_prompt therefore see two BeforeRun/AfterRun
// pairs on the first tick — one with [CronRunInitial] and one with
// [CronRunScheduled] — and one pair per tick after that.
//
// BeforeRun may mutate CronRunContext.Prompt, AppName, and StateDelta before the
// ADK call. It may not usefully mutate Cron or Metadata; those are passed through
// to the executor and observed by later ticks only if persisted via
// SchedulerService. See [CronRunInterceptor] for ordering and error semantics.
//
// # Cron metadata
//
// cron.metadata (google.protobuf.Struct) is exposed on [CronRunContext.Metadata]
// so interceptors can carry structured, cron-scoped configuration (routing keys,
// feature flags, tenant ids, etc.) into each ADK invocation without threading it
// through the prompt. The field is read-only in BeforeRun; the executor does not
// forward Metadata to ADK on its own.
//
// # Migration from the A2A scheduler
//
// This package previously targeted the A2A scheduler extension
// (go.alis.build/a2a/extension/scheduler). It now targets go.alis.build/agui/scheduler.
// Notable changes for callers upgrading:
//
//   - Proto import path: go.alis.build/common/alis/agui/scheduler/v1 (was
//     go.alis.build/common/alis/a2a/extension/scheduler/v1).
//   - gRPC service: alis.agui.scheduler.v1.SchedulerService (was
//     alis.a2a.extension.v1.SchedulerService). JSON-RPC and Cloud Tasks handler
//     paths are derived from the service name and change accordingly.
//   - Cloud Tasks TargetUrl: point at the new /alis.agui.scheduler.v1.SchedulerService/handler
//     path when re-provisioning the queue.
//   - Cron session model: Cron.context_id is replaced by Cron.thread, formatted
//     as threads/{thread_id}. Existing sessions must be migrated to the new field.
//   - The stock A2A loopback handler is no longer used: this package runs the
//     agent in-process on every tick.
//   - New Cron fields: agent_id (per-cron ADK app selection) and metadata
//     (google.protobuf.Struct, exposed via [CronRunContext.Metadata]).
package scheduler
