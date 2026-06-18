# agui

ADK web sublauncher for the [AG-UI protocol](https://docs.ag-ui.com). Streams agent runs over SSE for CopilotKit and other AG-UI clients.

Full API documentation: [`doc.go`](doc.go) (also on [pkg.go.dev](https://pkg.go.dev/go.alis.build/adk/launchers/agui)).

## Quick start

```go
agui.NewLauncher("my-agent",
    agui.WithCORS(agui.CORSConfig{
        AllowedOrigins: []string{"http://localhost:3000"},
    }),
)
```

```bash
adk web --port 8080 agui -path_prefix=/agui
```

## Architecture

| Layer | Responsibility |
| ----- | -------------- |
| HTTP handler (`runSSEFunc`) | Decode request, run `CallInterceptor.Before`/`After`, commit SSE headers, apply `OnEmit`, write events |
| `AgentExecutor` | Run pipeline: `RunStarted`, interrupt validation, snapshots, `adkrun.RunSSE`, ADK→AG-UI mapping, `RunFinished`, persist interrupts |

Extension points:

- **`WithExecutor`** — configure, decorate, or replace the protocol executor (see `doc.go`)
- **`WithInterceptor`** — transport hooks; `OnEmit` mutates/suppresses wire events
- **`WithGenAIPartConverter`** — custom ADK part → AG-UI event mapping (merged into default executor when no `WithExecutor`)

## Internal packages

| Package | Role |
| ------- | ---- |
| `internal/aguimsg` | Inbound AG-UI → genai messages |
| `internal/interrupt` | HITL resume and validation |
| `internal/stream` | Outbound ADK → AG-UI event mapping |

These are not public import paths; use the root `agui` package.

## Related

- [`clienttool/`](clienttool/) — client-side tool proxy toolset (agent opt-in)
- [`../README.md`](../README.md) — composing sublaunchers with `web.NewLauncher`

## Tests

```bash
ALIS_OS_PROJECT=test IDENTITY_SERVICE_URL=http://localhost go test ./...
```

Run from the repository root or this directory.
