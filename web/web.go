// Package web provides an ADK web launcher that serves routes through
// go.alis.build/mux while mounting ADK sublaunchers on a shared gorilla/mux router.
package web

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"go.alis.build/adk/launchers/internal/launcherutils"
	_ "go.alis.build/adk/launchers/internal/testenv"
	iam "go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/universal"
	adkweb "google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/telemetry"
)

// webConfig holds HTTP server and auth gateway settings for the web launcher.
type webConfig struct {
	port            int
	writeTimeout    time.Duration
	readTimeout     time.Duration
	idleTimeout     time.Duration
	shutdownTimeout time.Duration
	otelToCloud     bool
	useH2C          bool
	// maxPayloadSize is the -max_request_body_size flag. Zero leaves the limit
	// to launcher.Config.MaxPayloadSize, and failing that to adkrest's default.
	maxPayloadSize int64
	// allowOrigins collects the repeated -allow_origins flag.
	allowOrigins []string
	// authGateway is the package-wide go.alis.build/mux gateway installed before
	// serving. It is responsible for resolving the upstream caller identity into
	// the request context so sublauncher handlers can authorize. A nil value
	// installs no gateway. Defaults to [DefaultAuthGateway].
	authGateway alismux.Middleware
}

// Option configures optional [webLauncher] settings applied in [NewLauncher].
type Option func(*webLauncher)

// WithAuthGateway overrides the package-wide authorization gateway installed
// before serving.
//
// The launchers perform authorization only; they assume authentication already
// happened upstream. The gateway's job is to translate the upstream-provided
// identity (for example the x-alis-identity header set by a trusted gateway or
// BFF) into the request context via iam.Identity.Context so that handlers can
// call iam.FromContext / iam.MustFromContext. go.alis.build/mux supports a
// single gateway per process, so a host that also needs to forward identity to
// a downstream gRPC server should compose that behaviour into the middleware
// passed here.
//
// Passing nil is equivalent to [WithoutAuthGateway].
func WithAuthGateway(gw alismux.Middleware) Option {
	return func(l *webLauncher) {
		l.config.authGateway = gw
	}
}

// WithoutAuthGateway disables the package-wide authorization gateway.
//
// Use this when the host installs its own gateway with alismux.AddGateway or
// when identity is resolved per route. Handlers that require an identity still
// fail closed when none is present in the request context.
func WithoutAuthGateway() Option {
	return func(l *webLauncher) {
		l.config.authGateway = nil
	}
}

// DefaultAuthGateway is the authorization gateway installed by [NewLauncher].
//
// It reads the caller identity from the request headers (the x-alis-identity
// header set by a trusted upstream) and injects it into the request context so
// sublauncher handlers can authorize via iam.FromContext. It performs no
// authentication: requests without a valid identity header pass through
// unchanged so that routes using a different scheme (for example the scheduler
// cron endpoint authenticated as the environment service account) keep working.
// Each handler remains responsible for rejecting callers that require an
// identity it cannot find.
func DefaultAuthGateway(w http.ResponseWriter, r *http.Request, next alismux.Func) error {
	if identity, err := iam.FromHeader(r); err == nil {
		r = r.WithContext(identity.Context(r.Context()))
	}
	return next(w, r)
}

// RequireIdentity is a go.alis.build/mux middleware that rejects requests
// lacking a caller identity in the request context with 401 Unauthorized.
//
// Use it on routes whose handlers assume an identity is present (for example
// handlers that call iam.MustFromContext), so a missing identity yields a clean
// 401 instead of a panic. The identity is normally injected by the
// authorization gateway (see [DefaultAuthGateway]) from the upstream
// x-alis-identity header; this middleware only enforces its presence and never
// authenticates.
func RequireIdentity(w http.ResponseWriter, r *http.Request, next alismux.Func) error {
	if _, err := iam.FromContext(r.Context()); err != nil {
		return alismux.UnauthorizedErr("identity required")
	}
	return next(w, r)
}

