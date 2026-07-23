// Package scheduler is an ADK web sublauncher for the A2A scheduler extension
// (go.alis.build/a2a/extension/scheduler).
//
// It registers two HTTP surfaces on go.alis.build/mux (via [go.alis.build/adk/launchers/web]):
//
//   - JSON-RPC at /alis.a2a.extension.v1.SchedulerService for cron CRUD (POST + OPTIONS).
//   - Cloud Tasks callback at /alis.a2a.extension.v1.SchedulerService/handler for cron execution.
//
// When Cloud Tasks invokes the handler, this package runs the configured ADK app
// in-process through [go.alis.build/adk/launchers/internal/adkrun] instead of the
// stock extension handler that loops back over A2A gRPC.
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
//  4. Persists context_id (ADK session id), last_run_time, and archives TYPE_AT crons.
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
//   - [WithThreadService] — upsert thread metadata on cron runs (same instance as AGUI launcher).
//   - [WithGRPCRegistrar] — register SchedulerService on the host grpc.Server during setup.
//   - [WithoutSystemAuth] — delegate cron callback authentication to a trusted upstream.
//
// # Example
//
//	import (
//	    historyservice "go.alis.build/agui/history/service"
//	    schedulerservice "go.alis.build/a2a/extension/scheduler/service"
//	    "go.alis.build/adk/launchers/agui"
//	    "go.alis.build/adk/launchers/scheduler"
//	    launchersweb "go.alis.build/adk/launchers/web"
//	    "go.alis.build/iam/v3"
//	    hostmux "go.alis.build/mux"
//	    "google.golang.org/grpc"
//	)
//
//	historySvc := historyservice.New(/* ... */)
//	grpcServer := grpc.NewServer(
//	    grpc.UnaryInterceptor(schedulerservice.UnaryServerInterceptor()),
//	)
//	sched := scheduler.NewLauncher("my.agent", svc,
//	    scheduler.WithCronIdentity(&iam.Identity{
//	        ID:    "alis-build@my-project.iam.gserviceaccount.com",
//	        Email: "alis-build@my-project.iam.gserviceaccount.com",
//	        Type:  iam.ServiceAccount,
//	    }),
//	    scheduler.WithThreadService(historySvc),
//	    scheduler.WithGRPCRegistrar(grpcServer),
//	)
//	launchersweb.NewLauncher(
//	    webapi.NewLauncher(),
//	    agui.NewLauncher("my.agent", agui.WithThreadService(historySvc)),
//	    sched,
//	)
//	hostmux.HandleGRPC(grpcServer)
//
// CLI: adk web --port 8080 api scheduler -app_name=my.agent
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
// the same threads).
//
// Upsert timing: when cron.context_id (or the in-tick session id) is already set,
// metadata is upserted before the ADK run. On the first tick of a new cron both ids
// are empty, so the pre-run upsert is skipped and the thread is created after ADK
// returns a new session id. When ADK returns a different session id than the
// pre-run thread id, a second upsert runs after a successful invocation (future
// ticks reuse that id via cron.context_id).
//
// # Multi-agent crons (TODO)
//
// Per-cron agent_id is not implemented yet (proto field + executeCron →
// RunRequest.AppName + LoadAgent). Until then, all crons use the app name
// passed to NewLauncher.
package scheduler
