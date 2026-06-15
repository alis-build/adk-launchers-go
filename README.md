# adk-launchers-go

Go modules that extend [Google ADK](https://google.golang.org/adk) with optional **web sublaunchers**. Each sublauncher plugs into `google.golang.org/adk/cmd/launcher/web` and adds HTTP routes or protocols on top of the standard ADK web server.

Use this repository when you need extra capabilities beyond the core ADK launchers—for example streaming to AG-UI frontends, resuming long-running operations from Cloud Tasks, or running scheduled agent prompts in-process.

## Packages

| Package                                | CLI keyword | Purpose                                                                                                                                  |
| -------------------------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| [`agui`](./agui)                       | `agui`      | [AG-UI](https://docs.ag-ui.com) SSE endpoint for CopilotKit and other AG-UI clients                                                      |
| [`agui/clienttool`](./agui/clienttool) | —           | Dynamic `tool.Toolset` for AG-UI client-side tools (agent opt-in, used with `agui`)                                                      |
| [`lro`](./lro)                         | `lro`       | HTTP resume routes for [go.alis.build/lro/v2](https://pkg.go.dev/go.alis.build/lro/v2) long-running operations                           |
| [`scheduler`](./scheduler)             | `scheduler` | [A2A scheduler](https://pkg.go.dev/go.alis.build/a2a/extension/scheduler) cron JSON-RPC and Cloud Tasks callback (in-process ADK runner) |
| [`console`](./console)                 | `console`   | Embedded Vue operator console SPA, runtime config, and `/auth/me` (register **last** in `web.NewLauncher`)                               |

## Quick start

Import the sublaunchers you need and pass them to [`web.NewLauncher`](./web):

```go
import (
    schedulerservice "go.alis.build/a2a/extension/scheduler/service"

    "go.alis.build/adk/launchers/agui"
    "go.alis.build/adk/launchers/console"
    "go.alis.build/adk/launchers/lro"
    "go.alis.build/adk/launchers/scheduler"
    launchersweb "go.alis.build/adk/launchers/web"
    "go.alis.build/iam/v3"
    hostmux "go.alis.build/mux"
    weblauncher "google.golang.org/adk/cmd/launcher/web"
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
adk web --port 8080 agui lro scheduler console -service_id=my-service -app_name=my-agent
```

Importing [`web`](./web) (and the scheduler sublauncher) pulls in `go.alis.build/mux`, which requires `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` at process start.

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

If you replace the bundled SPA, your app must call the same host APIs on the **same origin** (so IAM cookies apply). Register sublaunchers before `console` so API routes are not shadowed by the SPA catch-all.

**Authentication:** Routes marked _(auth)_ require `go.alis.build/mux` IAM (session cookies or `Authorization` bearer). `GET /auth/logout` is handled by the mux identity layer (redirect sign-out), not by package `console`.

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
| DELETE | `/agui/threads/{threadId}`          | _(auth)_ Delete thread (`WithThreadService`)                                                                                                                                                              |
| GET    | `/agui/threads`                     | _(auth)_ Query: `agentId`, `pageSize`, `pageToken`. Thread list with unread/pinned metadata (`WithThreadService`)                                                                                         |

#### History JSON-RPC (`agui` + `WithThreadService`)

`POST /alis.agui.history.v1.ThreadService` — JSON-RPC 2.0: `{"jsonrpc":"2.0","method":"...","params":{...},"id":1}`.

Methods used by the bundled app:

- **UpdateUserThreadState** — `params`: `userThreadState` (`thread`, `readRunCount`, `pinned`) and `updateMask` (comma-separated snake_case field paths, e.g. `"pinned,read_run_count"`)

#### Scheduler (`scheduler` sublauncher)

`POST /alis.a2a.extension.v1.SchedulerService` — JSON-RPC 2.0 (same envelope). Methods used by `/automation`:

- **ListCrons** — `params`: `{pageSize?, pageToken?}`
- **CreateCron** — `params`: `{cron: {...}}` (prompt, expr, timezone, type, at, …)
- **DeleteCron** — `params`: `{name: "crons/<id>"}`
- **RunCron** — `params`: `{id: "<cron-id>"}`

Params are protojson-compatible camelCase. See [`agui/doc.go`](agui/doc.go) and [`scheduler/doc.go`](scheduler/doc.go) for full route and option details.

#### Local Vite dev proxies

When running `cd console/app && pnpm dev`, Vite proxies these paths to `AGENT_HOST` (default `http://localhost:8080`): `/agui`, `/auth`, `/alis.agui.history.v1.ThreadService`, `/alis.a2a.extension.v1.SchedulerService`, `/assets/config/runtime-config.json`.

## Testing

```bash
go test ./...
```

The scheduler package imports `go.alis.build/mux`; set `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` when running tests.

## Requirements

- Go 1.26+
- `google.golang.org/adk` (see [go.mod](./go.mod) for the pinned version)

## License

Apache 2.0 — see [LICENSE](./LICENSE).