// webLauncher composes ADK sublaunchers and serves them through go.alis.build/mux.
type webLauncher struct {
	flags        *flag.FlagSet
	config       *webConfig
	sublaunchers []adkweb.Sublauncher
	// maps keyword to sublauncher for the keywords parsed from command line
	activeSublaunchers map[string]adkweb.Sublauncher
}

// Execute implements launcher.Launcher.
func (w *webLauncher) Execute(ctx context.Context, config *launcher.Config, args []string) error {
	if err := config.Validate(); err != nil {
		return err
	}
	remainingArgs, err := w.Parse(args)
	if err != nil {
		return fmt.Errorf("cannot parse args: %w", err)
	}
	err = universal.ErrorOnUnparsedArgs(remainingArgs)
	if err != nil {
		return fmt.Errorf("cannot parse all the arguments: %w", err)
	}
	return w.Run(ctx, config)
}

// CommandLineSyntax implements launcher.Launcher.
func (w *webLauncher) CommandLineSyntax() string {
	var b strings.Builder
	fmt.Fprint(&b, launcherutils.FormatFlagUsage(w.flags))
	fmt.Fprintf(&b, "  You may specify sublaunchers:\n")
	for _, l := range w.sublaunchers {
		fmt.Fprintf(&b, "    * %s - %s\n", l.Keyword(), l.SimpleDescription())
	}
	fmt.Fprintf(&b, "  Sublaunchers syntax:\n")
	for _, l := range w.sublaunchers {
		fmt.Fprintf(&b, "    %s\n  %s\n", l.Keyword(), l.CommandLineSyntax())
	}
	return b.String()
}

// Keyword implements launcher.SubLauncher.
func (w *webLauncher) Keyword() string {
	return "web"
}

// Parse implements launcher.SubLauncher.
func (w *webLauncher) Parse(args []string) ([]string, error) {
	keyToSublauncher := make(map[string]adkweb.Sublauncher)
	for _, l := range w.sublaunchers {
		if _, ok := keyToSublauncher[l.Keyword()]; ok {
			return nil, fmt.Errorf("cannot create web launcher. Keywords for sublaunchers should be unique and they are not: '%s'", l.Keyword())
		}
		keyToSublauncher[l.Keyword()] = l
	}

	err := w.flags.Parse(args)
	if err != nil || !w.flags.Parsed() {
		return nil, fmt.Errorf("failed to parse web flags: %v", err)
	}

	restArgs := w.flags.Args()
	w.activeSublaunchers = make(map[string]adkweb.Sublauncher)

	for len(restArgs) > 0 {
		keyword := restArgs[0]
		if _, ok := w.activeSublaunchers[keyword]; ok {
			return restArgs, fmt.Errorf("the keyword %q is specified and processed more than once, which is not allowed", keyword)
		}

		if sublauncher, ok := keyToSublauncher[keyword]; ok {
			restArgs, err = sublauncher.Parse(restArgs[1:])
			if err != nil {
				return nil, fmt.Errorf("the %q launcher cannot parse arguments: %v", keyword, err)
			}
			w.activeSublaunchers[keyword] = sublauncher
		} else {
			break
		}
	}
	return restArgs, nil
}

// SimpleDescription implements launcher.SubLauncher.
func (w *webLauncher) SimpleDescription() string {
	return "starts web server with additional sub-servers specified by sublaunchers"
}

