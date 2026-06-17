package agui

import (
	"context"
	"fmt"
	"iter"
	"log"
	"net/http"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/clienttool"
	"go.alis.build/adk/launchers/agui/internal/aguimsg"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// AgentExecutor defines the protocol-level contract for running one AG-UI agent turn.
// Each call to Execute drives a full pipeline: RunStarted → ADK invocation → event
// mapping → RunFinished (or RunError on failure) → interrupt persistence. The caller
// (runSSEFunc) consumes the yielded events, applies CallInterceptor.OnEmit for
// wire-level mutation, and writes SSE frames to the client.
//
// The default implementation is built by [ExecutorDeps.NewDefault] and handles all
// of the above automatically. Callers that need custom lifecycle hooks (metrics,
// auth enrichment, event observation) should use [WithExecutor] to configure,
// decorate, or fully replace the default. See the package doc for the three
// WithExecutor patterns (configure / decorate / replace).
type AgentExecutor interface {
	Execute(ctx context.Context, execCtx ExecutorContext) iter.Seq2[events.Event, error]
}

// ExecutorContext exposes request metadata and session views for one /run_sse turn.
//
// Callbacks (BeforeExecuteCallback, AfterEventCallback, AfterExecuteCallback) receive
// this interface so they can inspect the in-flight run without reaching into HTTP
// handler internals or the executor's private state. This is important because it
// keeps the callback contract stable even when the executor internals change.
//
// Session access ([ExecutorContext.ReadonlyState] and [ExecutorContext.Events]) is
// lazy: the underlying session.Service.Get is called at most once per Execute, then
// cached. This matters because many runs never need session data in callbacks, and
// each Get is a network or storage round-trip. If no callback calls ReadonlyState or
// Events, no session load happens at all.
type ExecutorContext interface {
	context.Context

	// Request is the decoded AG-UI JSON body (messages, resume entries, tools, state).
	Request() *types.RunAgentInput
	// HTTPRequest is the incoming HTTP request; use it for headers or auth-derived values.
	HTTPRequest() *http.Request
	// UserID is the authenticated ADK user id for this turn.
	UserID() string
	// ThreadID is the AG-UI thread id and doubles as the ADK session id for continuity.
	ThreadID() string
	// RunID is the AG-UI run id emitted to clients in RunStarted/RunFinished events.
	RunID() string
	// AppName is the resolved ADK agent name (multi-agent routing happens before Execute).
	AppName() string
	// UserContent is the genai message the default executor will send to adkrun, once resolved.
	UserContent() *genai.Content
	// SetUserContent stores the resolved message so callbacks can inspect what ADK will receive.
	SetUserContent(*genai.Content)
	// IsResume is true when the client sent resume entries for a prior HITL interrupt.
	IsResume() bool
	// ReadonlyState returns committed session state at first access time (not streaming partials).
	ReadonlyState() session.ReadonlyState
	// Events returns committed session history at first access time (not in-flight ADK events).
	Events() session.Events
}

// BeforeExecuteCallback runs after the inbound message is resolved but before adkrun.RunSSE
// starts the ADK agent. This is the last chance to enrich the context (e.g. inject tracing
// spans, auth tokens, or per-request metadata) or to reject the run entirely by returning an
// error. A non-nil error aborts the run with a RunError event on the SSE stream. The returned
// context replaces the run context for all downstream ADK calls, so callers must return a
// non-nil context on success.
type BeforeExecuteCallback func(ctx context.Context, execCtx ExecutorContext) (context.Context, error)

// AfterEventCallback runs after each ADK session event is mapped to zero or more AG-UI events.
// The emitted slice contains the AG-UI events that were just sent to the sink (and will be
// written to the SSE stream). This callback is strictly observe-and-abort: it must not mutate
// the emitted events. If you need to change events before they hit the wire, use
// CallInterceptor.OnEmit instead (which runs in the HTTP handler after Execute yields).
// Return a non-nil error to abort the run mid-stream with RunError.
type AfterEventCallback func(execCtx ExecutorContext, adkEvent *session.Event, emitted []events.Event) error

// AfterExecuteCallback runs exactly once after the run completes, whether it finished
// successfully, aborted with an error, or was interrupted for HITL. The runErr parameter
// is nil on success and non-nil when the run ended in RunError. Callbacks may return their
// own error to record a post-run failure (e.g. failed metric flush); this error is logged
// but does not re-emit a RunError (the AG-UI protocol allows only one terminal event per run).
type AfterExecuteCallback func(execCtx ExecutorContext, err error) error

// ExecutorConfig configures the default executor's callbacks and converters.
//
// Callbacks run at protocol boundaries inside [AgentExecutor.Execute] and are
// intentionally restricted: they must not mutate AG-UI events directly. This
// separation exists because event mutation belongs at the HTTP boundary
// ([CallInterceptor.OnEmit]), not inside the protocol pipeline. Mixing the two
// would make the event flow unpredictable for downstream consumers.
type ExecutorConfig struct {
	// GenAIPartConverter replaces the launcher default for ADK part → AG-UI mapping in this executor.
	GenAIPartConverter GenAIPartConverter
	// BeforeExecuteCallback can enrich context or reject the run before ADK starts.
	BeforeExecuteCallback BeforeExecuteCallback
	// AfterEventCallback can observe mapped events or abort mid-run; must not mutate emitted.
	AfterEventCallback AfterEventCallback
	// AfterExecuteCallback runs after RunFinished/RunError and before interrupt persistence.
	AfterExecuteCallback AfterExecuteCallback
}

// ExecutorDeps holds the launcher dependencies that are available at route-mount time
// (after SetupHostRoutes creates the adkrun.Runtime). An ExecutorFactory receives these
// so it can build an AgentExecutor with access to the runtime and session helpers.
// This struct is the only way a factory gets launcher internals — it prevents factories
// from depending on unexported aguiLauncher fields directly.
type ExecutorDeps struct {
	// Launcher owns session helpers, snapshot builders, and interrupt persistence.
	Launcher *aguiLauncher
	// Runtime executes the ADK agent in-process for each turn.
	Runtime *adkrun.Runtime
	// Config is the launcher-level options (predict state, end-of-run snapshots, etc.).
	Config *AGUIConfig
}

// ExecutorFactory is called once during SetupHostRoutes to build the [AgentExecutor] that
// handles every /run_sse request for the lifetime of the process. The factory receives
// [ExecutorDeps] so it can call [ExecutorDeps.NewDefault] (configure), wrap the result
// (decorate), or return a completely custom implementation (replace). See [WithExecutor]
// and the package documentation for examples of each pattern.
type ExecutorFactory func(deps ExecutorDeps) AgentExecutor

// NewDefault returns the stock [AgentExecutor] with the given [ExecutorConfig].
func (d ExecutorDeps) NewDefault(cfg ExecutorConfig) AgentExecutor {
	return &defaultExecutor{
		launcher: d.Launcher,
		runtime:  d.Runtime,
		cfg:      cfg,
	}
}

type defaultExecutor struct {
	launcher *aguiLauncher
	runtime  *adkrun.Runtime
	cfg      ExecutorConfig
}

// executeContext is the concrete [ExecutorContext] implementation. One instance is created
// per /run_sse request and threaded through Execute and all callbacks. It embeds
// context.Context so it can be used directly where a context is needed, and caches
// the ADK session on first access to avoid repeated round-trips to the session service.
type executeContext struct {
	context.Context
	req         *types.RunAgentInput
	httpReq     *http.Request
	userID      string
	threadID    string
	runID       string
	appName     string
	userContent *genai.Content
	isResume    bool

	sessionService session.Service
	cachedSession  session.Session
	sessionLoaded  bool
}

func newExecuteContext(ctx context.Context, req *types.RunAgentInput, httpReq *http.Request, userID, threadID, runID, appName string, isResume bool, sessionService session.Service) *executeContext {
	return &executeContext{
		Context:        ctx,
		req:            req,
		httpReq:        httpReq,
		userID:         userID,
		threadID:       threadID,
		runID:          runID,
		appName:        appName,
		isResume:       isResume,
		sessionService: sessionService,
	}
}

func (c *executeContext) Request() *types.RunAgentInput     { return c.req }
func (c *executeContext) HTTPRequest() *http.Request        { return c.httpReq }
func (c *executeContext) UserID() string                    { return c.userID }
func (c *executeContext) ThreadID() string                  { return c.threadID }
func (c *executeContext) RunID() string                     { return c.runID }
func (c *executeContext) AppName() string                   { return c.appName }
func (c *executeContext) UserContent() *genai.Content       { return c.userContent }
func (c *executeContext) SetUserContent(msg *genai.Content) { c.userContent = msg }
func (c *executeContext) IsResume() bool                    { return c.isResume }

// loadSession fetches the ADK session once per Execute and caches it on the context.
// Subsequent ReadonlyState/Events calls reuse the cache so callbacks do not multiply
// session service traffic. Errors are logged but return nil — this is intentional because
// session access is best-effort for callbacks (the run proceeds regardless), and callers
// already handle nil sessions via the empty fallbacks in ReadonlyState/Events.
func (c *executeContext) loadSession() session.Session {
	if c.sessionLoaded {
		return c.cachedSession
	}
	c.sessionLoaded = true
	if c.sessionService == nil {
		return nil
	}
	getResp, err := c.sessionService.Get(c.Context, &session.GetRequest{
		AppName:   c.appName,
		UserID:    c.userID,
		SessionID: c.threadID,
	})
	if err != nil {
		log.Printf("agui: ExecutorContext.loadSession: session.Get failed for %s/%s: %v", c.appName, c.threadID, err)
		return nil
	}
	c.cachedSession = getResp.Session
	return c.cachedSession
}

func (c *executeContext) ReadonlyState() session.ReadonlyState {
	sess := c.loadSession()
	if sess == nil {
		return emptySessionState{}
	}
	return sess.State()
}

func (c *executeContext) Events() session.Events {
	sess := c.loadSession()
	if sess == nil {
		return emptySessionEvents{}
	}
	return sess.Events()
}

type emptySessionState struct{}

func (emptySessionState) Get(string) (any, error) {
	return nil, session.ErrStateKeyNotExist
}

func (emptySessionState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {}
}

type emptySessionEvents struct{}

func (emptySessionEvents) At(int) *session.Event { return nil }

func (emptySessionEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {}
}

func (emptySessionEvents) Len() int { return 0 }

// partConverter picks the GenAIPartConverter that will map genai.Part values to AG-UI
// events during this run. The ExecutorConfig-level converter takes precedence over the
// launcher-level WithGenAIPartConverter. This priority rule exists so that a custom
// executor factory can override mapping without conflicting with a launcher-wide default
// — there is exactly one source of truth per run.
func (d *defaultExecutor) partConverter() GenAIPartConverter {
	if d.cfg.GenAIPartConverter != nil {
		return d.cfg.GenAIPartConverter
	}
	if d.launcher != nil && d.launcher.config != nil {
		return d.launcher.config.genAIPartConverter
	}
	return nil
}

// Execute runs the full AG-UI protocol pipeline for one turn and yields events to the
// HTTP handler via an iterator (iter.Seq2). The handler consumes each yielded event,
// applies CallInterceptor.OnEmit for wire-level mutation/suppression, and writes it as
// an SSE frame. This method must not modify events for wire concerns — that responsibility
// belongs to OnEmit.
//
// The pipeline phases are:
//  1. RunStarted — announce the SSE stream is live.
//  2. Interrupt validation — load pending HITL interrupts, validate resume payloads.
//  3. State snapshot — emit baseline session state for the client.
//  4. Message resolution — build the genai turn from resume, tool results, or user text.
//  5. ADK invocation — stream ADK events and map each to AG-UI protocol events.
//  6. Lifecycle finalization — close open text/reasoning/step events.
//  7. Terminal event — RunFinished (success or interrupt) or RunError.
//  8. Interrupt persistence — persist new interrupts or clear stale ones.
//
// If the client disconnects mid-stream (yield returns false), phases 7-8 are skipped
// to avoid writing interrupt state from an incomplete run.
func (d *defaultExecutor) Execute(ctx context.Context, execCtx ExecutorContext) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		l := d.launcher
		if l == nil || d.runtime == nil {
			yield(nil, fmt.Errorf("agui: executor not configured"))
			return
		}

		// The yield sink bridges the executor (which pushes events) to the HTTP handler (which
		// pulls events via range). Each call to sink.Emit becomes a yield to the iterator
		// consumer (runSSEFunc), which applies OnEmit interceptors and writes SSE. When the
		// client disconnects or the handler stops reading, yield returns false and the sink
		// records itself as stopped. We check sinkStopped before every emit and bail out
		// without persisting interrupts, because writing state from an incomplete run would
		// leave the session in an inconsistent state.
		sink := newYieldSink(yield)
		state := &streamState{}

		var predictMappings map[string][]stream.PredictStateMapping
		if len(l.config.predictStateMappings) > 0 {
			predictMappings = make(map[string][]stream.PredictStateMapping)
			for _, m := range l.config.predictStateMappings {
				predictMappings[m.Tool] = append(predictMappings[m.Tool], stream.PredictStateMapping(m))
			}
		}
		var reqState map[string]any
		if stateMap, ok := execCtx.Request().State.(map[string]any); ok {
			reqState = stateMap
		}
		state.ConfigureRun(execCtx.RunID(), execCtx.ThreadID(), execCtx.UserID(), execCtx.AppName(), ctx, reqState, predictMappings)

		req := execCtx.Request()

		var runErr error
		// emitError is the single path for terminal failures on the SSE stream. The AG-UI
		// protocol allows exactly one terminal event per run (RunFinished or RunError), so
		// we must close any open text/reasoning/step lifecycles before emitting RunError.
		// The runErr variable is captured by this closure — it is populated here and later
		// read by finishRun to pass to AfterExecuteCallback, giving callbacks visibility
		// into why the run failed.
		emitError := func(err error, opts ...events.RunErrorOption) {
			runErr = err
			if state.RunFinalized {
				return
			}
			finalizeLifecycle(sink, state)
			opts = append([]events.RunErrorOption{events.WithRunID(state.RunID)}, opts...)
			sink.Emit(events.NewRunErrorEvent(err.Error(), opts...))
			state.RunFinalized = true
		}

		// RunStarted tells the client the SSE stream is live. The HTTP handler has already
		// flushed headers; this is the first protocol event on the wire.
		sink.Emit(events.NewRunStartedEvent(state.ThreadID, state.RunID))
		if sinkStopped(sink) {
			return
		}

		sessionID := state.ThreadID
		userID := execCtx.UserID()
		appName := execCtx.AppName()

		// Pending interrupts are stored in ADK session state between runs. We load them now
		// so resume payloads can be validated against what is still open (ids, expiry, schema).
		// Invalid resume attempts become RunError instead of starting a broken ADK run.
		pending, err := l.loadPendingInterrupts(ctx, appName, userID, sessionID)
		if err != nil {
			emitError(fmt.Errorf("failed to load pending interrupts: %w", err))
			return
		}
		if err := interrupt.ValidateResumeAgainstPending(req.Resume, pending, time.Now()); err != nil {
			emitError(err)
			return
		}

		// Clients expect a baseline StateSnapshot early in the run. ensureSessionForSnapshot
		// creates the ADK session if needed so snapshot emission matches AutoCreateSession timing.
		snapSess, snapErr := l.ensureSessionForSnapshot(ctx, appName, userID, sessionID, reqState)
		if snapErr != nil {
			emitError(fmt.Errorf("failed to prepare session for state snapshot: %w", snapErr))
			return
		}
		emitStateSnapshotIfNonEmpty(sink, buildStateSnapshot(snapSess, reqState))

		// ADK expects exactly one genai.Content per run, representing the user's turn. The
		// content shape depends on how the client continued the thread:
		//   - HITL resume: client sent resume entries → convert to FunctionResponse parts
		//     that answer the pending adk_request_confirmation.
		//   - Tool results: client executed a tool locally and sent results → convert
		//     trailing tool-role messages to FunctionResponse parts.
		//   - Normal message: extract the last user message (text or multimodal).
		// The resolved message is stored on execCtx so callbacks can inspect it.
		isResumeRun := execCtx.IsResume()
		isToolResultRun := !isResumeRun && aguimsg.IsToolResultSubmission(req.Messages)

		var msg *genai.Content
		switch {
		case isResumeRun:
			msg, err = interrupt.EntriesToConfirmationContent(req.Resume)
			if err != nil {
				emitError(fmt.Errorf("invalid resume payload: %w", err))
				return
			}
		case isToolResultRun:
			msg, err = aguimsg.ExtractToolResultContent(req.Messages)
			if err != nil {
				emitError(fmt.Errorf("failed to extract tool results: %w", err))
				return
			}
		default:
			msg, err = aguimsg.ExtractLastUserMessage(req.Messages)
			if err != nil {
				emitError(err)
				return
			}
		}
		execCtx.SetUserContent(msg)

		// Optional hook for auth, tracing, or last-minute context mutation before ADK runs.
		// The callback may return an enriched context (e.g. with tracing spans or auth tokens).
		// We update both the local ctx (used for the ADK run) and the execCtx's embedded
		// context so that downstream callbacks see the enriched context via execCtx.Context.
		if d.cfg.BeforeExecuteCallback != nil {
			var cbErr error
			ctx, cbErr = d.cfg.BeforeExecuteCallback(ctx, execCtx)
			if cbErr != nil {
				emitError(fmt.Errorf("before execute: %w", cbErr))
				return
			}
			if ctx == nil {
				emitError(fmt.Errorf("before execute: callback returned nil context"))
				return
			}
			if ec, ok := execCtx.(*executeContext); ok {
				ec.Context = ctx
			}
		}

		// Build the adkrun request that will be sent to the ADK runner. Key fields:
		//   - StateDelta: merges client-provided state and client tool definitions into the
		//     ADK session. Client tools are injected under clienttool.StateKey so the
		//     clienttool.Toolset can discover them at agent invocation time.
		//   - InvocationID: set only on resume runs. It tells ADK which paused invocation
		//     to continue, so the confirmation FunctionResponse is routed to the right turn.
		runReq := adkrun.RunRequest{
			AppName:                   appName,
			UserID:                    userID,
			SessionID:                 sessionID,
			NewMessage:                *msg,
			Streaming:                 true,
			SaveInputBlobsAsArtifacts: false,
		}
		if len(reqState) > 0 {
			runReq.StateDelta = reqState
		}
		if len(req.Tools) > 0 {
			if runReq.StateDelta == nil {
				runReq.StateDelta = make(map[string]any)
			}
			runReq.StateDelta[clienttool.StateKey] = req.Tools
		}
		if isResumeRun {
			var resumeSess session.Session
			if resumeSess, err = l.getSession(ctx, appName, userID, sessionID); err != nil {
				log.Printf("agui: resume: session.Get for invocation id: %v", err)
			}
			if invID := resolveInvocationIDForResume(pending, req.Resume, req.State, resumeSess); invID != "" {
				runReq.InvocationID = invID
			}
		}

		partConv := d.partConverter()

		// Start the ADK run and stream session events. Each ADK event is mapped to zero or
		// more AG-UI protocol events by processEvent (text streaming, tool calls, reasoning,
		// steps, state deltas, interrupts). processEvent returns done=true when the run is
		// finalized early (e.g. a tool confirmation interrupt emits RunFinished immediately).
		// The collector wraps the sink so AfterEventCallback can see which AG-UI events
		// were emitted for each ADK event.
		_, adkEvents, err := d.runtime.RunSSE(ctx, runReq)
		if err != nil {
			emitError(err)
			d.finishRun(ctx, execCtx, l, sink, state, runErr, appName, userID, sessionID)
			return
		}

		for ev, err := range adkEvents {
			if sinkStopped(sink) {
				break
			}
			if err != nil {
				emitError(err)
				break
			}
			if ev == nil {
				continue
			}
			if ev.ErrorMessage != "" {
				var opts []events.RunErrorOption
				if ev.ErrorCode != "" {
					opts = append(opts, events.WithErrorCode(ev.ErrorCode))
				}
				emitError(fmt.Errorf("%s", ev.ErrorMessage), opts...)
				break
			}

			collector := newEventCollector(sink)
			done, procErr := l.processEvent(collector, ev, state, partConv)
			if d.cfg.AfterEventCallback != nil {
				if cbErr := d.cfg.AfterEventCallback(execCtx, ev, collector.Emitted); cbErr != nil {
					emitError(fmt.Errorf("after event: %w", cbErr))
					break
				}
			}
			if procErr != nil {
				emitError(procErr)
				break
			}
			if done {
				break
			}
		}

		// Close any still-open text, reasoning, or step lifecycles before a success terminal event.
		if !sinkStopped(sink) {
			finalizeLifecycle(sink, state)
		}

		// Successful runs emit RunFinished with a success outcome, unless the run was already
		// finalized by an interrupt (RunFinished with interrupt outcome) or an error (RunError).
		// The optional MessagesSnapshot gives clients the full conversation history in one
		// payload, so they don't have to reconstruct it from streamed TEXT_MESSAGE_* deltas.
		if !state.RunFinalized && !sinkStopped(sink) {
			if l.config.emitMessagesSnapshotOnRunEnd {
				if sess, ok, err := l.loadSessionForSnapshot(ctx, appName, userID, sessionID); err == nil && ok {
					if msgs, err := l.buildMessagesSnapshot(ctx, sess); err == nil {
						emitMessagesSnapshotIfNonEmpty(sink, msgs)
					}
				}
			}
			sink.Emit(events.NewRunFinishedEventWithOptions(
				state.ThreadID,
				state.RunID,
				events.WithSuccessOutcome(),
			))
			state.RunFinalized = true
		}

		d.finishRun(ctx, execCtx, l, sink, state, runErr, appName, userID, sessionID)
	}
}

