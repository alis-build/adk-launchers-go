package console

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	iofs "io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"sync"

	"github.com/gorilla/mux"
	"go.alis.build/adk/launchers/internal/launcherutils"
	launchersweb "go.alis.build/adk/launchers/web"
	"go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	adklauncher "google.golang.org/adk/cmd/launcher"
	adkweb "google.golang.org/adk/cmd/launcher/web"
)

//go:embed all:app/dist
var distFS embed.FS

// Branding configures shell-level console chrome (not per-agent metadata).
// Title and DisplayName are set directly. Logo and Favicon are optional
// AssetResolvers resolved at setup into LogoURL and FaviconURL for runtime-config.json.
type Branding struct {
	Title       string `json:"title,omitempty"`
	DisplayName string `json:"displayName,omitempty"`

	// Resolved at setup; emitted in runtime-config.json.
	FaviconURL string `json:"faviconUrl,omitempty"`
	LogoURL    string `json:"logoUrl,omitempty"`

	// Per-field resolvers; not serialized.
	Favicon AssetResolver `json:"-"`
	Logo    AssetResolver `json:"-"`
}

// Option configures the console sublauncher.
type Option func(*consoleLauncher)

// WithBranding sets shell title, display name, and optional logo/favicon resolvers.
// Resolved asset URLs are exposed in runtime-config.json.
func WithBranding(b Branding) Option {
	return func(l *consoleLauncher) {
		l.branding = &b
	}
}

// WithDevServerURL sets the Vite dev server URL and enables the dev proxy.
// When unset, SPA_DEV_SERVER_URL is read at request time; the proxy target
// defaults to http://localhost:8000.
func WithDevServerURL(url string) Option {
	return func(l *consoleLauncher) {
		l.devServerURL = url
	}
}

// WithIsLocal overrides dev-proxy vs embedded-dist selection. When the function
// returns true, GET / is proxied to the Vite dev server; when false, app/dist
// is served from go:embed. By default, the dev proxy is used only when
// SPA_DEV_SERVER_URL is set or WithDevServerURL was called.
func WithIsLocal(fn func() bool) Option {
	return func(l *consoleLauncher) {
		l.isLocal = fn
	}
}

type runtimeConfigResponse struct {
	Agents         []string  `json:"agents,omitempty"`
	DefaultAgentID string    `json:"defaultAgentId,omitempty"`
	Branding       *Branding `json:"branding,omitempty"`
	GcpProject     string    `json:"gcpProject,omitempty"`
}

type consoleLauncher struct {
	flags        *flag.FlagSet
	branding     *Branding
	devServerURL string
	isLocal      func() bool

	distHandler http.Handler
	devProxy    *httputil.ReverseProxy

	setupOnce sync.Once
	setupErr  error
}

var (
	_ adkweb.Sublauncher          = (*consoleLauncher)(nil)
	_ launchersweb.HostRouteSetup = (*consoleLauncher)(nil)
)

// NewLauncher returns a console sublauncher that serves the embedded Vue SPA,
// runtime-config.json, and /auth/me on the host mux.
//
// Register console last in web.NewLauncher so the / catch-all does not shadow
// agui, scheduler, or other API routes.
func NewLauncher(opts ...Option) adkweb.Sublauncher {
	l := &consoleLauncher{}
	for _, opt := range opts {
		opt(l)
	}
	if l.isLocal == nil {
		l.isLocal = l.defaultUseDevProxy
	}
	distRoot, err := iofs.Sub(distFS, "app/dist")
	if err != nil {
		panic("console: embed app/dist: " + err.Error())
	}
	l.distHandler = http.FileServer(http.FS(distRoot))
	l.devProxy = httputil.NewSingleHostReverseProxy(l.devServerURLParsed())

	l.flags = flag.NewFlagSet("console", flag.ContinueOnError)
	return l
}

func (l *consoleLauncher) defaultUseDevProxy() bool {
	if l.devServerURL != "" {
		return true
	}
	return os.Getenv("SPA_DEV_SERVER_URL") != ""
}