// applyServiceDefaults fills in the in-memory services it is safe to default.
//
// Every sublauncher shares this config, so a default here reaches agui, the
// scheduler and evals as well as ADK's REST API. Each default is logged,
// because an in-memory service is lost on restart and is not shared between
// instances.
//
//   - Session: defaulted, as this launcher always has.
//   - Memory: defaulted. ADK's REST handlers panic on a nil memory service,
//     dropping the connection with no response, and an in-memory one only
//     costs recall: nothing a user saved lives there.
//   - Artifact: deliberately not defaulted, unlike ADK's own web launcher. A
//     file saved to one instance's memory would vanish on restart or on the
//     next request served elsewhere. Without a service, ADK's REST API
//     answers 503 and a run that saves an artifact fails, which is the loud
//     failure a deployment that forgot to configure one needs.
func applyServiceDefaults(config *launcher.Config) {
	var defaulted []string
	if config.SessionService == nil {
		config.SessionService = session.InMemoryService()
		defaulted = append(defaulted, "session")
	}
	if config.MemoryService == nil {
		config.MemoryService = memory.InMemoryService()
		defaulted = append(defaulted, "memory")
	}
	for _, name := range defaulted {
		log.Printf("No %s service configured. Using an in-memory one, so whatever it holds is lost when the process exits.", name)
	}
}

// Run implements launcher.SubLauncher. It owns the telemetry providers it
// initializes and shuts them down on every exit path.
func (w *webLauncher) Run(ctx context.Context, config *launcher.Config) error {
	applyServiceDefaults(config)

	router, err := w.buildRouter(config)
	if err != nil {
		return err
	}

	if w.config.authGateway != nil {
		alismux.AddGateway(w.config.authGateway)
	}

	if err := mountGorillaOnHostMux(router); err != nil {
		return fmt.Errorf("gorilla host mount failed: %w", err)
	}

	// After the sublaunchers are set up, because ADK's api sublauncher adds its
	// span and log processors to config.TelemetryOptions in SetupSubrouters.
	// Before the listener opens, so the first request is traced and a failed
	// init does not leave a server running.
	telemetryProviders, err := initTelemetry(ctx, config, w.config.otelToCloud)
	if err != nil {
		return fmt.Errorf("telemetry initialization failed: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), w.config.shutdownTimeout)
		defer cancel()
		if err := telemetryProviders.Shutdown(shutdownCtx); err != nil {
			log.Printf("telemetry shutdown failed: %v", err)
		}
	}()

	log.Printf("Starting the web server: %+v", w.config)
	log.Println()
	webURL := fmt.Sprintf("http://localhost:%v", w.config.port)
	log.Printf("Web server starts on %s", webURL)
	for _, l := range w.activeSublaunchers {
		l.UserMessage(webURL, log.Println)
	}
	log.Println()

	addr := fmt.Sprintf(":%d", w.config.port)
	srv := buildHTTPServer(addr, w.config)
	errChan := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutting down the web server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), w.config.shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err, ok := <-errChan:
		if !ok {
			return nil
		}
		return fmt.Errorf("server failed: %w", err)
	}
}

// buildRouter builds the gorilla base router and sets up the active
// sublaunchers on it, host routes first.
//
// It touches no process-wide state: the auth gateway and the host mount are
// Run's job, so this can be driven directly from tests.
func (w *webLauncher) buildRouter(config *launcher.Config) (*mux.Router, error) {
	if len(w.activeSublaunchers) == 0 {
		availableSublaunchers := make([]string, len(w.sublaunchers))
		for i, l := range w.sublaunchers {
			availableSublaunchers[i] = l.Keyword()
		}
		return nil, fmt.Errorf("no active sublaunchers found - please specify them in the command line. Possible values: %v", availableSublaunchers)
	}

	router := BuildBaseRouter()
	registerHealthRoute(router)

	// The flag overrides a limit the embedder set in code only when it was
	// given, and the resolved value feeds both the base router and the
	// sublaunchers, so ADK's REST server and every other gorilla route agree.
	//
	// Only gorilla routes are limited. Host routes are left alone because a
	// body limit counts every byte of a request, so it would cut off a
	// long-lived gRPC stream.
	if w.config.maxPayloadSize > 0 {
		config.MaxPayloadSize = w.config.maxPayloadSize
	}
	router.Use(adkrest.MaxBytesMiddleware(config.MaxPayloadSize))

	// Before any sublauncher runs, so the REST server ADK's api sublauncher
	// builds is given the operator's origins too. Clipped so neither this
	// append nor a sublauncher's writes into a slice the caller owns.
	//
	// config.BindHost is deliberately left empty: this launcher listens on
	// every interface (Cloud Run needs that), so ADK's loopback-only Host
	// check does not apply.
	config.AllowedOrigins = append(slices.Clip(config.AllowedOrigins), w.config.allowOrigins...)

	for _, l := range w.sublaunchers {
		if _, isActive := w.activeSublaunchers[l.Keyword()]; !isActive {
			continue
		}
		if hr, ok := l.(HostRouteSetup); ok {
			if err := hr.SetupHostRoutes(config); err != nil {
				return nil, fmt.Errorf("%s host route setup failed: %w", l.Keyword(), err)
			}
		}
		if err := l.SetupSubrouters(router, config); err != nil {
			return nil, fmt.Errorf("%s subrouter setup failed: %w", l.Keyword(), err)
		}
	}
	return router, nil
}