// finishRun is the post-run cleanup called at the end of every Execute, regardless of
// success or failure. It runs AfterExecuteCallback (if configured) and then manages
// interrupt persistence: new interrupts are written to session state so the next
// resume run can validate against them, or existing interrupts are cleared on a clean
// success so stale entries don't block future runs on the same thread.
//
// When the client disconnected mid-stream (sinkStopped), finishRun skips persistence
// entirely. Writing interrupt state from an incomplete run could leave the session in
// an inconsistent state (e.g. clearing interrupts when the RunFinished event never
// reached the client).
func (d *defaultExecutor) finishRun(
	ctx context.Context,
	execCtx ExecutorContext,
	l *aguiLauncher,
	sink *yieldSink,
	state *streamState,
	runErr error,
	appName, userID, sessionID string,
) {
	if d.cfg.AfterExecuteCallback != nil {
		if cbErr := d.cfg.AfterExecuteCallback(execCtx, runErr); cbErr != nil {
			if runErr == nil {
				runErr = cbErr
			} else {
				log.Printf("agui: AfterExecuteCallback error (original error takes precedence): %v", cbErr)
			}
		}
	}

	if sinkStopped(sink) {
		log.Printf("agui: SSE stream stopped; skipping interrupt persistence")
		return
	}

	// Persist or clear pending interrupts based on run outcome:
	//   - Interrupts emitted → persist them so the next resume run can validate.
	//   - Clean success (no error, no interrupts) → clear any stale entries.
	//   - Error → leave existing state untouched (the client may retry).
	switch {
	case len(state.EmittedInterrupts) > 0:
		if err := l.persistPendingInterrupts(ctx, appName, userID, sessionID, state.EmittedInterrupts); err != nil {
			log.Printf("agui: failed to persist pending interrupts: %v", err)
		}
	case runErr == nil:
		if err := l.clearPendingInterrupts(ctx, appName, userID, sessionID); err != nil {
			log.Printf("agui: failed to clear pending interrupts: %v", err)
		}
	}
}
