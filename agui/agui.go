package agui

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/internal/launcherutils"
	launchersweb "go.alis.build/adk/launchers/web"
	historyjsonrpc "go.alis.build/agui/history/jsonrpc"
	historyservice "go.alis.build/agui/history/service"
	"go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	"google.golang.org/adk/cmd/launcher"
	weblauncher "google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/session"
	"google.golang.org/grpc"
)

var (
	_ launchersweb.HostRouteSetup = (*aguiLauncher)(nil)
	_ weblauncher.Sublauncher     = &aguiLauncher{}
)

// CORSConfig controls Cross-Origin Resource Sharing headers on the AG-UI endpoint.
// Browser-based frontends (CopilotKit, custom Vue/React apps) require CORS
// because the frontend origin differs from the agent server origin.
type CORSConfig struct {
	// AllowedOrigins is the list of origins permitted to access the endpoint.
	// Use ["*"] to allow all origins (development only — not recommended for production).
	AllowedOrigins []string

	// AllowedHeaders lists HTTP headers the client may send.
	// Defaults to ["Content-Type", "Authorization"] if empty.
	AllowedHeaders []string

	// ExposeHeaders lists response headers the browser may read.
	// Defaults to none if empty.
	ExposeHeaders []string

	// AllowCredentials indicates whether the response to the request can include credentials.
	AllowCredentials bool
}

// GenAIPartConverter converts a genai.Part from an ADK session event into
// zero or more AG-UI events, allowing consumers to intercept, transform, or
// suppress specific parts before the default event mapping runs.
type GenAIPartConverter = stream.PartConverter

// AGUIConfig holds configuration for the AG-UI sublauncher. It is populated
// by [NewLauncher] and functional options; fields are read when routes are
// registered in [SetupHostRoutes] and on each /run_sse request.
type AGUIConfig struct {
	// appName is the ADK runner AppName and used to distinguish the root agent
	// from sub-agent authors when emitting StepStarted/StepFinished events.
	appName string
	// pathPrefix is the HTTP path prefix for /run_sse and /capabilities (default "/agui").
	pathPrefix string
	// interceptors run Before/OnEmit/After hooks around each /run_sse request.
	interceptors []CallInterceptor
	// cors, when non-nil, enables CORS middleware on AG-UI routes.
	cors *CORSConfig
	// capabilities, when non-nil, enables GET /capabilities JSON discovery.
	capabilities *Capabilities
	// genAIPartConverter optionally overrides mapping of genai.Part to AG-UI events.
	genAIPartConverter GenAIPartConverter
	// threadService, when non-nil, enables thread metadata tracking and the
	// GET /threads listing endpoint.
	threadService *historyservice.ThreadService
	// emitMessagesSnapshotOnRunEnd emits a MESSAGES_SNAPSHOT before RunFinished
	// on successful (non-interrupt) runs when true. Interrupt boundaries always
	// emit snapshots regardless of this flag.
	emitMessagesSnapshotOnRunEnd bool
	// predictStateMappings configures predictive state custom events. When a
	// tool call matches a mapping, a PredictState CustomEvent is emitted before
	// the tool call events.
	predictStateMappings []PredictStateMapping
	// enableAgentStateEndpoint registers POST {pathPrefix}/agents/state when true.
	enableAgentStateEndpoint bool
	// appNameResolver optionally extracts app name from RunAgentInput before state/context.
	appNameResolver AppNameResolver
	// historyJSONRPCOpts are forwarded to history jsonrpc.Register when WithThreadService is set.
	historyJSONRPCOpts []historyjsonrpc.JSONRPCHandlerOption
	// grpcRegistrar when set triggers ThreadService.Register in [SetupHostRoutes].
	grpcRegistrar grpc.ServiceRegistrar
	// executorFactory builds the AgentExecutor when host routes mount. When nil,
	// a default factory using [ExecutorConfig] merged from [WithGenAIPartConverter] is used.
	executorFactory ExecutorFactory
	// customExecutorSet is true when [WithExecutor] was called; [WithGenAIPartConverter]
	// is ignored in that case.
	customExecutorSet bool
}

// Option configures an [AGUIConfig] passed to [NewLauncher].
type Option func(*AGUIConfig)

// WithInterceptor adds a CallInterceptor to the AG-UI launcher.
// Interceptors are executed in the order they are added.
func WithInterceptor(interceptor CallInterceptor) Option {
	return func(c *AGUIConfig) {
		c.interceptors = append(c.interceptors, interceptor)
	}
}