// initTelemetry initializes OpenTelemetry providers for the web server process.
func initTelemetry(ctx context.Context, config *launcher.Config, otelToCloud bool) (*telemetry.Providers, error) {
	telemetryProviders, err := telemetry.New(ctx, telemetryOptions(config, otelToCloud)...)
	if err != nil {
		return nil, err
	}
	telemetryProviders.SetGlobalOtelProviders()
	return telemetryProviders, nil
}

// telemetryOptions returns the options telemetry is initialized with.
//
// otelToCloud is the -otel_to_cloud flag. Set, it turns export to GCP on.
// Unset, it leaves the choice to config.TelemetryOptions, so the flag's default
// does not override a telemetry.WithOtelToCloud(true) the embedder passed.
func telemetryOptions(config *launcher.Config, otelToCloud bool) []telemetry.Option {
	// A copy: appending to config.TelemetryOptions could write into the
	// embedder's backing array.
	opts := slices.Clone(config.TelemetryOptions)
	if otelToCloud {
		opts = append(opts, telemetry.WithOtelToCloud(true))
	}
	return opts
}

// NewLauncher creates a web launcher that serves through go.alis.build/mux and
// composes one or more Sublaunchers on a shared gorilla/mux router. Sublaunchers
// may optionally implement HostRouteSetup to register routes on the host mux first.
//
// By default it installs [DefaultAuthGateway], which resolves the upstream
// caller identity into the request context. Use [NewLauncherWithOptions] to
// customise or disable that gateway.
func NewLauncher(sublaunchers ...Sublauncher) launcher.SubLauncher {
	return NewLauncherWithOptions(sublaunchers)
}

