# adk-launchers-go

Go modules that extend [Google ADK](https://google.golang.org/adk) with optional **web sublaunchers**. Each sublauncher plugs into `google.golang.org/adk/v2/cmd/launcher/web` and adds HTTP routes or protocols on top of the standard ADK web server.

Use this repository when you need extra capabilities beyond the core ADK launchers—for example streaming to AG-UI frontends, resuming long-running operations from Cloud Tasks, running scheduled agent prompts in-process, or serving the dev evaluation API for adk-web.

## Packages

| Package                                | CLI keyword | Purpose                                                                                                                                                                                             |
| -------------------------------------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`agui`](./agui)                       | `agui`      | [AG-UI](https://docs.ag-ui.com) SSE endpoint for CopilotKit and other AG-UI clients; optional [AgentExecutor](https://pkg.go.dev/go.alis.build/adk/launchers/agui#AgentExecutor) via `WithExecutor` |
| [`agui/clienttool`](./agui/clienttool) | —           | Dynamic `tool.Toolset` for AG-UI client-side tools (agent opt-in, used with `agui`)                                                                                                                 |
| [`lro`](./lro)                         | `lro`       | HTTP resume routes for [go.alis.build/lro/v2](https://pkg.go.dev/go.alis.build/lro/v2) long-running operations                                                                                      |
| [`scheduler`](./scheduler)             | `scheduler` | [AG-UI scheduler](https://pkg.go.dev/go.alis.build/agui/scheduler) cron JSON-RPC and Cloud Tasks callback (in-process ADK runner)                                                            |
| [`console`](./console)                 | `console`   | Embedded Vue operator console SPA, runtime config, and `/auth/me` (register **last** in `web.NewLauncher`)                                                                                          |
| [`evals`](./evals)                     | `evals`     | Dev evaluation HTTP API (eval sets, run eval, metrics-info) with full adk-python evaluation engine parity                                                                                            |

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
    weblauncher "google.golang.org/adk/v2/cmd/launcher/web"
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
    weblauncher.NewLauncher(),
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

Requires `evals` on the `adk web` command line. Local storage needs `-agents_dir=./agents` or [evals.WithAgentsDir](./evals). GCS storage: `-eval_storage_uri=gs://bucket` or [evals.WithEvalStorageURI](./evals).

This sublauncher is separate from [`console`](./console): it serves the dev evaluation API for adk-web, not the operator console SPA. Routes do not enforce caller identity; restrict access at a trusted upstream.

All routes are under `/api/dev/apps/{appName}/...` by default (match webui `-api_server_address=/api`). Override with `-path_prefix` or [evals.WithPathPrefix](./evals). adk-web uses the legacy underscore paths (`eval_sets`, `run_eval`, etc.); canonical hyphen paths are also registered.

| Method | Path (legacy) | Description |
| ------ | ------------- | ----------- |
| GET | `/api/dev/apps/{app}/eval_sets` | List eval set IDs |
| POST | `/api/dev/apps/{app}/eval-sets` | Create eval set |
| GET | `/api/dev/apps/{app}/eval_sets/{id}` | Get eval set |
| DELETE | `/api/dev/apps/{app}/eval_sets/{id}` | Delete eval set |
| GET | `/api/dev/apps/{app}/eval-sets/{id}/eval-cases` | List eval case IDs (canonical) |
| GET | `/api/dev/apps/{app}/eval_sets/{id}/evals` | List eval case IDs (legacy) |
| GET/PUT/DELETE | `/api/dev/apps/{app}/eval_sets/{id}/evals/{caseId}` | CRUD eval case |
| POST | `/api/dev/apps/{app}/eval_sets/{id}/add_session` | Session → eval case |
| POST | `/api/dev/apps/{app}/eval_sets/{id}/run_eval` | Run inference + evaluate |
| GET | `/api/dev/apps/{app}/eval_results` | List result IDs |
| GET | `/api/dev/apps/{app}/eval_results/{resultId}` | Get eval result |
| GET | `/api/dev/apps/{app}/metrics-info` | Registered metric metadata |

See [`evals/doc.go`](evals/doc.go) for routes and storage options.

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

## Testing

```bash
ALIS_OS_PROJECT=test IDENTITY_SERVICE_URL=http://localhost go test ./...
```

Packages that import `go.alis.build/mux` (including [`agui`](./agui), [`scheduler`](./scheduler), and [`evals`](./evals)) require `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` at test time.

## Contributing

While the module is below **v1.0.0**, open pull requests and land work on the **`v0`** branch. After v1.0.0, use **`main`** for ongoing development.

## Requirements

- Go 1.26+
- `google.golang.org/adk/v2` (see [go.mod](./go.mod) for the pinned version)

## License

Apache 2.0 — see [LICENSE](./LICENSE).
