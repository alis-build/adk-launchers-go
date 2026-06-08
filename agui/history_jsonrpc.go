package agui

import (
	"net/http"
	"strings"

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

func (historyMuxRegistrar) Handle(pattern string, handler http.Handler) {
	switch {
	case strings.HasPrefix(pattern, "POST "+historyjsonrpc.JSONRPCPath):
		alismux.AuthenticatedHandleHTTP(pattern, handler)
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
