// Package web provides an ADK web launcher built on go.alis.build/mux.
//
// ADK sublaunchers register routes on a shared gorilla/mux subrouter via [Sublauncher].
// The subrouter is mounted at "/" on the process-wide host mux after all sublaunchers
// have run setup.
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