// WithCORS enables CORS headers on the AG-UI endpoint. This is required when
// browser-based frontends (CopilotKit, custom SPAs) call the endpoint from a
// different origin than the agent server. The middleware handles OPTIONS
// preflight requests and sets Access-Control-* headers on all responses.
func WithCORS(cors CORSConfig) Option {
	return func(c *AGUIConfig) {
		c.cors = &cors
	}
}

// WithCapabilities declares the agent's capabilities so clients can discover
// supported features via GET /capabilities and adapt their UI accordingly.
//
// It calls [MergeInterruptCapabilities] on the provided value so interrupt
// resume support is advertised by default (see that function for rationale).
// Pass humanInTheLoop.interrupts or approveWithEdits as false to opt out.
func WithCapabilities(caps Capabilities) Option {
	return func(c *AGUIConfig) {
		MergeInterruptCapabilities(&caps)
		MergeClientToolCapabilities(&caps)
		c.capabilities = &caps
	}
}

// WithGenAIPartConverter registers a callback that intercepts genai.Part values
// from ADK session events before the default AG-UI event mapping runs.
//
// When the converter returns a non-nil slice, those events are emitted and the
// default handling for that part is skipped. When it returns (nil, nil), the
// default mapping (text streaming, tool calls, etc.) proceeds normally.
//
// This is the AG-UI equivalent of [adka2a.ExecutorConfig.GenAIPartConverter]:
// it lets consumers customize how specific parts (e.g. generative UI payloads,
// extension-specific function calls) are represented on the SSE stream without
// modifying the launcher itself.
func WithGenAIPartConverter(converter GenAIPartConverter) Option {
	return func(c *AGUIConfig) {
		c.genAIPartConverter = converter
	}
}

// WithExecutor sets a factory that builds the [AgentExecutor] for /run_sse runs.
//
// Use [ExecutorDeps.NewDefault] with [ExecutorConfig] to configure callbacks on the
// stock pipeline, wrap the result to decorate, or return a fully custom
// [AgentExecutor]. See package documentation for configure, decorate, and replace
// examples. When [WithExecutor] is set, [WithGenAIPartConverter] is ignored — the
// factory owns all executor configuration.
func WithExecutor(factory ExecutorFactory) Option {
	return func(c *AGUIConfig) {
		c.executorFactory = factory
		c.customExecutorSet = true
	}
}

// WithThreadService enables thread metadata tracking backed by the given
// [historyservice.ThreadService]. When configured:
//
//   - GET {path_prefix}/threads is registered, returning thread listings with
//     unread/pinned state for the authenticated user.
//   - Each /run_sse request automatically creates or updates the thread's
//     metadata (run count, last activity time, display name on first run).
func WithThreadService(svc *historyservice.ThreadService) Option {
	return func(c *AGUIConfig) {
		c.threadService = svc
	}
}

// WithGRPCRegistrar registers ThreadService on reg during [SetupHostRoutes].
//
// Requires [WithThreadService]. Pass the host's grpc.Server (it implements
// [grpc.ServiceRegistrar]). The host must mount that server on go.alis.build/mux
// (for example hostmux.HandleGRPC) and install iam.UnaryInterceptor so caller
// identity is available to ThreadService methods. Do not also call
// threadService.Register for the same service instance.
func WithGRPCRegistrar(reg grpc.ServiceRegistrar) Option {
	if reg == nil {
		panic("agui: WithGRPCRegistrar requires a non-nil ServiceRegistrar")
	}
	return func(c *AGUIConfig) {
		c.grpcRegistrar = reg
	}
}

// PredictStateMapping declares how a tool call argument should be mapped to
// a state key for predictive state updates. When the LLM calls a tool matching
// Tool, a "PredictState" CustomEvent is emitted before the tool call events,
// telling the UI to optimistically render the state change.
type PredictStateMapping = stream.PredictStateMapping

// WithMessagesSnapshotOnRunEnd enables emitting a MESSAGES_SNAPSHOT event
// before RunFinished on every successful run (not just interrupt boundaries).
// This is useful for AG-UI clients that rely on complete message history
// without maintaining their own from TEXT_MESSAGE_* events.
func WithMessagesSnapshotOnRunEnd() Option {
	return func(c *AGUIConfig) {
		c.emitMessagesSnapshotOnRunEnd = true
	}
}