func (l *consoleLauncher) devServerURLParsed() *url.URL {
	devURL := l.devServerURL
	if devURL == "" {
		devURL = os.Getenv("SPA_DEV_SERVER_URL")
	}
	if devURL == "" {
		devURL = "http://localhost:8000"
	}
	u, err := url.Parse(devURL)
	if err != nil {
		panic("console: invalid dev server URL: " + err.Error())
	}
	return u
}

func (l *consoleLauncher) Keyword() string { return "console" }

func (l *consoleLauncher) Parse(args []string) ([]string, error) {
	if err := l.flags.Parse(args); err != nil || !l.flags.Parsed() {
		return nil, fmt.Errorf("console: parse flags: %w", err)
	}
	return l.flags.Args(), nil
}

func (l *consoleLauncher) CommandLineSyntax() string {
	return launcherutils.FormatFlagUsage(l.flags)
}

func (l *consoleLauncher) SimpleDescription() string {
	return "Vue console SPA, runtime config, and /auth/me"
}

func (l *consoleLauncher) SetupSubrouters(_ *mux.Router, _ *adklauncher.Config) error {
	return nil
}

func (l *consoleLauncher) SetupHostRoutes(config *adklauncher.Config) error {
	l.setupOnce.Do(func() {
		l.setupErr = l.mountHostRoutes(config)
	})
	return l.setupErr
}

func (l *consoleLauncher) mountHostRoutes(config *adklauncher.Config) error {
	registrar := hostBrandingRegistrar{}
	if l.branding != nil {
		if err := resolveBranding(l.branding, registrar); err != nil {
			return fmt.Errorf("console: branding: %w", err)
		}
	}

	alismux.AuthenticatedGet("/assets/config/runtime-config.json", l.runtimeConfigHandler(config))
	alismux.AuthenticatedGet("/auth/me", getMe)
	alismux.AuthenticatedGet("/", l.spaHandler)
	return nil
}

type hostBrandingRegistrar struct{}

func (hostBrandingRegistrar) AuthenticatedGet(pattern string, handler http.Handler) {
	alismux.AuthenticatedGet(pattern, func(w http.ResponseWriter, r *http.Request) error {
		handler.ServeHTTP(w, r)
		return nil
	})
}

func (l *consoleLauncher) spaHandler(w http.ResponseWriter, r *http.Request) error {
	if l.isLocal() {
		l.devProxy.ServeHTTP(w, r)
		return nil
	}
	if path.Ext(r.URL.Path) == "" {
		cloned := *r.URL
		cloned.Path = "/"
		r2 := *r
		r2.URL = &cloned
		r = &r2
	}
	l.distHandler.ServeHTTP(w, r)
	return nil
}

func (l *consoleLauncher) runtimeConfigHandler(config *adklauncher.Config) alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		resp := runtimeConfigResponse{
			GcpProject: os.Getenv("ALIS_OS_PROJECT"),
		}
		if l.branding != nil {
			b := *l.branding
			resp.Branding = &b
		}
		if config != nil && config.AgentLoader != nil {
			resp.Agents = config.AgentLoader.ListAgents()
			resp.DefaultAgentID = config.AgentLoader.RootAgent().Name()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		return json.NewEncoder(w).Encode(resp)
	}
}

func getMe(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")

	identity, err := iam.FromContext(r.Context())
	if err != nil || identity == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil
	}
	return json.NewEncoder(w).Encode(map[string]string{
		"sub":   identity.ID,
		"email": identity.Email,
	})
}

func (l *consoleLauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("       console:  SPA at %s/", webURL))
	printer(fmt.Sprintf("       console:  runtime config at %s/assets/config/runtime-config.json", webURL))
	if l.isLocal() {
		printer(fmt.Sprintf("       console:  Vite dev proxy at %s (embedded dist when SPA_DEV_SERVER_URL is unset)", l.devServerURLParsed()))
	} else {
		printer("       console:  serving embedded app/dist")
	}
}
