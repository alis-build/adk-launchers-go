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
	"strings"
	"time"

	"github.com/gorilla/mux"
	"go.alis.build/adk/launchers/internal/launcherutils"
	iam "go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/universal"
	adkweb "google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/telemetry"
)

// webConfig contains parameters for launching web server
type webConfig struct {
	port            int
	writeTimeout    time.Duration
	readTimeout     time.Duration
	idleTimeout     time.Duration
	shutdownTimeout time.Duration
	otelToCloud     bool
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

type webLauncher struct {
	flags        *flag.FlagSet
	config       *webConfig
	sublaunchers []adkweb.Sublauncher
	// maps keyword to sublauncher for the keywords parsed from command line
	activeSublaunchers map[string]adkweb.Sublauncher
}

// Execute implements launcher.Launcher.
func (w *webLauncher) Execute(ctx context.Context, config *launcher.Config, args []string) error {
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

// Run implements launcher.SubLauncher.
func (w *webLauncher) Run(ctx context.Context, config *launcher.Config) error {
	if config.SessionService == nil {
		config.SessionService = session.InMemoryService()
	}

	router := buildRouter()

	if len(w.activeSublaunchers) == 0 {
		availableSublaunchers := make([]string, len(w.sublaunchers))
		for i, l := range w.sublaunchers {
			availableSublaunchers[i] = l.Keyword()
		}
		return fmt.Errorf("no active sublaunchers found - please specify them in the command line. Possible values: %v", availableSublaunchers)
	}

	for _, l := range w.sublaunchers {
		if _, isActive := w.activeSublaunchers[l.Keyword()]; !isActive {
			continue
		}
		if hr, ok := l.(HostRouteSetup); ok {
			if err := hr.SetupHostRoutes(config); err != nil {
				return fmt.Errorf("%s host route setup failed: %w", l.Keyword(), err)
			}
		}
		if err := l.SetupSubrouters(router, config); err != nil {
			return fmt.Errorf("%s subrouter setup failed: %w", l.Keyword(), err)
		}
	}

	if w.config.authGateway != nil {
		alismux.AddGateway(w.config.authGateway)
	}

	alismux.HandleHTTP("/", router)

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

	telemetryService, err := initTelemetry(ctx, config, w.config.otelToCloud)
	if err != nil {
		return fmt.Errorf("telemetry initialization failed: %w", err)
	}

	select {
	case <-ctx.Done():
		log.Println("Shutting down the web server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), w.config.shutdownTimeout)
		defer cancel()
		serverErr := srv.Shutdown(shutdownCtx)
		telemetryErr := telemetryService.Shutdown(shutdownCtx)
		return errors.Join(serverErr, telemetryErr)
	case err, ok := <-errChan:
		if !ok {
			return nil
		}
		return fmt.Errorf("server failed: %w", err)
	}
}

// initTelemetry initializes telemetry and sets the global OTel providers.
func initTelemetry(ctx context.Context, config *launcher.Config, otelToCloud bool) (*telemetry.Providers, error) {
	opts := append(config.TelemetryOptions, telemetry.WithOtelToCloud(otelToCloud))
	telemetryProviders, err := telemetry.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	telemetryProviders.SetGlobalOtelProviders()
	return telemetryProviders, nil
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
	fs.DurationVar(&config.writeTimeout, "write-timeout", 15*time.Second, "Server write timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for writing the response after reading the headers & body")
	fs.DurationVar(&config.readTimeout, "read-timeout", 15*time.Second, "Server read timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for reading the whole request including body")
	fs.DurationVar(&config.idleTimeout, "idle-timeout", 60*time.Second, "Server idle timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for waiting for the next request (only when keep-alive is enabled)")
	fs.DurationVar(&config.shutdownTimeout, "shutdown-timeout", 15*time.Second, "Server shutdown timeout (i.e. '10s', '2m' - see time.ParseDuration for details) - for waiting for active requests to finish during shutdown")
	fs.BoolVar(&config.otelToCloud, "otel_to_cloud", false, "Enables/disables OpenTelemetry export to GCP: telemetry.googleapis.com. See adk-go/telemetry package for details about supported options, credentials and environment variables.")

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

// logger is a middleware that logs the HTTP method, request URI, and the time taken to process the request.
func logger(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		inner.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

// buildRouter returns the ADK sublauncher router.
func buildRouter() *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	router.Use(logger)
	return router
}

func buildHTTPServer(addr string, cfg *webConfig) *http.Server {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Addr:         addr,
		Handler:      alismux.HTTPHandler(),
		WriteTimeout: cfg.writeTimeout,
		ReadTimeout:  cfg.readTimeout,
		IdleTimeout:  cfg.idleTimeout,
		Protocols:    protocols,
	}
}
