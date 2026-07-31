// Package console implements an ADK web sublauncher that serves the Vue operator
// console (embedded SPA), deploy-time runtime configuration, and /auth/me.
//
// # Registration order
//
// Pass console last when composing launchers so the SPA catch-all on GET /
// does not shadow agui, scheduler, or other host routes:
//
//	web.NewLauncher(
//	    agui.NewLauncher("my-agent", agui.WithThreadService(historySvc)),
//	    scheduler.NewLauncher("my-agent", schedSvc),
//	    console.NewLauncher(console.WithBranding(console.Branding{
//	        Title:       "My Console",
//	        DisplayName: "My Console",
//	        Favicon:     console.URLAsset("/my-agent/branding/favicon.ico"),
//	        Logo:        console.EmbedAsset(brandingFS, "branding", "logo.svg"),
//	    })),
//	)
//
// # Branding
//
// WithBranding accepts Title and DisplayName as plain strings. Logo and Favicon
// are optional AssetResolver values resolved at setup into logoUrl and faviconUrl
// in runtime-config.json. Nil resolvers omit those URLs; the SPA uses bundled
// defaults (/logo.svg and index.html favicon).
//
// Out-of-the-box resolvers:
//   - URLAsset(href) — existing HTTP path or external URL (agent routes, CDN, dist)
//   - EmbedAsset(fs, root, relativePath) — go:embed files at /console/branding/...
//   - DirAsset(dir, relativePath) — host directory files at /console/branding/...
//
// Register agent branding routes before console so paths like /my-agent/... are
// not shadowed by the SPA catch-all.
//
// # Runtime configuration
//
// GET /assets/config/runtime-config.json returns agents and defaultAgentId from
// launcher.Config.AgentLoader.ListAgents() and RootAgent().Name(), plus resolved
// shell branding from WithBranding when set.
//
// # Local development
//
// By default the launcher serves the embedded app/dist build (rebuild with
// cd console/app && pnpm build and recompile Go). For HMR, run Vite and set
// SPA_DEV_SERVER_URL so GET / proxies to the dev server with mux auth:
//
//	cd console/app && pnpm dev
//	SPA_DEV_SERVER_URL=http://localhost:8000 adk web --port 8080 agui scheduler console
//
// Override explicitly with WithIsLocal(func() bool { return true }) or
// WithIsLocal(func() bool { return false }) when composing NewLauncher.
//
// For frontend-only work on the Vite port, see console/app/vite.config.ts — it
// forwards /agui and JSON-RPC paths to AGENT_HOST (default http://localhost:8080).
//
// # Custom SPA dist
//
// By default the launcher serves the embedded app/dist build. To serve a custom
// production dist (for example output from a Docker frontend build), use WithDist
// or set SPA_DIST_DIR:
//
//	console.NewLauncher(
//	    console.WithDist(console.DirDist("dist")),
//	)
//
// Built-in DistResolver values:
//   - DefaultDist() — embedded console/app/dist (default when WithDist is unset)
//   - DirDist(dir) — host directory (SPA_DIST_DIR uses this)
//   - EmbedDist(fs, root) — go:embed dist in a consumer module
//   - HandlerDist(h) — existing handler (for example http.FileServer(http.Dir("dist")))
//
// Resolution order in production: WithDist, then SPA_DIST_DIR, then DefaultDist.
// Dev mode (SPA_DEV_SERVER_URL / WithDevServerURL) always takes precedence.
//
// # Backend API for custom SPAs
//
// A custom dist must call the same-origin backend APIs that the bundled Vue app
// uses. Routes are registered by sublaunchers on the host mux (go.alis.build/mux);
// register console last so its GET / catch-all does not shadow API paths below.
//
// Authorization: endpoints marked (auth) require a caller identity in the request
// context (resolved from the upstream x-alis-identity header by the web launcher's
// authorization gateway) and fail closed with 401 when none is present. The
// launcher authorizes but does not authenticate. /auth/logout is served by the mux
// identity layer (browser redirect), not by package console.
//
// ## Console launcher
//
//	GET  /assets/config/runtime-config.json  (auth)  Deploy-time shell config
//	GET  /auth/me                            (auth)  Current user OIDC claims
//	GET  /                                   (auth)  SPA static assets (catch-all)
//	GET  /console/branding/{file}            (auth)  Optional branding assets (EmbedAsset/DirAsset)
//
// GET /assets/config/runtime-config.json response:
//
//	{
//	  "gcpProject": "my-project",
//	  "agents": ["root", "other"],
//	  "defaultAgentId": "root",
//	  "branding": {
//	    "title": "My Console",
//	    "displayName": "My Console",
//	    "logoUrl": "/console/branding/logo.svg",
//	    "faviconUrl": "/console/branding/favicon.ico"
//	  }
//	}
//
// GET /auth/me response: {"sub": "<identity-id>", "email": "<email>"}.
//
// ## AG-UI launcher (keyword "agui", default path prefix /agui)
//
// Requires the agui sublauncher. Override the prefix with agui -path_prefix=/api/agui.
//
//	POST   /agui/run_sse                           (auth)  Agent run SSE stream
//	GET    /agui/threads/{threadId}/messages       (auth)  Message history (JSON or SSE)
//	GET    /agui/threads/{threadId}                (auth)  Thread metadata (WithThreadService)
//	DELETE /agui/threads/{threadId}                (auth)  Delete thread (WithThreadService)
//	GET    /agui/threads                           (auth)  List threads (WithThreadService)
//
// POST /agui/run_sse
//
//   - Content-Type: application/json
//   - Body: AG-UI RunAgentInput (see https://docs.ag-ui.com). The bundled SPA sends
//     threadId, messages, optional tools, resume (HITL), and context
//     [{description:"app", value:"<agentId>"}] for multi-agent routing.
//   - Response: text/event-stream (AG-UI SSE events: RunStarted, TextMessage*, ToolCall*,
//     RunFinished, RunError, etc.)
//
// GET /agui/threads/{threadId}/messages
//
//   - Query: agentId (optional), after (RFC 3339 cursor), limit (non-negative int)
//   - Accept: application/json (default) or text/event-stream
//   - JSON response: {"messages":[...], "nextCursor":"..."} (AG-UI Message objects)
//
// GET /agui/threads
//
//   - Query: agentId (optional), pageSize, pageToken
//   - Response: thread list with per-user metadata (readRunCount, hasUnread, pinned)
//
// GET /agui/threads/{threadId} returns a single thread proto JSON object.
// DELETE /agui/threads/{threadId} returns 204 No Content on success.
//
// ## History JSON-RPC (agui WithThreadService)
//
//	POST /alis.agui.history.v1.ThreadService  (auth)  JSON-RPC 2.0
//
// Request envelope:
//
//	{"jsonrpc":"2.0","method":"<Method>","params":{...},"id":1}
//
// Methods used by the bundled SPA:
//
//   - UpdateUserThreadState — params:
//     {"userThreadState":{"thread":"<threadId>","readRunCount":N,"pinned":true},
//     "updateMask":"pinned,read_run_count"}
//     Returns updated UserThreadState. updateMask uses comma-separated snake_case paths.
//
// Other ThreadService RPCs may be available on the same path; see go.alis.build/agui/history.
//
// ## Scheduler JSON-RPC (scheduler sublauncher)
//
//	POST /alis.agui.scheduler.v1.SchedulerService  (auth)  JSON-RPC 2.0
//
// Methods used by the bundled SPA (/automation page):
//
//   - ListCrons — params: {"pageSize":N, "pageToken":"..."}
//   - CreateCron — params: {"cron":{...}} (prompt, expr, timezone, type, at, etc.)
//   - DeleteCron — params: {"name":"crons/<id>"}
//   - RunCron — params: {"id":"<cron-id>"}
//
// Params use protojson-compatible camelCase. See go.alis.build/agui/scheduler.
//
// ## Typical launcher composition
//
//	adk web --port 8080 agui scheduler console
//
// For local Vite dev, proxy these paths to the agent host (see console/app/vite.config.ts):
// /agui, /auth, /alis.agui.history.v1.ThreadService,
// /alis.agui.scheduler.v1.SchedulerService, /assets/config/runtime-config.json.
//
// # Embedded SPA build
//
// app/dist is checked in for go:embed (all: prefix so Vite "_" chunks are included).
// Regenerate with go generate ./console/...
// or cd console/app && pnpm build. The Husky pre-commit hook rebuilds dist when
// console/app changes are committed.
package console
