package agui

import (
	"net/http"
	"strings"

	launchersweb "go.alis.build/adk/launchers/web"
	historyjsonrpc "go.alis.build/agui/history/jsonrpc"
	alismux "go.alis.build/mux"
)

// WithHistoryJSONRPCOptions forwards options to the history JSON-RPC handler (for example CORS).
func WithHistoryJSONRPCOptions(opts ...historyjsonrpc.JSONRPCHandlerOption) Option {
	return func(c *AGUIConfig) {
		c.historyJSONRPCOpts = append(c.historyJSONRPCOpts, opts...)
	}
}

type historyMuxRegistrar struct{}

// Handle registers the history JSON-RPC routes on the host mux.
//
// The launcher authorizes but does not authenticate. The caller identity is
// expected to already be in the request context (the web launcher's
// authorization gateway resolves it from the upstream x-alis-identity header),
// and the history ThreadService enforces authorization.
//
// The POST route is guarded with [launchersweb.RequireIdentity] so an
// identity-less request gets a clean 401 instead of panicking downstream.
// OPTIONS is the CORS preflight and must stay identity-free.
func (historyMuxRegistrar) Handle(pattern string, handler http.Handler) {
	switch {
	case strings.HasPrefix(pattern, "POST "+historyjsonrpc.JSONRPCPath):
		alismux.HandleHTTP(pattern, handler, launchersweb.RequireIdentity)
	case strings.HasPrefix(pattern, "OPTIONS "+historyjsonrpc.JSONRPCPath):
		alismux.HandleHTTP(pattern, handler)
	default:
		panic("agui: unexpected history JSON-RPC route " + pattern)
	}
}

func (l *aguiLauncher) registerHistoryJSONRPC() {
	if l.config.threadService == nil {
		return
	}
	historyjsonrpc.Register(historyMuxRegistrar{}, l.config.threadService, l.config.historyJSONRPCOpts...)
}
