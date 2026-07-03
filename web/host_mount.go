package web

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gorilla/mux"
	alismux "go.alis.build/mux"
)

// mountGorillaOnHostMux registers the shared gorilla/mux subrouter on
// go.alis.build/mux. Non-POST traffic uses an unmethoded "/" catch-all so this
// does not conflict with method-specific host routes from HostRouteSetup (for
// example GET /agui/...). POST paths are mirrored with method-specific patterns
// more specific than the gRPC fallback (POST /), derived from the gorilla tree.
func mountGorillaOnHostMux(router *mux.Router) error {
	postPatterns, err := gorillaPostHostPatterns(router)
	if err != nil {
		return err
	}
	// Unmethoded catch-all: WebUI, redirects, DELETE /api/..., etc.
	alismux.HandleHTTP("/", router)
	for _, pattern := range postPatterns {
		alismux.HandleHTTP(pattern, router)
	}
	return nil
}

// gorillaPostHostPatterns returns POST patterns for mounting router on the host mux.
func gorillaPostHostPatterns(router *mux.Router) ([]string, error) {
	postPatterns := make(map[string]struct{})

	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if route.GetError() != nil {
			return route.GetError()
		}

		template, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}

		methods, err := route.GetMethods()
		if err != nil {
			methods = []string{
				http.MethodGet,
				http.MethodHead,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodOptions,
			}
		}

		for _, method := range methods {
			if strings.ToUpper(method) != http.MethodPost {
				continue
			}
			path := postHostPattern(template, route)
			if path == "" {
				continue
			}
			postPatterns[http.MethodPost+" "+path] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	patterns := make([]string, 0, len(postPatterns))
	for pattern := range postPatterns {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns, nil
}

// postHostPattern returns the host mux path for a gorilla POST route, or empty
// when the route must not be mirrored (for example POST /, which is reserved for
// native gRPC).
func postHostPattern(template string, route *mux.Route) string {
	template = strings.TrimSpace(template)
	if template == "" || template == "/" {
		return ""
	}
	if isGorillaPrefixRoute(route) || strings.HasSuffix(template, "/") {
		return hostPrefixPath(template)
	}
	return template
}

// isGorillaPrefixRoute reports whether route matches a path prefix subtree.
func isGorillaPrefixRoute(route *mux.Route) bool {
	if _, ok := route.GetHandler().(*mux.Router); ok {
		return true
	}
	re, err := route.GetPathRegexp()
	if err != nil {
		return false
	}
	return !strings.HasSuffix(re, "$")
}

// hostPrefixPath normalizes a gorilla prefix template for host mux registration.
func hostPrefixPath(path string) string {
	if path == "" || path == "/" {
		return ""
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}
