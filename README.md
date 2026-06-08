# adk-launchers-go

Go modules that extend [Google ADK](https://google.golang.org/adk) with optional **web sublaunchers**. Each sublauncher plugs into `google.golang.org/adk/cmd/launcher/web` and adds HTTP routes or protocols on top of the standard ADK web server.

Use this repository when you need extra capabilities beyond the core ADK launchers—for example streaming to AG-UI frontends, resuming long-running operations from Cloud Tasks, or running scheduled agent prompts in-process.

## Packages

| Package                            | CLI keyword | Purpose                                                                                                        |
| ---------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------- |
| [`agui`](./agui) | `agui` | [AG-UI](https://docs.ag-ui.com) SSE endpoint for CopilotKit and other AG-UI clients |
| [`agui/clienttool`](./agui/clienttool) | — | Dynamic `tool.Toolset` for AG-UI client-side tools (agent opt-in, used with `agui`) |
| [`lro`](./lro)   | `lro`  | HTTP resume routes for [go.alis.build/lro/v2](https://pkg.go.dev/go.alis.build/lro/v2) long-running operations |
| [`scheduler`](./scheduler) | `scheduler` | [A2A scheduler](https://pkg.go.dev/go.alis.build/a2a/extension/scheduler) cron JSON-RPC and Cloud Tasks callback (in-process ADK runner) |
| [`console`](./console) | `console` | Embedded Vue operator console SPA, runtime config, and `/auth/me` (register **last** in `web.NewLauncher`) |

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

## Testing

```bash
go test ./...
```

The scheduler package imports `go.alis.build/mux`; set `ALIS_OS_PROJECT` and `IDENTITY_SERVICE_URL` when running tests (see [.vscode/settings.json](./.vscode/settings.json) for local IDE defaults).

## Requirements

- Go 1.26+
- `google.golang.org/adk` (see [go.mod](./go.mod) for the pinned version)

## License

Apache 2.0 — see [LICENSE](./LICENSE).