// WithPredictState configures predictive state updates. When a tool call
// matches one of the mappings, a "PredictState" CustomEvent is emitted on
// the SSE stream before the tool call events, enabling CopilotKit's
// useCoAgentStateRender real-time state preview.
func WithPredictState(mappings ...PredictStateMapping) Option {
	return func(c *AGUIConfig) {
		c.predictStateMappings = append(c.predictStateMappings, mappings...)
	}
}

// WithAgentStateEndpoint registers POST {pathPrefix}/agents/state, which
// returns thread state and message history without starting a new agent run.
// Used by CopilotKit's useCoAgentState for on-demand state retrieval.
func WithAgentStateEndpoint() Option {
	return func(c *AGUIConfig) {
		c.enableAgentStateEndpoint = true
	}
}

// aguiLauncher implements [weblauncher.Sublauncher] for the AG-UI protocol.
// A single instance serves one root agent; see [SetupHostRoutes] for routing.
type aguiLauncher struct {
	flags          *flag.FlagSet
	config         *AGUIConfig
	runtime        *adkrun.Runtime
	launcherCfg    *launcher.Config
	sessionService session.Service
	executor       AgentExecutor

	hostSetupOnce sync.Once
	hostSetupErr  error
}

// NewLauncher creates a new AG-UI sublauncher. Register it with [web.NewLauncher]
// and activate it with the "agui" CLI keyword. The appName argument becomes the
// ADK runner's AppName and must match the root agent name for step event filtering.
func NewLauncher(appName string, opts ...Option) weblauncher.Sublauncher {
	config := &AGUIConfig{
		appName: appName,
	}
	for _, opt := range opts {
		opt(config)
	}

	fs := flag.NewFlagSet("agui", flag.ContinueOnError)
	fs.StringVar(&config.pathPrefix, "path_prefix", "/agui", "AG-UI API path prefix. Default is '/agui'.")

	return &aguiLauncher{
		flags:  fs,
		config: config,
	}
}

// Keyword returns the sublauncher keyword used for CLI dispatch.
func (l *aguiLauncher) Keyword() string {
	return "agui"
}

// Parse parses AG-UI-specific command-line flags from args and normalizes
// the path prefix to ensure it starts with "/" and has no trailing slash.
func (l *aguiLauncher) Parse(args []string) ([]string, error) {
	err := l.flags.Parse(args)
	if err != nil || !l.flags.Parsed() {
		return nil, fmt.Errorf("failed to parse agui flags: %v", err)
	}
	p := l.config.pathPrefix
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	l.config.pathPrefix = strings.TrimSuffix(p, "/")
	return l.flags.Args(), nil
}

// CommandLineSyntax returns a formatted string describing all available flags.
func (l *aguiLauncher) CommandLineSyntax() string {
	return launcherutils.FormatFlagUsage(l.flags)
}

// SimpleDescription returns a human-readable description of the sublauncher.
func (l *aguiLauncher) SimpleDescription() string {
	return "starts AG-UI protocol server for CopilotKit and other AG-UI compatible clients"
}

// SetupSubrouters is a no-op; all routes are registered on the host mux via
// [SetupHostRoutes]. The gorilla subrouter is not used.
func (l *aguiLauncher) SetupSubrouters(_ *mux.Router, _ *launcher.Config) error {
	return nil
}

// SetupHostRoutes registers all AG-UI routes on the process-wide host mux
// (go.alis.build/mux). It creates the ADK runtime and session service used
// by both /run_sse and thread message history.
//
// Routes registered:
//
//	POST {pathPrefix}/run_sse                        — SSE streaming endpoint (authenticated)
//	GET  {pathPrefix}/threads/{threadId}/messages     — thread message history (authenticated)
//	GET  {pathPrefix}/capabilities                    — capability discovery (public, if configured)
//	OPTIONS for each route above                      — CORS preflight (when WithCORS is set)
//
// When [WithThreadService] and [WithGRPCRegistrar] are set, ThreadService is also
// registered on the host grpc.Server for native gRPC and gRPC-Web clients.
func (l *aguiLauncher) SetupHostRoutes(config *launcher.Config) error {
	l.hostSetupOnce.Do(func() {
		l.hostSetupErr = l.mountHostRoutes(config)
	})
	return l.hostSetupErr
}

