package scheduler

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	historyservice "go.alis.build/agui/history/service"
	schedulerext "go.alis.build/agui/scheduler"
	schedjsonrpc "go.alis.build/agui/scheduler/jsonrpc"
	schedulerservice "go.alis.build/agui/scheduler/service"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/internal/launcherutils"
	launchersweb "go.alis.build/adk/launchers/web"
	"go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	adklauncher "google.golang.org/adk/v2/cmd/launcher"
	adkweb "google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/grpc"
)

// Launcher is the public surface of [NewLauncher].
//
// Hosts compose it with [go.alis.build/adk/launchers/web.NewLauncher]. For gRPC, use
// [WithGRPCRegistrar] or call [Launcher.SchedulerService] with schedulerext.RegisterGRPC.
type Launcher interface {
	adkweb.Sublauncher
	launchersweb.HostRouteSetup
	// SchedulerService returns the extension service for host gRPC registration.
	SchedulerService() *schedulerservice.SchedulerService
}

// Option configures optional [schedulerLauncher] settings applied in [NewLauncher].
type Option func(*schedulerLauncher)

// WithCronIdentity sets the IAM principal used for SchedulerService GetCron and UpdateCron
// inside the cron handler.
//
// When unset, [defaultSystemIdentity] is used (alis-build@{ALIS_OS_PROJECT}.iam.gserviceaccount.com).
// The host constructs and owns the [*iam.Identity] (service account, test double, etc.).
func WithCronIdentity(identity *iam.Identity) Option {
	return func(l *schedulerLauncher) {
		l.cronCfg.systemIdentity = identity
	}
}

// WithJSONRPCOptions forwards options to the extension JSON-RPC handler (for example CORS).
func WithJSONRPCOptions(opts ...schedjsonrpc.JSONRPCHandlerOption) Option {
	return func(l *schedulerLauncher) {
		l.jsonrpcOpts = append(l.jsonrpcOpts, opts...)
	}
}

// WithSynchronousExecution waits for the ADK run to finish before returning HTTP 200.
// Agent failures are returned as 500 so Cloud Tasks may retry. Cron persist failures
// (UpdateCron) are logged and return 200 to prevent duplicate agent execution.
// Default is async (extension behavior).
func WithSynchronousExecution(sync bool) Option {
	return func(l *schedulerLauncher) {
		l.cronCfg.syncExecution = sync
	}
}

// WithCronObserver registers callbacks for cron execution lifecycle events.
func WithCronObserver(observer CronObserver) Option {
	return func(l *schedulerLauncher) {
		l.cronCfg.observer = observer
	}
}

// WithCronRunInterceptor registers a [CronRunInterceptor] around each ADK
// invocation inside executeCron. Nil values are ignored.
//
// May be called multiple times; interceptors are invoked in the order they
// were registered. See [CronRunInterceptor] for the ordering, mutation, and
// error contract, and for the concurrency requirement.
func WithCronRunInterceptor(interceptor CronRunInterceptor) Option {
	return func(l *schedulerLauncher) {
		if interceptor != nil {
			l.cronCfg.runInterceptors = append(l.cronCfg.runInterceptors, interceptor)
		}
	}
}

// WithCronRunInterceptors registers multiple [CronRunInterceptor] values in
// order. Nil entries are skipped.
//
// Interceptors added via successive calls to [WithCronRunInterceptor] and
// [WithCronRunInterceptors] compose into a single chain; the final ordering
// matches the order of option application. See [CronRunInterceptor] for
// ordering, mutation, and error semantics.
func WithCronRunInterceptors(interceptors ...CronRunInterceptor) Option {
	return func(l *schedulerLauncher) {
		for _, ic := range interceptors {
			if ic != nil {
				l.cronCfg.runInterceptors = append(l.cronCfg.runInterceptors, ic)
			}
		}
	}
}

// WithThreadService enables thread metadata upserts on cron runs so scheduled
// runs appear alongside interactive /run_sse runs in the AGUI history listing.
// When set, the cron handler calls CreateOrUpdateThread before and after each
// ADK invocation (best-effort: failures are logged, not returned).
//
// Pass the same *historyservice.ThreadService instance as
// [go.alis.build/adk/launchers/agui.WithThreadService]. If a different instance
// is passed, scheduled and interactive runs will write to independent service
// state and will not coexist in the history listing.
//
// A nil svc disables the upsert (equivalent to not setting the option).
func WithThreadService(svc *historyservice.ThreadService) Option {
	return func(l *schedulerLauncher) {
		l.cronCfg.threadService = svc
	}
}

