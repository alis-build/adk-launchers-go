// Package agui implements an ADK web sublauncher for the AG-UI protocol. It bridges
// ADK agent execution to AG-UI Server-Sent Events (SSE), enabling CopilotKit and
// other AG-UI-compatible frontends to stream agent responses in real time.
//
// See [README.md] for a short overview and architecture table.
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
//   - WithExecutor — replace or decorate the protocol-level [AgentExecutor] (see below).
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
// # Agent executor (protocol layer)
//
// The /run_sse handler is split into transport (HTTP/SSE, [CallInterceptor]) and
// protocol ([AgentExecutor]). After SSE headers are committed, the handler ranges
// over [AgentExecutor.Execute], applies [CallInterceptor.OnEmit] to each yielded
// [events.Event], and writes to the wire.
//
// The default executor owns the run pipeline: RunStarted, pending-interrupt
// validation, state/message snapshots, ADK invocation via [adkrun.Runtime], ADK→AG-UI
// mapping ([internal/stream]), lifecycle finalization, RunFinished/RunError, and
// interrupt persistence. Configure it with [WithExecutor]:
//
//	agui.WithExecutor(func(d agui.ExecutorDeps) agui.AgentExecutor {
//	    return d.NewDefault(agui.ExecutorConfig{
//	        AfterEventCallback: myObserveHook, // observe + abort only
//	        GenAIPartConverter: myConverter,
//	    })
//	})
//
// [ExecutorContext] exposes request metadata and lazily loads the ADK session
// (one [session.Service.Get] per Execute, cached) for [ExecutorContext.ReadonlyState]
// and [ExecutorContext.Events]. [BeforeExecuteCallback], [AfterEventCallback], and
// [AfterExecuteCallback] run at protocol boundaries; event mutation/suppression
// stays in [CallInterceptor.OnEmit] at the HTTP boundary.
//
// [WithGenAIPartConverter] merges into the default [ExecutorConfig] when no custom
// [WithExecutor] factory is set. When [WithExecutor] is used, the factory owns all
// executor configuration (including the part converter).
//
// Three common [WithExecutor] patterns:
//
// Configure the default (callbacks or converter):
//
//	agui.WithExecutor(func(d agui.ExecutorDeps) agui.AgentExecutor {
//	    return d.NewDefault(agui.ExecutorConfig{
//	        AfterEventCallback: myHook,
//	        GenAIPartConverter: myConverter,
//	    })
//	})
//
// Decorate the default (wrap, do not replace):
//
//	agui.WithExecutor(func(d agui.ExecutorDeps) agui.AgentExecutor {
//	    inner := d.NewDefault(agui.ExecutorConfig{})
//	    return &metricsExecutor{inner: inner}
//	})
//
// Full replace (custom [AgentExecutor] implementation).
//
// # Internal packages
//
// Implementation details are split into unexported subpackages (not importable
// outside agui):
//
//   - internal/aguimsg — inbound AG-UI→genai message helpers (user text, multimodal,
//     tool-result trailing messages).
//   - internal/interrupt — HITL [interrupt.Record], resume→FunctionResponse mapping,
//     and resume validation against pending session state.
//   - internal/stream — outbound ADK→AG-UI event mapping, SSE/yield sinks, snapshot
//     emission, and [GenAIPartConverter] integration for live runs.
//
// Public extension points remain [WithExecutor], [CallInterceptor], and
// [WithGenAIPartConverter].
//
// # Call interceptors
//
// [CallInterceptor] runs around each /run_sse request:
//
//   - Before — validate or enrich the request; return an error to reject before SSE starts.
//   - OnEmit — observe or modify each AG-UI event before it is written to the wire.
//     Runs in the handler after [AgentExecutor.Execute] yields an event; the executor
//     and [AfterEventCallback] do not mutate yielded events.
//   - After — cleanup; runs in reverse order for interceptors whose Before succeeded.
//
// The handler populates [CallContext.User] from the mux IAM identity before
// interceptors run. Interceptors may override [CallContext.User] if needed.
// The handler requires a non-empty user name after interceptors complete. Embed
// [PassthroughInterceptor] to implement only the hooks you need.
//
// # Event mapping and part conversion
//
// During a run, ADK session events are translated into AG-UI protocol events by
// [internal/stream] and written to the SSE stream: text streaming, tool calls,
// reasoning, sub-agent steps, interrupts (human-in-the-loop confirmations), and run
// lifecycle (RunStarted, RunFinished, RunError). Partial streaming deltas are folded
// into final messages before emission.
//
// Internal helpers live in [internal/aguimsg] (inbound messages), [internal/interrupt]
// (HITL resume/validation), and [internal/stream] (outbound ADK→AG-UI mapping).
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
// sub-agent names included). [WithRootAppName] records the resolved app name for
// a future omit-root policy; v1 always includes author when present.
//
// Live SSE text streaming emits optional name on TEXT_MESSAGE_START from
// ev.Author via [events.WithName]. When author changes mid-stream, any open
// text is closed before step events so each partial sequence gets a fresh
// START with the new name.
//
// This function does not require the sublauncher to be running; use it from custom
// HTTP handlers or tooling that need AG-UI-shaped history without a live SSE run.
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
// DELETE {path_prefix}/threads/{threadId} removes thread metadata and its
// associated user states, then deletes the backing ADK session (Vertex Agent
// Engine conversation history). Both operations require appropriate IAM roles
// on the thread's policy. Optional query parameter "agentId" overrides app-name
// resolution for multi-agent hosts.
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
// ADK invocation id for client correlation. Same-invocation resume is handled by
// ADK v2 runner: FunctionResponse ids in the resume message must match the
// confirmation FunctionCall id stored on the session (see interrupt.EntriesToResumeContent).
//
// Resume validation runs after RunStarted (protocol errors become RunError on
// the SSE stream). The server enforces AG-UI contract rules when pending state
// exists: all open interrupts must be addressed, unknown ids rejected, optional
// expiry and responseSchema checks applied. See [interrupt.ValidateResumeAgainstPending]
// and [interrupt.EntriesToResumeContent].
//
// Mapping from AG-UI to ADK uses payload.approved → response.confirmed and
// optional payload.editedArgs → response.payload, per ADK toolconfirmation
// conventions and the AG-UI approve-with-edits pattern.
//
// # Non-tool interrupts (input_required and confirmation)
//
// ADK workflow nodes can pause for human input directly, without proposing a
// tool, by emitting a request through workflow.NewRequestInputEvent
// (FunctionCall name adk_request_input). The launcher maps those to AG-UI
// interrupts with reason "input_required" or "confirmation".
//
// The reason is chosen from the response schema the node advertised: a bare
// boolean, or an object wrapping exactly one boolean property, is a yes/no and
// becomes "confirmation"; anything else needs a real input form and becomes
// "input_required". [WithInterruptReasonClassifier] overrides that per request,
// and its result is used verbatim, so hosts may return their own namespaced
// reasons.
//
// These interrupts carry no toolCallId, since nothing is being proposed, and
// carry the node's response schema on responseSchema when it advertised one.
// metadata.adk.requestPayload holds any context the node attached, such as the
// document under review. Clients resume them the same way:
//
//	resume: [{ interruptId: "<interrupt-id>", status: "resolved", payload: { copies: 3 } }]
//
// The payload is whatever shape the advertised schema describes, including a
// bare scalar; unlike a tool confirmation it needs no "approved" field. A
// "cancelled" status resumes with a nil response and lets the node decide what a
// withheld answer means.
//
// Only workflow agents produce this reason. resumeInputs is populated solely by
// the workflow scheduler, and agent/workflowagent.detectResume is what routes
// the reply back, so a plain llmagent never emits adk_request_input no matter
// how it is prompted.
//
// # Protocol details: activity, reasoning and metadata
//
// Repeated activity updates for the same message id and activity type arrive as
// ACTIVITY_DELTA carrying a JSON Patch, instead of a full ACTIVITY_SNAPSHOT
// every time. The first update for a surface is always a snapshot, since a
// patch needs something to apply against, and an update whose content is not a
// JSON object falls back to a snapshot. A repeat that changes nothing emits
// nothing: the protocol rejects a delta with an empty patch. Patches use add,
// replace and remove only, and a resized array is replaced whole rather than
// diffed at shifting indices.
//
// Activity events reach the stream only through a [WithGenAIPartConverter]
// converter returning them; the launcher has no activity source of its own.
//
// Opaque reasoning blobs reach the client. ADK exposes them as
// genai.Part.ThoughtSignature; the launcher base64-encodes the bytes, emits
// REASONING_ENCRYPTED_VALUE inside the REASONING_START/REASONING_END bracket,
// and puts the same value on the reconstructed message in MESSAGES_SNAPSHOT,
// so a client can persist a thread and re-render it without dropping them.
//
// That traffic is outbound only. Reasoning continuity is the ADK session's
// doing: it holds the original parts server-side, so the model keeps its
// context whether or not the client sends anything back. A blob arriving on an
// inbound message is ignored — the next turn is built from the user's message,
// never from client-supplied assistant history.
//
// The signature is read from whichever part carries it, not only thought parts.
// ADK re-attaches it to the function call that ends a reasoning turn, so on a
// reasoning-then-tool turn the tool-call message is the carrier; text and
// reasoning messages take it only when there is no tool call. A blob arriving
// on a part that opened no reasoning message opens one, so it is never emitted
// outside the bracket, and that bracket never nests inside a text message: a
// signature carried by the answer text follows the text, after its message has
// closed. The value stays opaque: the launcher neither reads nor validates it.
//
// Every event carries metadata. metadata.adk holds invocationId and author, and
// nodePath on workflow events.
//
// Token usage is not metadata. It rides the protocol's own usage field on the
// terminal event — RUN_FINISHED, or RUN_ERROR for a run that died after some
// model calls completed — summed across the run rather than repeated per event.
//
// Counts come from ADK and are reported as one entry with no provider or model,
// because ADK surfaces neither at this layer; a run spanning several models
// arrives summed rather than split. A count ADK reported as zero is left absent
// rather than sent as zero: ADK types counts as int32 with omitempty, so it
// cannot tell "produced none" from "did not report", and asserting a measured
// zero would be the stronger claim. A negative count is dropped, since the SDK
// rejects one and would take the whole terminal event down with it. A report
// left with no count at all carries no usage field either, rather than an empty
// entry claiming usage was reported with nothing in it to read.
// nodePath follows [WithoutGraphAttribution] along with every other attribution
// site, so opting out keeps node topology off the wire entirely.
//
// Everything the launcher writes lives under "adk". The "ag-ui" key
// ([types.AGUIMetadataKey]) is reserved for AG-UI's own use and every other key
// is user space, so the launcher writes nothing there — including token usage,
// which looks protocol-shaped but has no shape the SDK defines.
//
// An event with nothing to report carries no metadata at all rather than an
// empty object, metadata a part converter set itself is never overwritten, and
// each event gets its own copy of the block so a consumer editing one cannot
// reach its siblings.
//
// # Subagents
//
// Each sub-agent activation is bracketed by SUBAGENT_STARTED and
// SUBAGENT_FINISHED, and every event it produces carries its subagentRunId, so
// a client can tell three concurrent researchers apart instead of rendering one
// wall of text.
//
// An activation is identified by the event author together with ADK's Branch.
// Branch is what keeps peer sub-agents from seeing each other's history, so two
// activations of the same agent on different branches are concurrent runs
// rather than one continuing. The root agent is the run itself, not a sub-agent
// within it, so its events carry no attribution.
//
// A sub-agent still open when the run pauses finishes as "suspended" rather than
// "success", naming the interrupts it owns, which is how a client shows which
// branch is waiting on a human.
//
// Every activation closes. The usual close is the handover to the next producer,
// but a run whose last producer is a sub-agent — the ordinary shape of an ADK
// transfer, where the sub-agent gives the final answer — has no such handover,
// so the activation closes at run finalization instead, before RUN_FINISHED or
// RUN_ERROR.
//
// Subagent brackets and STEP_* events mark the same boundary and are both
// emitted. They are not nested: AG-UI steps are a flat sequence, so the pair
// simply closes adjacently rather than one containing the other. The step closes
// first, while the activation that owned it is still open, so the sub-agent's
// own STEP_FINISHED carries its run id rather than its successor's.
//
// Attribution is on by default. [WithoutSubagentAttribution] turns off both the
// brackets and the run ids for clients that cannot handle them; the AG-UI Go
// SDK's decoder rejects an unrecognised event type rather than skipping it, so
// a consumer on an older SDK fails to decode rather than degrading. The
// sub-agent's own output and its step bracketing are unaffected.
//
// # Workflow graphs
//
// ADK's workflow engine tags every event with graph provenance, and the
// launcher surfaces it so a client can tell which node of a multi-node workflow
// is running and what each node produced. It reads four ADK fields:
// NodeInfo.Path, NodeInfo.MessageAsOutput, NodeInfo.OutputFor and Event.Routes.
//
// Whether an event belongs to a workflow is decided by the NodeInfo pointer
// alone, per ADK's invariant. An event with a nil NodeInfo produces exactly the
// stream it did before graph attribution existed.
//
// Node activations are bracketed by STEP_STARTED and STEP_FINISHED named by
// NodeInfo.Path, falling back to the event author for top-level static nodes,
// which carry no path. AG-UI steps do not nest, so dynamic paths such as
// "parent/child@run-id" are flat, distinct steps and the path string carries the
// hierarchy for clients that want to parse it. A step closes when a different
// node appears or at run finalization, so an interrupt never leaves one open.
//
// Unlike the root agent, which gets no step, a workflow node is always
// bracketed, including when its agent shares the root's name: it is a real graph
// activation and hiding it would drop a node from the client's view.
//
// Node results are published on the state channel under a reserved key:
//
//	_adk.nodeOutputs.<node path> = <output>
//
// Event.Output is used when present; otherwise MessageAsOutput means the node's
// model text is its result. OutputFor records one output under every listed
// path, so a single event stands in for a whole delegation chain rather than
// each level re-emitting it. Keys use the same identity as step names, so a
// node appears under one name in both places.
//
// The _adk prefix is reserved and treated as internal state. Inbound _adk, from
// session state or a client request, is stripped rather than echoed; only the
// launcher writes it. Without that reservation node outputs would be read back
// as host state and written into the ADK session on the next turn.
//
// Attribution is on by default. [WithoutGraphAttribution] turns off all three
// surfaces together (step events, node outputs and interrupt metadata) for
// clients that do not expect them; the agent's own output is unaffected.
//
// # Multiple interrupts per event
//
// An event may carry several interrupt-producing calls. All of them are
// collected in part order and delivered in a single RunFinished, because the
// protocol allows exactly one terminal event per run. Clients must therefore
// answer every interrupt in outcome.interrupts, not just the first: resume
// validation rejects a resume that leaves any pending interrupt unaddressed.
//
// At run start and before interrupt RunFinished, the launcher emits StateSnapshot
// (and MessagesSnapshot at interrupt boundaries) so clients have baseline context.
// A resume run emits no baseline StateSnapshot: the interrupt snapshot that paused
// the run already published the full picture, including the _adk node outputs,
// which live on the per-run state and cannot be rebuilt on the resuming run.
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
// AG-UI interrupt emit and resume cover ADK tool confirmations (reason
// "tool_call") and workflow input requests (reasons "input_required" and
// "confirmation"). The latter come only from workflow agents; a plain llmagent
// cannot produce them. The launcher never emits adk_request_input itself, so a
// host wanting non-tool interrupts has to emit them from a workflow node.
// Resume without matching pending session state is rejected. Resume idempotency
// (replay of the same resume tuple) is not deduplicated server-side. Payload
// validation uses a minimal JSON Schema subset, not a full validator. Pending
// interrupt persist/clear failures after the terminal event are logged
// server-side (not re-emitted as RunError, which would violate the
// single-terminal-event protocol rule). Use [WithCapabilities] or
// [DefaultInterruptCapabilities] to advertise humanInTheLoop.interrupts,
// approveWithEdits and interruptReasons, plus output.activityDeltas and
// output.encryptedReasoning. Workflow graph attribution reads ADK NodeInfo,
// Routes and Output; a live per-node graph view
// (ACTIVITY_SNAPSHOT/ACTIVITY_DELTA) is not implemented. Client-side tools
// require agent opt-in via [clienttool.NewToolset]; see the Client-side tools
// section.
package agui
