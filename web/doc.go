// Package web provides an ADK web launcher built on go.alis.build/mux.
//
// ADK sublaunchers register routes on a shared gorilla/mux subrouter via [Sublauncher].
// After setup, the router is mounted on the process-wide host mux with an
// unmethoded "/" catch-all (GET /ui/, DELETE /api/..., etc.) plus POST prefix
// patterns discovered from the gorilla route tree so REST POST requests reach
// gorilla instead of the gRPC fallback (POST /). Unmethoded "/" avoids
// conflicting with method-specific host routes registered via HostRouteSetup.
//
// # Gorilla host mount
//
// [mountGorillaOnHostMux] registers the shared gorilla router on the host mux
// in two layers: an unmethoded "/" catch-all for GET, DELETE, and other
// non-POST traffic, plus method-specific "POST {path}" patterns derived from
// the gorilla route tree. POST patterns are more specific than the gRPC fallback
// (POST /) so REST POST handlers reach gorilla while native gRPC keeps POST /.
// Sublaunchers that mount only a subtree (for example lro resume-operation
// routes) avoid registering a broad POST / catch-all on gorilla.
//
// Sublaunchers that need host-level features (identity-aware routes, gRPC, and
// similar) may optionally implement [HostRouteSetup]. SetupHostRoutes runs before
// SetupSubrouters so specific host patterns take precedence over the gorilla
// catch-all. ADK built-in sublaunchers do not implement HostRouteSetup and are
// unaffected.
//
// # Authentication and authorization
//
// The launchers authorize but do not authenticate: authentication is expected to
// have already happened at a trusted upstream (for example a gateway or BFF)
// before a request reaches the process. That upstream is responsible for
// forwarding the caller identity as the x-alis-identity header.
//
// To make the forwarded identity available to handlers, [NewLauncher] installs
// [DefaultAuthGateway] as the package-wide go.alis.build/mux gateway. It reads
// the identity from the request headers and injects it into the request context,
// so handlers can call iam.FromContext / iam.MustFromContext to authorize the
// caller. It performs no authentication and does not reject requests that lack an
// identity; each handler decides whether an identity is required.
//
// Hosts that need different behaviour (for example forwarding the identity to a
// downstream gRPC server, or installing their own gateway) use
// [NewLauncherWithOptions] with [WithAuthGateway] or [WithoutAuthGateway].
// go.alis.build/mux supports a single gateway per process, so the web launcher
// owns that slot.
//
// Trust boundary: because the launchers no longer authenticate, the x-alis-identity
// header must only ever be set by the trusted upstream. Deployments must ensure
// the process is not directly reachable and that the header is stripped or
// overwritten at the edge.
//
// # Launcher usage
//
//	import (
//	    "go.alis.build/adk/launchers/agui"
//	    "go.alis.build/adk/launchers/web"
//	)
//
//	launcher := web.NewLauncher(agui.NewLauncher("my-agent"))
//
// At runtime:
//
//	adk web --port 8080 agui
//
// Importing go.alis.build/mux requires IDENTITY_SERVICE_URL and ALIS_OS_PROJECT
// to be set at process start.
//
// # Server behaviour
//
// The launcher tracks ADK's own web launcher, with these deliberate differences
// for deployments behind a trusted gateway on Cloud Run:
//
//   - It listens on every interface. ADK defaults to loopback, which a
//     container cannot be reached on.
//   - -h2c defaults on, because gRPC and gRPC-Web share the listener.
//   - It defaults the session and memory services to in-memory ones but leaves
//     the artifact service unset, so a deployment that forgot to configure one
//     fails loudly instead of losing files on restart.
//   - -allow_origins only adds to launcher.Config.AllowedOrigins, which ADK's
//     api sublauncher uses for CORS and its own cross-origin check. There is no
//     launcher-wide origin guard: access is decided by the forwarded identity.
//   - -max_request_body_size limits routes registered through SetupSubrouters
//     only (ADK's api, webui and a2a, and lro). Host routes are not limited:
//     a body limit counts every byte, so it would cut off a long-lived gRPC
//     stream.
//
// GET /health answers {"status":"ok"} for probes, whatever sublaunchers are
// active.
//
// # ADK's REST API and web UI
//
// ADK's api sublauncher builds its REST server with launcher.Config's
// Authenticator and Authorizer. When the embedder leaves them nil, the launcher
// fills them with [IdentityAuthenticator] and authz.NewStrict: a request with no
// identity (or one without an ID) answers 401, and a caller acting as any user
// but themselves, in the path, the JSON body or the query, answers 403. ADK's
// public routes such as /api/health stay open. There is no flag to loosen this.
// In code, Config.Authorizer = authz.NewNoop() lets any signed-in caller act as
// any user, and a fully open API also needs Config.Authenticator =
// authn.NewNoop().
//
// The checks cover ADK's REST API only. ADK's A2A routes have no
// authentication.
//
// ADK's web UI acts as user "user" unless its page is loaded with ?userId=. A
// browser opening /ui/ without one, with an identity on the request, is
// redirected to /ui/?userId=<identity ID>, so the UI sends the caller's real ID
// on every call. This needs the upstream gateway to forward x-alis-identity on
// page navigations and on the /run_live WebSocket upgrade, not only on API
// calls; otherwise the UI acts as "user" and every REST call answers 403. With
// [WithoutAuthGateway] and no gateway of the host's own, every /api route
// answers 401.
//
// # HostRouteSetup examples
//
// Implement [HostRouteSetup] on the same type that implements [Sublauncher].
// Register host routes in SetupHostRoutes; keep SetupSubrouters for gorilla-only
// paths (for example unauthenticated local dev) or leave it empty when all routes
// live on the host mux.
//
// GET that authorizes the caller (identity injected by the auth gateway):
//
//	import (
//	    "net/http"
//
//	    "go.alis.build/iam/v3"
//	    hostmux "go.alis.build/mux"
//	    "google.golang.org/adk/v2/cmd/launcher"
//	)
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    hostmux.Get("/my-agent/status", func(w http.ResponseWriter, r *http.Request) error {
//	        // The auth gateway (DefaultAuthGateway) injects the identity; if a
//	        // route requires one, read it and fail closed when it is absent.
//	        identity, err := iam.FromContext(r.Context())
//	        if err != nil {
//	            http.Error(w, "unauthorized", http.StatusUnauthorized)
//	            return nil
//	        }
//	        _, err = w.Write([]byte("hello, " + identity.Email))
//	        return err
//	    })
//	    return nil
//	}
//
// POST or an existing http.Handler (for example SSE). The auth gateway injects
// the identity; the handler authorizes:
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    hostmux.HandleHTTP("POST /my-agent/run_sse", l.runSSEHandler())
//	    return nil
//	}
//
// CORS preflight for a browser client must stay identity-free; register OPTIONS
// on the host mux:
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    hostmux.HandleHTTP("POST /my-agent/run_sse", l.handler())
//	    hostmux.Options("/my-agent/run_sse", func(w http.ResponseWriter, r *http.Request) error {
//	        w.WriteHeader(http.StatusNoContent)
//	        return nil
//	    })
//	    return nil
//	}
//
// Native gRPC (HTTP/2) on the same listener. Only one broad gRPC fallback (POST /)
// is allowed per process—register from a single sublauncher or from main, not from
// every sublauncher:
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    hostmux.HandleGRPC(l.grpcServer) // grpc.Server implements http.Handler
//	    return nil
//	}
//
// gRPC-Web for browser clients (identity injected by the auth gateway, as for
// other routes):
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    hostmux.HandleGRPCWeb(l.grpcWebServer)
//	    return nil
//	}
//
// After host setup, SetupSubrouters still runs for gorilla routes. Example split:
// production uses the host mux (covered by the auth gateway); local dev uses
// gorilla only:
//
//	func (l *myLauncher) SetupHostRoutes(config *launcher.Config) error {
//	    if !l.requireAuth {
//	        return nil
//	    }
//	    hostmux.HandleHTTP("POST /my-agent/run_sse", l.handler())
//	    return nil
//	}
//
//	func (l *myLauncher) SetupSubrouters(router *mux.Router, config *launcher.Config) error {
//	    if l.requireAuth {
//	        return nil // routes already on host mux
//	    }
//	    router.Handle("/my-agent/run_sse", l.handler()).Methods(http.MethodPost)
//	    return nil
//	}
package web