func (l *aguiLauncher) mountHostRoutes(config *launcher.Config) error {
	rt, err := adkrun.NewRuntime(config, l.config.appName)
	if err != nil {
		return fmt.Errorf("failed to create ADK runtime: %w", err)
	}
	l.runtime = rt
	l.launcherCfg = config
	l.sessionService = config.SessionService

	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	if l.config.executorFactory != nil {
		l.executor = l.config.executorFactory(deps)
	} else {
		cfg := ExecutorConfig{}
		if !l.config.customExecutorSet && l.config.genAIPartConverter != nil {
			cfg.GenAIPartConverter = l.config.genAIPartConverter
		}
		l.executor = deps.NewDefault(cfg)
	}

	// Build the CORS middleware once (nil slice when CORS is not configured).
	corsMW := l.buildCORSMiddleware()

	// POST /run_sse — SSE streaming endpoint for agent runs.
	ssePath := l.config.pathPrefix + "/run_sse"
	l.registerCORSPreflight(ssePath, "POST")
	alismux.Post(ssePath, l.runSSEFunc(), corsMW...)

	// GET /threads/{threadId}/messages — thread message history.
	messagesPath := l.config.pathPrefix + "/threads/{threadId}/messages"
	l.registerCORSPreflight(messagesPath, "GET")
	alismux.Get(messagesPath, l.threadMessagesFunc(), corsMW...)

	l.registerHistoryJSONRPC()
	l.registerGRPC()

	// Thread metadata endpoints (optional, requires WithThreadService).
	if l.config.threadService != nil {
		threadPath := l.config.pathPrefix + "/threads/{threadId}"
		l.registerCORSPreflight(threadPath, "GET, DELETE")
		alismux.Get(threadPath, l.getThreadFunc(), corsMW...)
		alismux.Delete(threadPath, l.deleteThreadFunc(), corsMW...)

		listPath := l.config.pathPrefix + "/threads"
		l.registerCORSPreflight(listPath, "GET")
		alismux.Get(listPath, l.listThreadsFunc(), corsMW...)
	}

	// GET /capabilities — capability discovery (optional).
	if l.config.capabilities != nil {
		capsPath := l.config.pathPrefix + "/capabilities"
		l.registerCORSPreflight(capsPath, "GET")
		alismux.Get(capsPath, l.capabilitiesFunc(), corsMW...)
	}

	// POST /agents/state — on-demand state and message history (optional).
	if l.config.enableAgentStateEndpoint {
		statePath := l.config.pathPrefix + "/agents/state"
		l.registerCORSPreflight(statePath, "POST")
		alismux.Post(statePath, l.agentStateFunc(), corsMW...)
	}

	return nil
}

const threadGRPCServiceName = "alis.agui.history.v1.ThreadService"

// serviceInfoProvider is satisfied by *grpc.Server but not by grpc.ServiceRegistrar,
// allowing a pre-registration check without importing a concrete type.
type serviceInfoProvider interface {
	GetServiceInfo() map[string]grpc.ServiceInfo
}

// registerGRPC wires ThreadService into grpcRegistrar when [WithGRPCRegistrar] and
// [WithThreadService] were both configured.
func (l *aguiLauncher) registerGRPC() {
	if l.config.grpcRegistrar == nil || l.config.threadService == nil {
		return
	}
	if si, ok := l.config.grpcRegistrar.(serviceInfoProvider); ok {
		if _, exists := si.GetServiceInfo()[threadGRPCServiceName]; exists {
			return
		}
	}
	l.config.threadService.Register(l.config.grpcRegistrar)
}

// UserMessage prints the AG-UI endpoint URLs to the console on startup.
func (l *aguiLauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("       agui:  AG-UI SSE endpoint is available at %s%s/run_sse", webURL, l.config.pathPrefix))
	printer(fmt.Sprintf("       agui:  thread messages at %s%s/threads/{threadId}/messages", webURL, l.config.pathPrefix))
	if l.config.threadService != nil {
		printer(fmt.Sprintf("       agui:  thread detail at %s%s/threads/{threadId}", webURL, l.config.pathPrefix))
		printer(fmt.Sprintf("       agui:  thread listing at %s%s/threads", webURL, l.config.pathPrefix))
		printer(fmt.Sprintf("       agui:  history JSON-RPC at %s%s", webURL, historyjsonrpc.JSONRPCPath))
	}
	if l.config.capabilities != nil {
		printer(fmt.Sprintf("       agui:  capabilities at %s%s/capabilities", webURL, l.config.pathPrefix))
	}
	if l.config.enableAgentStateEndpoint {
		printer(fmt.Sprintf("       agui:  agent state at %s%s/agents/state", webURL, l.config.pathPrefix))
	}
}

