# adk-launchers-go

Go modules that extend [Google ADK](https://google.golang.org/adk) with optional **web sublaunchers**. Each sublauncher implements ADK's web `Sublauncher` contract and runs under this repository's [`web`](./web) launcher, which serves them through `go.alis.build/mux` alongside gRPC on one listener. ADK's own sublaunchers (`api`, `webui`, `a2a`) compose the same way.

Use this repository when you need extra capabilities beyond the core ADK launchers—for example streaming to AG-UI frontends, resuming long-running operations from Cloud Tasks, running scheduled agent prompts in-process, or serving the dev evaluation API for adk-web.

## Packages

| Package                                | CLI keyword | Purpose                                                                                                                                                                                             |
| -------------------------------------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`agui`](./agui)                       | `agui`      | [AG-UI](https://docs.ag-ui.com) SSE endpoint for CopilotKit and other AG-UI clients; optional [AgentExecutor](https://pkg.go.dev/go.alis.build/adk/launchers/agui#AgentExecutor) via `WithExecutor` |
| [`agui/clienttool`](./agui/clienttool) | —           | Dynamic `tool.Toolset` for AG-UI client-side tools (agent opt-in, used with `agui`)                                                                                                                 |
| [`lro`](./lro)                         | `lro`       | HTTP resume routes for [go.alis.build/lro/v2](https://pkg.go.dev/go.alis.build/lro/v2) long-running operations                                                                                      |
| [`scheduler`](./scheduler)             | `scheduler` | [AG-UI scheduler](https://pkg.go.dev/go.alis.build/agui/scheduler) cron JSON-RPC and Cloud Tasks callback (in-process ADK runner)                                                                   |
| [`console`](./console)                 | `console`   | Embedded Vue operator console SPA, runtime config, and `/auth/me` (register **last** in `web.NewLauncher`)                                                                                          |
| [`evals`](./evals)                     | `evals`     | Dev evaluation HTTP API (eval sets, run eval, metrics-info) with full adk-python evaluation engine parity                                                                                           |
| [`tooloauth`](./tooloauth)             | —           | Runner plugin giving tools the end user's OAuth token: sign-in mid-run, refresh, and token storage ([`tooloauth/spannerstore`](./tooloauth/spannerstore) for Spanner + Cloud KMS)                   |

## Quick start

Import the sublaunchers you need and pass them to [`web.NewLauncher`](./web):

```go
import (
    schedulerservice "go.alis.build/agui/scheduler/service"

    "go.alis.build/adk/launchers/agui"
    "go.alis.build/adk/launchers/console"
    "go.alis.build/adk/launchers/evals"
    "go.alis.build/adk/launchers/lro"
    "go.alis.build/adk/launchers/scheduler"
    launchersweb "go.alis.build/adk/launchers/web"
    "go.alis.build/iam/v3"
    hostmux "go.alis.build/mux"
    "google.golang.org/grpc"
)

// Host constructs SchedulerService (Spanner, Cloud Tasks, etc.) — see scheduler/doc.go.
schedSvc, err := schedulerservice.NewSchedulerService(ctx, &schedulerservice.SchedulerServiceConfig{ /* ... */ })
if err != nil {
    log.Fatal(err)
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(schedulerservice.UnaryServerInterceptor()),
)

web := launchersweb.NewLauncher(
    agui.NewLauncher("my-agent", agui.WithCORS(agui.CORSConfig{
        AllowedOrigins: []string{"http://localhost:3000"},
    })),
    lro.NewLauncher(lro.WithServiceID("my-service")),
    scheduler.NewLauncher("my-agent", schedSvc,
        scheduler.WithCronIdentity(&iam.Identity{
            ID:    "alis-build@my-project.iam.gserviceaccount.com",
            Email: "alis-build@my-project.iam.gserviceaccount.com",
            Type:  iam.ServiceAccount,
        }),
        scheduler.WithGRPCRegistrar(grpcServer),
    ),
    evals.NewLauncher(evals.WithAgentsDir("./agents")),
    console.NewLauncher(console.WithBranding(console.Branding{
        Title:       "My Console",
        DisplayName: "My Console",
        Favicon:     console.URLAsset("/my-agent/branding/favicon.ico"),
        Logo:        console.URLAsset("/my-agent/branding/logo.svg"),
    })),
)

hostmux.HandleGRPC(grpcServer)
```

At runtime, activate sublaunchers by keyword on the `adk web` command line, for example:

```bash
adk web --port 8080 agui lro scheduler evals console -service_id=my-service -app_name=my-agent -agents_dir=./agents
```

The web launcher also serves `GET /health`, and takes `-max_request_body_size` and a repeatable `-allow_origins`; see the [`web` package docs](./web) for what each covers.

Importing [`web`](./web) (and the scheduler sublauncher) pulls in `go.alis.build/mux`, which requires `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` at process start.

## Authentication and authorization

The launchers **authorize but do not authenticate**. Authentication is expected to have already happened at a trusted upstream (a gateway or BFF) before a request reaches the process; that upstream forwards the caller identity as the `x-alis-identity` header.

To make the forwarded identity available to handlers, `web.NewLauncher` installs `web.DefaultAuthGateway` as the process-wide `go.alis.build/mux` gateway. It reads the identity from the request headers and injects it into the request context so handlers can call `iam.FromContext` / `iam.MustFromContext` to authorize the caller. It performs **no** authentication and does not reject requests that lack an identity — each handler decides whether an identity is required and fails closed when it is missing.

Customize or disable the gateway with `web.NewLauncherWithOptions`:

```go
// Provide a custom gateway (for example to also forward identity to a downstream gRPC server).
launchersweb.NewLauncherWithOptions(
    []launchersweb.Sublauncher{ /* ... */ },
    launchersweb.WithAuthGateway(myGateway),
)

// Or disable it entirely and install your own with alismux.AddGateway.
launchersweb.NewLauncherWithOptions(
    []launchersweb.Sublauncher{ /* ... */ },
    launchersweb.WithoutAuthGateway(),
)
```

`go.alis.build/mux` supports a single gateway per process, so the web launcher owns that slot.

**Scheduler cron callback.** The Cloud Tasks cron callback (`.../handler`) is privileged — it runs agents and impersonates the cron owner — so it is authenticated in-launcher as the environment service account by default. When a trusted upstream already authenticates that caller (and the endpoint is not directly reachable), opt out with `scheduler.WithoutSystemAuth()`.

**ADK's REST API.** With ADK's `api` sublauncher, the web launcher fills `launcher.Config.Authenticator` with `web.IdentityAuthenticator()` and `Authorizer` with `authz.NewStrict()` when you leave them nil. A request without an identity answers 401, and a caller acting as another user answers 403; `/api/health` stays open. There is no flag to loosen it. In code, `authz.NewNoop()` lets any signed-in caller act as any user, and a fully open API also needs `authn.NewNoop()`. ADK's A2A routes are not covered and have no authentication.

**ADK's web UI.** The UI acts as user `"user"` unless loaded with `?userId=`, so the launcher redirects `/ui/` to `/ui/?userId=<caller>` and the UI then sends the caller's real ID. The UI itself sends no credentials, so **the upstream gateway must forward `x-alis-identity` on page navigations and on the `/run_live` WebSocket upgrade**, not only on API calls; otherwise every call naming a user answers 403 (a refused chat shows as an empty stream), and a `/run_live` upgrade without an identity answers 401.

**Evals.** Every eval route requires an identity. add-session and runs act only as the caller, on the route's app; reads of eval cases and results strip credential secrets.

**Trust boundary.** Because the launchers no longer authenticate, the `x-alis-identity` header must only ever be set by the trusted upstream. Deployments must ensure the process is not directly reachable and that the header is stripped or overwritten at the edge.

### Console embed build

The embedded SPA lives in `console/app/dist` (versioned in git). Rebuild with:

```bash
cd console/app && pnpm build
# or
go generate ./console/...
```

A Husky pre-commit hook rebuilds `dist`, runs frontend tests, and re-stages `console/app/dist` when any `console/app/` files are committed (`pnpm install` in `console/app` wires hooks via `prepare`).

### Local console development

By default the console serves the **embedded** `app/dist` (rebuild with `pnpm build`, then recompile Go). To use Vite HMR on the agent host:

```bash
cd console/app && pnpm dev   # port 8000
SPA_DEV_SERVER_URL=http://localhost:8000 adk web --port 8080 agui scheduler console
```

Unset `SPA_DEV_SERVER_URL` to test the production embed locally. Use `console.WithIsLocal(...)` in code to force either mode.

### Custom console dist

By default the console serves the **embedded** `app/dist`. To serve a custom production build (for example from a Docker frontend stage):

```go
console.NewLauncher(
    console.WithDist(console.DirDist("dist")),
)
```

Or set `SPA_DIST_DIR=./dist` when `WithDist` is not used. Other built-in resolvers: `DefaultDist()`, `EmbedDist(fs, root)`, and `HandlerDist(http.Handler)`.

Dev mode (`SPA_DEV_SERVER_URL`) always takes precedence over any custom dist.

### Backend API contract (custom dist)

If you replace the bundled SPA, your app must call the same host APIs on the **same origin**. Register sublaunchers before `console` so API routes are not shadowed by the SPA catch-all.

**Authorization:** Routes marked _(auth)_ require a caller identity in the request context (resolved from the upstream `x-alis-identity` header by the web launcher's auth gateway — see [Authentication and authorization](#authentication-and-authorization)) and fail closed with `401` when none is present. `GET /auth/logout` is handled by the `go.alis.build/mux` identity layer (redirect sign-out), not by package `console`.

#### Console (`console` sublauncher)

| Method | Path                                 | Description                                                         |
| ------ | ------------------------------------ | ------------------------------------------------------------------- |
| GET    | `/assets/config/runtime-config.json` | _(auth)_ Agents, `defaultAgentId`, `gcpProject`, shell `branding`   |
| GET    | `/auth/me`                           | _(auth)_ `{"sub":"...","email":"..."}`                              |
| GET    | `/`                                  | _(auth)_ SPA static files (catch-all)                               |
| GET    | `/console/branding/*`                | _(auth)_ Optional logo/favicon when using `EmbedAsset` / `DirAsset` |

`runtime-config.json` shape matches [`console/app/src/runtimeConfig.ts`](console/app/src/runtimeConfig.ts).

#### AG-UI (`agui` sublauncher, default prefix `/agui`)

Requires `agui` on the `adk web` command line. Change prefix with `agui -path_prefix=/api/agui`.

| Method | Path                                | Description                                                                                                                                                                                               |
| ------ | ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| POST   | `/agui/run_sse`                     | _(auth)_ JSON **RunAgentInput** body → `text/event-stream` (AG-UI SSE). Bundled app passes `context: [{description:"app", value: agentId}]` for multi-agent routing. [AG-UI spec](https://docs.ag-ui.com) |
| GET    | `/agui/threads/{threadId}/messages` | _(auth)_ Query: `agentId`, `after` (RFC 3339), `limit`. JSON: `{"messages":[],"nextCursor":"..."}` or SSE if `Accept: text/event-stream`                                                                  |
| GET    | `/agui/threads/{threadId}`          | _(auth)_ Single thread metadata (`WithThreadService`)                                                                                                                                                     |
| DELETE | `/agui/threads/{threadId}`          | _(auth)_ Delete thread metadata and backing ADK session (`WithThreadService`)                                                                                                                             |
| GET    | `/agui/threads`                     | _(auth)_ Query: `agentId`, `pageSize`, `pageToken`. Thread list with unread/pinned metadata (`WithThreadService`)                                                                                         |

#### History JSON-RPC (`agui` + `WithThreadService`)

`POST /alis.agui.history.v1.ThreadService` — JSON-RPC 2.0: `{"jsonrpc":"2.0","method":"...","params":{...},"id":1}`.

Methods used by the bundled app:

- **UpdateUserThreadState** — `params`: `userThreadState` (`thread`, `readRunCount`, `pinned`) and `updateMask` (comma-separated snake_case field paths, e.g. `"pinned,read_run_count"`)

#### Scheduler (`scheduler` sublauncher)

`POST /alis.agui.scheduler.v1.SchedulerService` — JSON-RPC 2.0 (same envelope). Methods used by `/automation`:

- **ListCrons** — `params`: `{pageSize?, pageToken?}`
- **CreateCron** — `params`: `{cron: {...}}` (prompt, expr, timezone, type, at, …)
- **DeleteCron** — `params`: `{name: "crons/<id>"}`
- **RunCron** — `params`: `{id: "<cron-id>"}`

Params are protojson-compatible camelCase. See [`agui/doc.go`](agui/doc.go) and [`scheduler/doc.go`](scheduler/doc.go) for full route and option details.

> **Migrating from the A2A scheduler?** The `scheduler` sublauncher now targets [`go.alis.build/agui/scheduler`](https://pkg.go.dev/go.alis.build/agui/scheduler). Update:
>
> - Proto import: `go.alis.build/common/alis/agui/scheduler/v1` (was `.../a2a/extension/scheduler/v1`).
> - gRPC service and JSON-RPC / Cloud Tasks paths: `alis.agui.scheduler.v1.SchedulerService` (was `alis.a2a.extension.v1.SchedulerService`). Repoint Cloud Tasks `TargetUrl` to `/alis.agui.scheduler.v1.SchedulerService/handler`.
> - Cron session model: `context_id` → `thread` (`threads/{thread_id}`).
> - The stock A2A loopback handler is dropped; cron ticks now run the ADK app in-process.
>
> See [`scheduler/doc.go`](scheduler/doc.go) for the full migration notes and new options (`agent_id`, `metadata`, `WithThreadService`, `WithCronRunInterceptor`).

#### Evals (`evals` sublauncher)

Requires `evals` on the `adk web` command line. By default eval sets and results live under `-agents_dir=./agents` ([evals.WithAgentsDir](./evals)). A storage URI moves them: `-eval_storage_uri=gs://bucket` for Cloud Storage or `-eval_storage_uri=file:///abs/path` for a local directory ([evals.WithEvalStorageURI](./evals)); when neither is set, the `ADK_EVAL_STORAGE_URI` environment variable supplies it, as in adk-python.

This sublauncher is separate from [`console`](./console): it serves the dev evaluation API for adk-web, not the operator console SPA. Every route requires a caller identity; see [Authentication and authorization](#authentication-and-authorization).

All routes are under `/api/dev/apps/{appName}/...` by default (match webui `-api_server_address=/api`). Override with `-path_prefix` or [evals.WithPathPrefix](./evals). adk-web uses the legacy underscore paths (`eval_sets`, `run_eval`, etc.); canonical hyphen paths are also registered.

| Method         | Path (legacy)                                       | Description                    |
| -------------- | --------------------------------------------------- | ------------------------------ |
| GET            | `/api/dev/apps/{app}/eval_sets`                     | List eval set IDs              |
| POST           | `/api/dev/apps/{app}/eval-sets`                     | Create eval set                |
| GET            | `/api/dev/apps/{app}/eval_sets/{id}`                | Get eval set                   |
| DELETE         | `/api/dev/apps/{app}/eval_sets/{id}`                | Delete eval set                |
| GET            | `/api/dev/apps/{app}/eval-sets/{id}/eval-cases`     | List eval case IDs (canonical) |
| GET            | `/api/dev/apps/{app}/eval_sets/{id}/evals`          | List eval case IDs (legacy)    |
| GET/PUT/DELETE | `/api/dev/apps/{app}/eval_sets/{id}/evals/{caseId}` | CRUD eval case                 |
| POST           | `/api/dev/apps/{app}/eval_sets/{id}/add_session`    | Session → eval case            |
| POST           | `/api/dev/apps/{app}/eval-sets/{id}/run`            | Run inference + evaluate; answers `{"runEvalResults": [...]}` (canonical) |
| POST           | `/api/dev/apps/{app}/eval_sets/{id}/run_eval`       | Run inference + evaluate; answers a bare array (legacy) |
| GET            | `/api/dev/apps/{app}/eval_results`                  | List result IDs                |
| GET            | `/api/dev/apps/{app}/eval_results/{resultId}`       | Get eval result                |
| GET            | `/api/dev/apps/{app}/metrics-info`                  | Registered metric metadata     |

A run body may also set `parallelism` (cases run at once; Go only), `live_model_config` (run inference live; any object, even `{}`, turns it on) and `user_simulator_config` (`llm_backed`, or `llm_audio` to speak every user turn through an audio model resolved from ADK's model registry; adk-python's default `cloud_tts` has no Go implementation, so register one or name another). Every run reports the efficiency metrics (tool calls, model calls, tokens, duration). A case's `sessionInput.sessionId` reuses a prepared session only when it is an eval session (`adk-eval-session-…`) and the caller's own.

Runs execute as the caller, so an agent that reads `user:` state or the memory service can score differently for different people. Seed what the agent needs in the case's session state, or compare results only within one caller's runs. For the same reason, eval runs use no stored [`tooloauth`](./tooloauth) token unless the embedder passes `evals.WithStoredOAuthTokens()`: otherwise an eval of a tool that writes would act on the caller's real account.

See [`evals/doc.go`](evals/doc.go) for routes, request and response shapes, and storage options.

#### AG-UI architecture

The `agui` sublauncher splits each `/run_sse` request into two layers:

1. **Transport** — HTTP/SSE, identity, CORS, and [`CallInterceptor`](./agui/interceptor.go) hooks (`Before`, `OnEmit`, `After`). `OnEmit` is the only supported place to mutate or suppress events on the wire.
2. **Protocol** — [`AgentExecutor`](./agui/executor.go) yields AG-UI events (`RunStarted` → ADK run → `RunFinished` / `RunError`). The default executor handles interrupts, snapshots, and session persistence.

Most apps need no custom executor. Use [`WithExecutor`](./agui/agui.go) to configure callbacks, wrap the default pipeline, or replace it entirely:

```go
agui.NewLauncher("my-agent",
    agui.WithExecutor(func(d agui.ExecutorDeps) agui.AgentExecutor {
        return d.NewDefault(agui.ExecutorConfig{
            AfterExecuteCallback: func(_ agui.ExecutorContext, err error) error {
                log.Printf("run finished: %v", err)
                return nil
            },
        })
    }),
)
```

[`WithGenAIPartConverter`](./agui/agui.go) still works without `WithExecutor` (merged into the default config). When `WithExecutor` is set, the factory owns all executor settings including the part converter. See [`agui/doc.go`](agui/doc.go) for configure, decorate, and replace patterns.

#### Local Vite dev proxies (console only)

When running `cd console/app && pnpm dev`, Vite proxies console-related paths to `AGENT_HOST` (default `http://localhost:8080`): `/agui`, `/auth`, `/alis.agui.history.v1.ThreadService`, `/alis.agui.scheduler.v1.SchedulerService`, `/assets/config/runtime-config.json`. Eval routes (`/api/dev/...`) are served by the [`evals`](./evals) sublauncher on the agent host and are not part of the console Vite proxy; adk-web calls them directly.

## Tool OAuth (tooloauth)

[`tooloauth`](./tooloauth) is a runner plugin, not a sublauncher: add `tooloauth.NewPlugin(cfg)` to `launcher.Config.PluginConfig`. Tools named in its config get the end user's OAuth token through `cfg.Provider(name)` and ADK's `auth.Transport`. With no token the call pauses for sign-in; AG-UI clients see an `auth_required` interrupt, open its authorization URI and resume with the provider's redirect URL. Scheduled and eval runs never pause; they get `connect <Provider> in chat first`. Keep tokens in [`tooloauth/spannerstore`](./tooloauth/spannerstore) (Spanner + Cloud KMS) or your own `auth.CredentialStore`. See [`tooloauth/doc.go`](tooloauth/doc.go) for setup, the resume flow and limits.

## Testing

```bash
ALIS_OS_PROJECT=test IDENTITY_SERVICE_URL=http://localhost go test ./...
```

Packages that import `go.alis.build/mux` (including [`agui`](./agui), [`scheduler`](./scheduler), and [`evals`](./evals)) require `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` at test time.

## Contributing

While the module is below **v1.0.0**, open pull requests and land work on the **`v0`** branch. After v1.0.0, use **`main`** for ongoing development.

## Requirements

- Go 1.26.10+ (required by `google.golang.org/adk/v2` v2.7)
- `google.golang.org/adk/v2` (see [go.mod](./go.mod) for the pinned version)

### Upgrading to ADK v2.7

ADK v2.4 to v2.7 change runtime behaviour that tests do not catch:

- **Model-call budget.** One invocation may make at most 500 model calls; past that the run ends with `agent.ErrLLMCallsLimitExceeded` (an AG-UI `RunError`, a failed cron tick, a failed eval). Raise it per deployment with `ADK_MAX_LLM_CALLS`, or set it to a negative number for no limit. With `scheduler.WithSynchronousExecution`, a budget-exhausted tick is acknowledged rather than retried, since a retry would spend the budget again.
- **Telemetry schema.** Traces follow the OpenTelemetry GenAI semantic conventions v2. Dashboards and alerts built on the old fields need updating; `ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN=otel_semconv_1_36` restores the old format until at least March 31, 2027.
- **ADK's REST API refuses cross-origin browsers** whose origin is not allowed. Add origins with the web launcher's `-allow_origins` or `launcher.Config.AllowedOrigins`. This affects ADK's `api` sublauncher only; agui, scheduler, evals and console keep their own CORS options.
- **Optional instruction placeholders.** `{key?}` and `{artifact.name?}` render empty only when the key or artifact is missing; any other session or artifact service error now fails the run, and an `agenttool` sub-agent with an optional artifact placeholder fails when no artifact service is configured. The web launcher does not default one, so configure a real service.
- **Eval scores can change without any agent change.** Criterion thresholds now grade HTTP runs (many used to grade against 0 and pass), thought parts no longer count as response text, a partial judge reply scores nothing, and an informational metric no longer passes or fails a case. Compare results from before and after the update with that in mind.
- **Memory service default.** The web launcher defaults a missing memory service to an in-memory one (ADK's REST API panics without one). That config is shared, so agui, scheduler and evals get it too: a memory tool that used to fail without a service now gets instance-local results that are lost on restart. Configure a real memory service in production.

## License

Apache 2.0 — see [LICENSE](./LICENSE).
