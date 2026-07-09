// Package agui implements an ADK web sublauncher for the AG-UI protocol. It bridges
// ADK agent execution to AG-UI Server-Sent Events (SSE), enabling CopilotKit and
// other AG-UI-compatible frontends to stream agent responses in real time.
//
// # Role in the ADK web launcher
//
// The ADK web launcher composes one or more sublaunchers, each activated by a CLI
// keyword. This package registers the keyword "agui" and mounts AG-UI HTTP routes on
// the process-wide host mux (go.alis.build/mux) via [HostRouteSetup]. The /run_sse
// and /threads endpoints require a caller identity (resolved from the upstream
// x-alis-identity header by the web launcher's authorization gateway) and fail
// closed when none is present; /capabilities is public.
//
// # Agent binding and multi-agent routing
//
// NewLauncher takes a default app name used when clients do not specify an agent.
// Each request resolves the ADK app name via the same chain (used by /run_sse,
// GET /threads/{id}/messages, and POST /agents/state):
//
//   - Optional [WithAppNameResolver] (full RunAgentInput for /run_sse; partial for
//     GET/POST handlers — agentId query/body is injected as context "app" for the resolver)
//   - RunAgentInput.state app_name / appName
//   - RunAgentInput.context entry with description "app" (recommended for browser clients)
//   - AgentLoader.RootAgent().Name()
//   - The NewLauncher default app name
//
// Names from resolver, state, or context are validated against
// AgentLoader.ListAgents when a multi-agent loader is configured.
//
// Browser clients should pass the selected agent on every run:
//
//	runAgent({ context: [{ description: "app", value: agentId }] })
//
// GET thread message history and POST /agents/state accept an optional agentId
// query or body field for the same resolution. Thread metadata display names
// use each agent's ADK Description() when set (see agentDisplayName).
// When [WithThreadService] is set, history JSON-RPC is mounted
// at POST /alis.agui.history.v1.ThreadService for browser clients, and native
// gRPC is available when [WithGRPCRegistrar] registers the same service on the
// host grpc.Server (for gRPC-Web clients and BFF proxies).
//
// At setup time, [SetupHostRoutes] creates a single [adkrun.Runtime]; per-request
// [adkrun.RunRequest.AppName] selects the agent via AgentLoader.LoadAgent.
//
// Conversation continuity uses a 1:1 mapping between AG-UI threadId and the ADK
// session ID. [adkrun.Runtime] enables AutoCreateSession so the first request for a
// thread creates the session automatically.
//
// # HTTP routes
//
// Routes are mounted under a configurable path prefix (default "/agui"):
//
//	{path_prefix}/run_sse                        POST  — SSE streaming endpoint for agent runs (authenticated)
//	{path_prefix}/capabilities                   GET   — capability discovery (public, only if configured)
//	{path_prefix}/threads/{threadId}/messages     GET   — thread message history (authenticated)
//	{path_prefix}/threads/{threadId}             GET    — single thread metadata (authenticated, WithThreadService)
//	{path_prefix}/threads/{threadId}             DELETE — delete a thread (authenticated, WithThreadService)
//	{path_prefix}/threads                        GET    — thread listing with metadata (authenticated, WithThreadService)
//	{path_prefix}/agents/state                   POST   — on-demand state and message history (authenticated, WithAgentStateEndpoint)
//
// When CORS is enabled via WithCORS, OPTIONS preflight is handled for the registered
// routes in addition to POST and GET.
//
// The /run_sse handler accepts a JSON [types.RunAgentInput] body. It extracts the
// latest user message from the request (full history may be sent, but ADK session
// service maintains authoritative history via threadId). Optional request state is
// forwarded into the ADK session via [adkrun.RunRequest.StateDelta].
//
// Errors before SSE headers are committed return standard HTTP status codes. After
// the stream starts (RunStartedEvent emitted), errors are delivered as RunErrorEvent
// on the SSE connection.
//
// # Configuration
//
// Options apply when calling NewLauncher:
//
//   - WithInterceptor — add [CallInterceptor] hooks (auth, logging, event mutation).
//   - WithCORS — enable CORS middleware for browser-based frontends.
//   - WithCapabilities — expose GET /capabilities for client discovery (see below).
//   - WithGenAIPartConverter — customize how [genai.Part] values map to AG-UI events.
//   - WithThreadService — enable thread metadata tracking, GET /threads listing, and history JSON-RPC.
//   - WithGRPCRegistrar — register ThreadService on the host grpc.Server during setup (requires WithThreadService).
//   - WithAppNameResolver — custom app name extraction from RunAgentInput.
//   - WithHistoryJSONRPCOptions — CORS and other options for the history JSON-RPC handler.
//   - WithMessagesSnapshotOnRunEnd — emit MESSAGES_SNAPSHOT before RunFinished on every successful run.
//   - WithPredictState — emit PredictState custom events for CopilotKit real-time state preview.
//   - WithAgentStateEndpoint — register POST /agents/state for on-demand state retrieval.
//
// CLI flags (after the "agui" keyword on the web command line):
//
//   - -path_prefix — URL prefix for AG-UI routes (default "/agui").
//
// The app name is set only via NewLauncher's first argument; there is no CLI flag
// for it. Path prefix can be overridden at runtime via -path_prefix even when
// defaults were set at construction.
//
// # Usage
//
// Programmatic defaults:
//
//	streaming := true
//	web.NewLauncher(
//	    agui.NewLauncher(
//	        "my-agent",
//	        agui.WithCORS(agui.CORSConfig{
//	            AllowedOrigins: []string{"http://localhost:3000"},
//	        }),
//	        agui.WithCapabilities(agui.Capabilities{
//	            Transport: &agui.TransportCapabilities{Streaming: &streaming},
//	        }),
//	    ),
//	)
//
// CLI example:
//
//	adk web --port 8080 agui -path_prefix=/api/agui
//
// On startup, UserMessage prints the registered endpoint URLs (for example
// http://localhost:8080/agui/run_sse and the thread messages path).
//
// # Call interceptors
//
// [CallInterceptor] runs around each /run_sse request:
//
//   - Before — validate or enrich the request; return an error to reject before SSE starts.
//   - OnEmit — observe or modify each AG-UI event before it is written to the wire.
//   - After — cleanup; runs in reverse order for interceptors whose Before succeeded.
//
// The handler populates [CallContext.User] from the mux IAM identity before
// interceptors run. Interceptors may override [CallContext.User] if needed.
// The handler requires a non-empty user name after interceptors complete. Embed
// [PassthroughInterceptor] to implement only the hooks you need.
//
// # Event mapping and part conversion
//
// During a run, ADK session events are translated into AG-UI protocol events on the
// SSE stream: text streaming, tool calls, reasoning, sub-agent steps, interrupts
// (human-in-the-loop confirmations), and run lifecycle (RunStarted, RunFinished,
// RunError). Partial streaming deltas are folded into final messages before emission.
//
// [GenAIPartConverter] mirrors the adka2a pattern: return a non-nil slice (including
// empty) to handle a part and skip default mapping; return (nil, nil) to use the
// default handler. The same converter can be passed to [ConvertSessionToMessages] via
// [WithPartConverter] for consistent history replay.
//
// # Session history conversion
//
// [ConvertSessionToMessages] converts stored ADK session events into AG-UI
// [types.Message] values for MESSAGES_SNAPSHOT payloads or direct JSON responses.
// It skips partial (in-flight) events and supports cursor pagination via
// [WithConvertAfter] and [WithConvertLimit]. Assistant messages and tool-call
// batches set [types.Message].Name from the ADK event Author field (root and
// sub-agent names included). [WithRootAppName] records the resolved app name
// for a future omit-root policy; v1 always includes author when present.
//
// Live SSE text streaming emits optional name on TEXT_MESSAGE_START from
// ev.Author via a temporary emit-only shim (agui.TextMessageStartEvent) until
// the AG-UI Go SDK adds Name on events.TextMessageStartEvent. Follow-up:
// upstream PR to ag-ui/sdks/community/go/pkg/core/events/message_events.go —
// not opened by this launcher. When author changes mid-stream, open text is
// closed before step events so each partial sequence gets a fresh START with
// the new name.
//
// This function does not require the sublauncher to be running; use it from
// custom HTTP handlers or tooling that need AG-UI-shaped history without a
// live SSE run.
//
// # Thread message history
//
// GET {path_prefix}/threads/{threadId}/messages loads the ADK session for the
// authenticated user and thread ID, converts stored events to AG-UI messages via
// [ConvertSessionToMessages], and returns them as JSON or SSE depending on the
// Accept header.
//
// JSON response (default): {"messages": [...], "nextCursor": "..."}.
// SSE response (Accept: text/event-stream): RunStarted → MessagesSnapshot →
// StateSnapshot (if non-empty) → RunFinished.
//
// Query parameters "after" (RFC 3339 cursor) and "limit" support pagination.
// The path matches CopilotKit's fetch-router expectation for
// /threads/{id}/messages.
//
// # Single thread
//
// When [WithThreadService] is configured, GET {path_prefix}/threads/{threadId}
// returns the metadata for a single thread (display name, run count, agent ID,
// timestamps) from the [go.alis.build/agui/history/service.ThreadService].
// DELETE {path_prefix}/threads/{threadId} removes the thread and its associated
// user states. Both operations require appropriate IAM roles on the thread's policy.
//
// # Thread listing
//
// When [WithThreadService] is configured, GET {path_prefix}/threads returns a
// list of threads with rich metadata (display names, unread tracking, pinned
// state) from the [go.alis.build/agui/history/service.ThreadService]. Each
// /run_sse request automatically creates or updates thread metadata (run count,
// last activity time, display name on first run). Query parameters: "agentId"
// (optional filter), "pageSize", "pageToken".
//
// # Capabilities
//
// When [WithCapabilities] is set, GET /capabilities returns the declared
// [Capabilities] document as JSON. Only fields the agent actually supports should be
// populated; omitted fields mean the capability is undeclared ("absent = unknown").
// Clients use this endpoint to adapt UI features (tools, multimodal input, streaming,
// human-in-the-loop, and so on).
//
// [WithCapabilities] calls [MergeInterruptCapabilities] so that agents using this
// launcher advertise AG-UI interrupt resume by default (humanInTheLoop.interrupts
// and humanInTheLoop.approveWithEdits). Set those fields explicitly to false in
// your [Capabilities] value if you need to opt out. Alternatively, use
// [DefaultInterruptCapabilities] as a starting point for a minimal HITL-only document.
//
// # Interrupts and resume (human-in-the-loop)
//
// When ADK pauses for tool confirmation (FunctionCall name
// adk_request_confirmation), the launcher emits a [types.Interrupt] inside
// RunFinished.outcome and records pending interrupts in ADK session state under
// [pendingInterruptsStateKey]. The interrupt id is the confirmation call id
// (fc.ID), so clients resume with:
//
//	resume: [{ interruptId: "<confirmation-call-id>", status: "resolved", payload: { approved: true } }]
//
// Interrupt metadata.adk.invocationId and resume state.adk.invocationId carry the
// ADK invocation id for future same-invocation resume; RunRequest.InvocationID
// is set on resume runs but not yet passed to runner.Run (see adkrun TODO).
//
// Resume validation runs after RunStarted (protocol errors become RunError on
// the SSE stream). The server enforces AG-UI contract rules when pending state
// exists: all open interrupts must be addressed, unknown ids rejected, optional
// expiry and responseSchema checks applied. See [validateResumeAgainstPending]
// and [resumeEntriesToConfirmationContent].
//
// Mapping from AG-UI to ADK uses payload.approved → response.confirmed and
// optional payload.editedArgs → response.payload, per ADK toolconfirmation
// conventions and the AG-UI approve-with-edits pattern.
//
// Only interrupts with reason "tool_call" are emitted (from ADK tool confirmations).
// AG-UI core reasons "input_required" and "confirmation" are deferred until ADK
// exposes a native pause primitive; see the TODO in stream.go. Non-tool resume
// paths are not implemented (see resume.go).
//
// At run start and before interrupt RunFinished, the launcher emits StateSnapshot
// (and MessagesSnapshot at interrupt boundaries) so clients have baseline context.
// Successful runs emit RunFinished with outcome.type "success".
//
// # CORS
//
// Browser frontends (CopilotKit, Vue/React SPAs) typically call the agent server from
// a different origin. WithCORS wraps handlers with Access-Control-* headers and
// handles OPTIONS preflight. When AllowCredentials is true, the middleware echoes the
// request Origin instead of using "*", per the CORS specification.
//
// # Client-side tools
//
// AG-UI clients like CopilotKit can define tools on the frontend (e.g. via
// useCopilotAction) and send their definitions in [types.RunAgentInput.Tools].
// The launcher supports these through the [clienttool] sub-package.
//
// To enable client-side tools, the agent must include a [clienttool.Toolset] in
// its toolset list:
//
//	agent, _ := llmagent.New(llmagent.Config{
//	    Name: "my_agent",
//	    Toolsets: []tool.Toolset{
//	        clienttool.NewToolset(),  // enables frontend-defined tools
//	    },
//	    Tools: []tool.Tool{
//	        // ... server-side tools as usual
//	    },
//	})
//
// The data flow for a client-side tool call:
//
//  1. Client sends RunAgentInput with tool definitions in Tools[].
//  2. Launcher injects definitions into session state via StateDelta.
//  3. ADK calls [clienttool.Toolset.Tools], which reads state and creates proxy tools.
//  4. LLM sees the tools in its schema and may call one.
//  5. Proxy tool returns {"status": "pending"} — ADK emits the function call event.
//  6. Launcher maps the event to TOOL_CALL_START/ARGS/END on the SSE stream.
//  7. Run finishes. Client executes the tool locally.
//  8. Client sends a new RunAgentInput with the result as a tool-role message
//     (role "tool", toolCallId, and the result content).
//  9. Launcher detects trailing tool messages, converts them to FunctionResponse
//     parts, and starts a new ADK run with the responses.
//  10. ADK processes the FunctionResponse and continues the conversation.
//
// Tool definitions with empty names are silently skipped. Duplicate names are
// deduplicated (first wins). The "pending" FunctionResponse from proxy tools is
// filtered from the SSE stream — clients never see it as a ToolCallResult.
//
// When using [WithCapabilities], tools.clientProvided is automatically set to
// true via [MergeClientToolCapabilities].
//
// # Predictive state
//
// [WithPredictState] enables real-time state preview for CopilotKit's
// useCoAgentStateRender. When a tool call matches a configured
// [PredictStateMapping], a "PredictState" [CustomEvent] is emitted on the SSE
// stream before the tool call events. This tells the UI to optimistically update
// a state key from the tool's streaming arguments.
//
//	agui.NewLauncher("my-agent",
//	    agui.WithPredictState(agui.PredictStateMapping{
//	        StateKey:     "document",
//	        Tool:         "write_document",
//	        ToolArgument: "content",
//	    }),
//	)
//
// PredictState is emitted once per tool name per run. A second call to the same
// tool in one run does not re-emit the event. This matches the Python ADK
// middleware behaviour.
//
// # Agent state endpoint
//
// [WithAgentStateEndpoint] registers POST {path_prefix}/agents/state, which
// returns thread state and message history without starting a new agent run.
// Used by CopilotKit's useCoAgentState for on-demand state retrieval.
//
// Request body: {"threadId": "..."}. Identity is read from the request context
// (same as /run_sse).
//
// Response:
//
//	{
//	    "threadId": "...",
//	    "threadExists": true,
//	    "state": { ... },
//	    "messages": [ ... ]
//	}
//
// When the thread does not exist, threadExists is false and state/messages are
// empty. Load failures return HTTP 500.
//
// # Messages snapshot at run end
//
// [WithMessagesSnapshotOnRunEnd] emits a MESSAGES_SNAPSHOT event before
// RunFinished on every successful (non-interrupt) run. Without this option,
// message snapshots are only emitted at interrupt boundaries (always). Enable
// this for AG-UI clients that rely on complete message history without
// maintaining their own from TEXT_MESSAGE_* streaming events.
//
// # Protocol dependencies
//
// Streaming and event types come from the AG-UI community Go SDK
// (github.com/ag-ui-protocol/ag-ui/sdks/community/go). See https://docs.ag-ui.com
// for the protocol specification.
//
// # Limitations
//
// AG-UI interrupt resume is supported for ADK tool
// confirmations (adk_request_confirmation) with reason "tool_call" only.
// Core AG-UI reasons "input_required" and "confirmation" are not implemented;
// support depends on a future ADK pause/HITL API (see TODO in stream.go).
// Resume without matching pending session state is rejected. Resume idempotency
// (replay of the same resume tuple) is not deduplicated server-side. Payload
// validation uses a minimal JSON Schema subset, not a full validator. Pending
// interrupt persist/clear failures after the terminal event are logged
// server-side (not re-emitted as RunError, which would violate the
// single-terminal-event protocol rule). Use
// [WithCapabilities] or [DefaultInterruptCapabilities] to advertise
// humanInTheLoop.interrupts and approveWithEdits. Client-side tools require
// agent opt-in via [clienttool.NewToolset]; see the Client-side tools section.
package agui