// capabilitiesFunc returns a [alismux.Func] that serves the agent's
// declared capabilities as JSON.
func (l *aguiLauncher) capabilitiesFunc() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(l.config.capabilities); err != nil {
			http.Error(w, "failed to encode capabilities", http.StatusInternalServerError)
		}
		return nil
	}
}

// agentStateRequest is the JSON request body for POST /agents/state.
type agentStateRequest struct {
	ThreadID string `json:"threadId"`
	AgentID  string `json:"agentId,omitempty"`
}

// agentStateResponse is the JSON response body for POST /agents/state.
type agentStateResponse struct {
	ThreadID     string          `json:"threadId"`
	ThreadExists bool            `json:"threadExists"`
	State        map[string]any  `json:"state"`
	Messages     []types.Message `json:"messages"`
}

// agentStateFunc returns a handler for POST /agents/state that returns
// thread state and message history without starting a new agent run.
func (l *aguiLauncher) agentStateFunc() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req agentStateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return nil
		}
		defer r.Body.Close()

		if req.ThreadID == "" {
			http.Error(w, "threadId is required", http.StatusBadRequest)
			return nil
		}

		ctx := r.Context()
		identity, identityErr := iam.FromContext(ctx)
		if identityErr != nil || identity == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil
		}
		userID := identity.ID
		if userID == "" {
			http.Error(w, "userID is required", http.StatusUnauthorized)
			return nil
		}

		resp := agentStateResponse{
			ThreadID: req.ThreadID,
			State:    map[string]any{},
			Messages: []types.Message{},
		}

		agentID := req.AgentID
		if agentID == "" {
			agentID = r.URL.Query().Get("agentId")
		}
		appName, appNameErr := resolveAppNameFromSources(l, l.launcherCfg, nil, nil, agentID)
		if appNameErr != nil {
			http.Error(w, appNameErr.Error(), http.StatusBadRequest)
			return nil
		}

		sess, ok, loadErr := l.loadSessionForSnapshot(ctx, appName, userID, req.ThreadID)
		if loadErr != nil {
			log.Printf("agui: /agents/state: failed to load session for thread %s: %v", req.ThreadID, loadErr)
			http.Error(w, "failed to load session", http.StatusInternalServerError)
			return nil
		}
		if ok && sess != nil {
			resp.ThreadExists = true
			if snap := buildStateSnapshot(sess, nil); snap != nil {
				resp.State = snap
			}
			if msgs, err := l.buildMessagesSnapshot(ctx, sess); err != nil {
				log.Printf("agui: /agents/state: failed to build messages snapshot for thread %s: %v", req.ThreadID, err)
			} else if len(msgs) > 0 {
				resp.Messages = msgs
			}
		}

		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(resp)
	}
}

// buildCORSMiddleware returns a single [alismux.Middleware] that adds CORS
// response headers on actual (non-preflight) requests. Returns nil when CORS
// is not configured. OPTIONS preflight is handled by dedicated routes
// registered via [registerCORSPreflight].
//
// When AllowCredentials is true, the middleware echoes the request's Origin
// header instead of using "*", because the CORS spec forbids wildcard origins
// with credentialed requests. When AllowCredentials is false and the only
// configured origin is "*", it returns "*" directly.
func (l *aguiLauncher) buildCORSMiddleware() []alismux.Middleware {
	if l.config.cors == nil {
		return nil
	}

	exposeHeaders := ""
	if len(l.config.cors.ExposeHeaders) > 0 {
		exposeHeaders = strings.Join(l.config.cors.ExposeHeaders, ", ")
	}

	mw := func(w http.ResponseWriter, r *http.Request, handler alismux.Func) error {
		if l.setCORSOriginHeaders(w, r) && exposeHeaders != "" {
			w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
		}
		return handler(w, r)
	}

	return []alismux.Middleware{mw}
}