// WithGRPCRegistrar registers SchedulerService on reg during [SetupHostRoutes].
//
// Pass the host's grpc.Server (it implements [grpc.ServiceRegistrar]). The host must
// still mount that server on go.alis.build/mux (for example hostmux.HandleGRPC) and
// add [schedulerservice.UnaryServerInterceptor] (iam.UnaryInterceptor) so that caller
// identity is available to service methods. Do not also call schedulerext.RegisterGRPC
// for the same service instance.
func WithGRPCRegistrar(reg grpc.ServiceRegistrar) Option {
	if reg == nil {
		panic("scheduler: WithGRPCRegistrar requires a non-nil ServiceRegistrar")
	}
	return func(l *schedulerLauncher) {
		l.grpcRegistrar = reg
	}
}

// WithoutSystemAuth disables the in-launcher authentication on the cron callback
// endpoint ([schedulerext.HandlerPath]).
//
// By default the launcher registers the cron callback with alismux.SystemPost,
// which validates the inbound Google ID token and requires the environment
// service account before running the cron. This endpoint is privileged: it
// triggers agent execution and impersonates the cron owner. Disable the check
// only when a trusted upstream already authenticates the caller (and the
// endpoint is not directly reachable); the launcher then registers the callback
// with alismux.Post. The cron handler always runs under the configured system
// identity (see [WithCronIdentity]) regardless of this setting.
func WithoutSystemAuth() Option {
	return func(l *schedulerLauncher) {
		l.disableSystemAuth = true
	}
}

// schedulerLauncher implements [Launcher] and mounts scheduler routes on the host mux.
type schedulerLauncher struct {
	// flags holds CLI flags for the "scheduler" sublauncher keyword.
	flags *flag.FlagSet
	// appName is the ADK application name passed to [adkrun.NewRuntime].
	appName string
	// service is the extension SchedulerService (Spanner + Cloud Tasks); owned by the host.
	service *schedulerservice.SchedulerService
	// cronCfg is passed to cronHandler for identity, sync mode, and observers.
	cronCfg cronConfig
	// jsonrpcOpts are forwarded to schedulerext.RegisterHTTP for the JSON-RPC surface.
	jsonrpcOpts []schedjsonrpc.JSONRPCHandlerOption
	// grpcRegistrar when set triggers schedulerext.RegisterGRPC in [SetupHostRoutes].
	grpcRegistrar grpc.ServiceRegistrar
	// disableSystemAuth drops the in-launcher Google ID token check on the cron
	// callback endpoint, delegating authentication to a trusted upstream. See
	// [WithoutSystemAuth].
	disableSystemAuth bool

	// setupOnce ensures mountHostRoutes runs at most once per launcher instance.
	setupOnce sync.Once
	// setupErr stores the first error from mountHostRoutes.
	setupErr error
}

var (
	_ Launcher                    = (*schedulerLauncher)(nil)
	_ adkweb.Sublauncher          = (*schedulerLauncher)(nil)
	_ launchersweb.HostRouteSetup = (*schedulerLauncher)(nil)
)

// NewLauncher returns a scheduler sublauncher bound to svc and appName.
//
// svc must be constructed by the host (Spanner, Cloud Tasks, TargetUrl, etc.).
// appName is the ADK app to run when a cron fires (-app_name flag overrides at CLI).
//
// Optional gRPC: [WithGRPCRegistrar] during [SetupHostRoutes], or register manually:
//
//	schedulerext.RegisterGRPC(grpcServer, l.SchedulerService())
//
// The host still calls hostmux.HandleGRPC(grpcServer) once per process.
func NewLauncher(appName string, svc *schedulerservice.SchedulerService, opts ...Option) Launcher {
	l := &schedulerLauncher{service: svc, appName: appName}
	for _, opt := range opts {
		opt(l)
	}

	fs := flag.NewFlagSet("scheduler", flag.ContinueOnError)
	fs.StringVar(&l.appName, "app_name", l.appName, "ADK app name to run when a cron fires")
	l.flags = fs

	return l
}

// SchedulerService returns the extension service for host gRPC registration.
func (l *schedulerLauncher) SchedulerService() *schedulerservice.SchedulerService {
	return l.service
}

// Keyword returns the CLI sublauncher keyword ("scheduler").
func (l *schedulerLauncher) Keyword() string { return "scheduler" }

// Parse parses scheduler-specific CLI flags and returns remaining args.
func (l *schedulerLauncher) Parse(args []string) ([]string, error) {
	if err := l.flags.Parse(args); err != nil || !l.flags.Parsed() {
		return nil, fmt.Errorf("scheduler: parse flags: %w", err)
	}
	return l.flags.Args(), nil
}

// CommandLineSyntax returns formatted flag usage for help output.
func (l *schedulerLauncher) CommandLineSyntax() string {
	return launcherutils.FormatFlagUsage(l.flags)
}