// NewLauncherWithOptions is [NewLauncher] with additional configuration.
//
// Options tune host-level behaviour such as the authorization gateway (see
// [WithAuthGateway] and [WithoutAuthGateway]).
func NewLauncherWithOptions(sublaunchers []Sublauncher, opts ...Option) launcher.SubLauncher {
	config := &webConfig{authGateway: DefaultAuthGateway}

	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.IntVar(&config.port, "port", 8080, "Localhost port for the server")
	fs.DurationVar(&config.writeTimeout, "write-timeout", 300*time.Second, "Server write timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for writing the response after reading the headers & body")
	fs.DurationVar(&config.readTimeout, "read-timeout", 300*time.Second, "Server read timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for reading the whole request including body")
	fs.DurationVar(&config.idleTimeout, "idle-timeout", 300*time.Second, "Server idle timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for waiting for the next request (only when keep-alive is enabled)")
	fs.DurationVar(&config.shutdownTimeout, "shutdown-timeout", 30*time.Second, "Server shutdown timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for waiting for active requests to finish during shutdown")
	fs.BoolVar(&config.otelToCloud, "otel_to_cloud", false, "Enables OpenTelemetry export to GCP: telemetry.googleapis.com. When not set, a telemetry.WithOtelToCloud option in launcher.Config.TelemetryOptions decides. See adk-go/telemetry package for details about supported options, credentials and environment variables.")
	fs.Int64Var(&config.maxPayloadSize, "max_request_body_size", 0, "Maximum request body size in bytes for routes registered through SetupSubrouters (ADK's api, webui and a2a sublaunchers, and lro). Larger requests are rejected. 0 uses the value set by an embedder, or the ADK default (10 MiB). Routes registered through SetupHostRoutes (agui, scheduler, evals, console and gRPC) are not limited.")
	// Upstream's -allow_origins also feeds a launcher-wide origin guard. This
	// launcher has none: callers are authenticated upstream and arrive with a
	// forwarded identity, so a browser origin is not what decides access here.
	fs.Func("allow_origins", "Optional. Browser origins to add to launcher.Config.AllowedOrigins (e.g. 'https://example.com'; a bare hostname and optional port is read as http). Repeat the flag for more than one. ADK's api sublauncher allows these for CORS and refuses cross-origin browser requests from any other origin with 403. '*' allows any origin.", func(origin string) error {
		config.allowOrigins = append(config.allowOrigins, origin)
		return nil
	})
	// Defaults on, where ADK's own web launcher defaults it off: the host mux
	// serves gRPC and gRPC-Web on this same listener (see HostRouteSetup), and
	// those need HTTP/2. Turning it off is for hosts that register no gRPC.
	fs.BoolVar(&config.useH2C, "h2c", true, "Enable prior-knowledge cleartext HTTP/2 (h2c; no HTTP/1.1 Upgrade) on the web server listener. Required by gRPC and gRPC-Web handlers registered through a sublauncher's SetupHostRoutes. Cleartext is insecure; do not expose it to untrusted networks.")

	l := &webLauncher{
		config:       config,
		flags:        fs,
		sublaunchers: sublaunchers,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// logger logs method, URI, and elapsed time for each gorilla subrouter request.
func logger(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		inner.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

// BuildBaseRouter returns the main gorilla/mux router, which sublaunchers extend
// with sub-routers in SetupSubrouters. It carries the request logger, and the
// launcher mounts it under the go.alis.build/mux host mux as the catch-all for
// anything not claimed by a host route.
//
// Named to match ADK's own web launcher, so a sublauncher written against
// upstream needs no change to build its routes here.
//
// It registers no routes of its own: gorilla/mux serves the first route that
// matches, so anything registered here would shadow the same path a caller
// registers afterwards.
func BuildBaseRouter() *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	router.Use(logger)
	return router
}

// registerHealthRoute serves /health for load balancers and container probes,
// which are configured with a fixed path and cannot know which sublaunchers
// are enabled. Run registers it, rather than BuildBaseRouter, so an embedder
// building its own server keeps /health for itself.
func registerHealthRoute(router *mux.Router) {
	router.HandleFunc("/health", healthHandler).Methods(http.MethodGet, http.MethodHead)
}

// healthHandler reports that the web server is up. It says nothing about the
// agent or its models. The body matches ADK's /api/health, so a probe can point
// at either.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}` + "\n")); err != nil {
		log.Printf("failed to write health response: %v", err)
	}
}

// buildHTTPServer constructs the host HTTP server with timeouts and, unless
// disabled, cleartext HTTP/2 alongside HTTP/1.
func buildHTTPServer(addr string, cfg *webConfig) *http.Server {
	srv := &http.Server{
		Addr:         addr,
		Handler:      alismux.HTTPHandler(),
		WriteTimeout: cfg.writeTimeout,
		ReadTimeout:  cfg.readTimeout,
		IdleTimeout:  cfg.idleTimeout,
	}

	if cfg.useH2C {
		// Both protocols on one listener: REST and Web UI routes stay on
		// HTTP/1.1 while gRPC and gRPC-Web handlers get the HTTP/2 they require.
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		srv.Protocols = protocols
	}

	return srv
}