// setCORSOriginHeaders checks the request origin against the configured
// AllowedOrigins and sets Access-Control-Allow-Origin (and Allow-Credentials
// when applicable). Returns true if the origin was allowed.
func (l *aguiLauncher) setCORSOriginHeaders(w http.ResponseWriter, r *http.Request) bool {
	corsCfg := l.config.cors
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	allowed := false
	for _, a := range corsCfg.AllowedOrigins {
		if a == "*" || a == origin {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}

	if corsCfg.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else if len(corsCfg.AllowedOrigins) == 1 && corsCfg.AllowedOrigins[0] == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	return true
}

// runSSEFunc returns the handler for the AG-UI /run_sse endpoint.
//
// The handler has two phases separated by the SSE commitment point:
//   - Pre-SSE: request parsing, interceptors, validation.
//     Errors in this phase return standard HTTP error responses.
//   - Post-SSE: after SSE headers are written and RunStartedEvent is emitted.
//     Errors in this phase are delivered as RunErrorEvent on the SSE stream.
func (l *aguiLauncher) runSSEFunc() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		// Pre-SSE phase: errors use http.Error.

		var req types.RunAgentInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("agui: error decoding request: %v", err)
			http.Error(w, "Invalid AGUI request payload", http.StatusBadRequest)
			return nil
		}
		defer r.Body.Close()

		// Use client-provided IDs when available; generate otherwise.
		runID := req.RunID
		if runID == "" {
			runID = events.GenerateRunID()
		}
		threadID := req.ThreadID
		if threadID == "" {
			threadID = uuid.New().String()
		}

		ctx := r.Context()

		// Populate user from the mux IAM identity when present. This is
		// best-effort: interceptors may supply or override the user below, and
		// the userID is validated after interceptors run.
		callCtx := &CallContext{User: &User{}}
		if identity, identityErr := iam.FromContext(ctx); identityErr == nil && identity != nil {
			callCtx.User.Name = identity.ID
			callCtx.User.Authenticated = true
		}

		// Run Before interceptors, tracking how many succeeded so After
		// only runs for those (prevents calling After for interceptors
		// whose Before never ran or failed).
		var handlerErr error
		var succeeded int
		defer func() {
			for i := succeeded - 1; i >= 0; i-- {
				if afterErr := l.config.interceptors[i].After(ctx, callCtx, handlerErr); afterErr != nil {
					log.Printf("agui: After interceptor error: %v", afterErr)
				}
			}
		}()
		for i, interceptor := range l.config.interceptors {
			var err error
			ctx, err = interceptor.Before(ctx, callCtx, &req, r)
			if err != nil {
				http.Error(w, "Interceptor rejected request: "+err.Error(), http.StatusInternalServerError)
				return nil
			}
			succeeded = i + 1
		}

		// Interceptors may populate callCtx.User; validate it's set.
		if callCtx == nil || callCtx.User == nil || callCtx.User.Name == "" {
			http.Error(w, "userID is required", http.StatusBadRequest)
			return nil
		}
		userID := callCtx.User.Name

		resolvedAppName, appNameErr := resolveAppName(l, req, l.launcherCfg)
		if appNameErr != nil {
			http.Error(w, appNameErr.Error(), http.StatusBadRequest)
			return nil
		}

		// Upsert thread metadata after interceptors succeed and userID is
		// validated, so rejected requests don't increment run_count.
		if l.config.threadService != nil && threadID != "" && userID != "" {
			if iamIdentity, identityErr := iam.FromContext(ctx); identityErr == nil && iamIdentity != nil {
				l.upsertThreadMetadata(ctx, iamIdentity, threadID, resolvedAppName, &req)
			}
		}

		isResumeRun := len(req.Resume) > 0

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			handlerErr = fmt.Errorf("failed to flush SSE headers: %w", err)
			http.Error(w, handlerErr.Error(), http.StatusInternalServerError)
			return nil
		}

		sseWriter := sse.NewSSEWriter()
		wire := newEmitter(ctx, w, sseWriter)

		writeEvent := func(event events.Event) {
			if wire.Err() != nil || event == nil {
				return
			}
			for i := 0; i < succeeded; i++ {
				var err error
				event, err = l.config.interceptors[i].OnEmit(ctx, callCtx, event)
				if err != nil {
					wire.SetErr(err)
					return
				}
				if event == nil {
					return
				}
			}
			wire.Emit(event)
		}

		execCtx := newExecuteContext(ctx, &req, r, userID, threadID, runID, resolvedAppName, isResumeRun, l.sessionService)

		if l.executor == nil {
			handlerErr = fmt.Errorf("agui: executor not configured")
			writeEvent(events.NewRunErrorEvent(handlerErr.Error(), events.WithRunID(runID)))
			return nil
		}

		for event, err := range l.executor.Execute(ctx, execCtx) {
			if err != nil {
				handlerErr = err
				break
			}
			writeEvent(event)
			if wire.Err() != nil {
				break
			}
		}
		return nil
	}
}