// SimpleDescription returns a one-line summary for the web launcher help text.
func (l *schedulerLauncher) SimpleDescription() string {
	return "scheduler JSON-RPC and ADK cron callback"
}

// SetupSubrouters is a no-op; all routes are registered on the host mux via [SetupHostRoutes].
func (l *schedulerLauncher) SetupSubrouters(_ *mux.Router, _ *adklauncher.Config) error {
	return nil
}

// UserMessage prints scheduler endpoint URLs when the web server starts.
func (l *schedulerLauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("       scheduler:  JSON-RPC %s%s", webURL, schedulerext.JSONRPCPath))
	printer(fmt.Sprintf("       scheduler:  cron handler %s%s", webURL, schedulerext.HandlerPath))
}

// SetupHostRoutes registers JSON-RPC and the cron execution handler on go.alis.build/mux.
// Safe to call multiple times; mounting runs once per launcher instance.
func (l *schedulerLauncher) SetupHostRoutes(config *adklauncher.Config) error {
	l.setupOnce.Do(func() {
		l.setupErr = l.mountHostRoutes(config)
	})

	return l.setupErr
}

// mountHostRoutes builds the in-process ADK runtime and registers HTTP routes.
func (l *schedulerLauncher) mountHostRoutes(config *adklauncher.Config) error {
	if l.service == nil {
		return fmt.Errorf("scheduler: service is nil")
	}
	if l.appName == "" {
		return fmt.Errorf("scheduler: app name is required")
	}

	systemIdentity := l.cronCfg.resolveSystemIdentity()
	if systemIdentity == nil {
		return fmt.Errorf("scheduler: system identity required (use WithCronIdentity or set ALIS_OS_PROJECT)")
	}

	rt, err := adkrun.NewRuntime(config, l.appName)
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	l.cronCfg.defaultAppName = l.appName
	l.cronCfg.launcherCfg = config

	l.registerGRPC()

	httpOpts := make([]schedulerext.HTTPOption, 0, len(l.jsonrpcOpts)+1)
	if len(l.jsonrpcOpts) > 0 {
		httpOpts = append(httpOpts, schedulerext.WithJSONRPCOptions(l.jsonrpcOpts...))
	}
	schedulerext.RegisterHTTP(muxRegistrar{}, l.service, httpOpts...)

	// The cron callback is privileged (it runs agents and impersonates the cron
	// owner). By default it is authenticated in-launcher as the environment
	// service account; WithoutSystemAuth delegates that to a trusted upstream.
	handler := cronHandler(l.service, &runtimeAdapter{rt: rt}, systemIdentity, &l.cronCfg)
	if l.disableSystemAuth {
		alismux.Post(schedulerext.HandlerPath, handler)
	} else {
		alismux.SystemPost(schedulerext.HandlerPath, handler)
	}
	return nil
}

const schedulerGRPCServiceName = "alis.agui.scheduler.v1.SchedulerService"

// serviceInfoProvider is satisfied by *grpc.Server but not by grpc.ServiceRegistrar,
// allowing a pre-registration check without importing a concrete type.
type serviceInfoProvider interface {
	GetServiceInfo() map[string]grpc.ServiceInfo
}

// registerGRPC wires SchedulerService into grpcRegistrar when [WithGRPCRegistrar] was used.
func (l *schedulerLauncher) registerGRPC() {
	if l.grpcRegistrar == nil {
		return
	}
	if si, ok := l.grpcRegistrar.(serviceInfoProvider); ok {
		if _, exists := si.GetServiceInfo()[schedulerGRPCServiceName]; exists {
			return
		}
	}
	schedulerext.RegisterGRPC(l.grpcRegistrar, l.service)
}

// muxRegistrar adapts schedulerext.RegisterHTTP to go.alis.build/mux route registration.
type muxRegistrar struct{}

// Handle registers extension HTTP patterns on the host mux.
//
// The launcher authorizes but does not authenticate. The caller identity is
// expected to already be in the request context (the web launcher's
// authorization gateway resolves it from the upstream x-alis-identity header),
// and SchedulerService enforces authorization via iam.MustFromContext + authz.
//
// The POST route is guarded with [launchersweb.RequireIdentity] so an
// identity-less request gets a clean 401 instead of panicking inside
// iam.MustFromContext. OPTIONS is the CORS preflight and must stay identity-free.
func (muxRegistrar) Handle(pattern string, handler http.Handler) {
	switch {
	case strings.HasPrefix(pattern, "POST "+schedulerext.JSONRPCPath):
		alismux.HandleHTTP(pattern, handler, launchersweb.RequireIdentity)
	case strings.HasPrefix(pattern, "OPTIONS "+schedulerext.JSONRPCPath):
		alismux.HandleHTTP(pattern, handler)
	default:
		panic("scheduler: unexpected route " + pattern)
	}
}
