package web

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	iam "go.alis.build/iam/v3"
)

// webUIPath is the page ADK's webui sublauncher serves the web UI from. ADK
// fixes this prefix; it is not configurable.
const webUIPath = "/ui/"

// devUIPath is where ADK's bundled web UI expects to be served from, as
// adk-python serves it. The UI sends OAuth sign-ins back to this path and
// relies on the page loaded there, with ?code= in its URL, to hand the
// redirect URL to the window that opened the sign-in popup.
const devUIPath = "/dev-ui/"

// registerDevUIRedirect sends GET and HEAD requests under /dev-ui/ to the
// same path and query under /ui/, so an ADK web UI sign-in popup that lands
// on /dev-ui/ loads the UI and completes the sign-in (an
// adk_request_credential call, such as go.alis.build/adk/launchers/tooloauth
// sends) instead of a 404.
func registerDevUIRedirect(router *mux.Router) {
	router.Methods(http.MethodGet, http.MethodHead).PathPrefix(devUIPath).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := webUIPath + strings.TrimPrefix(r.URL.Path, devUIPath)
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// webUIUserRedirect sends a browser opening the ADK web UI page without a
// userId query to the same URL with userId set to the caller's identity ID.
//
// The web UI acts as user "user" unless the page is loaded with ?userId=, and
// it sends that ID to ADK's REST API in the path, the JSON body and the query.
// Under authz.Strict a "user" that is not the caller is refused, so instead of
// rewriting every request the page is opened as the caller, and the UI then
// sends the real ID everywhere on its own. Requests without an identity, and
// anything other than the page itself, pass through.
func webUIUserRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ADK's webui routes accept only GET and HEAD, so the method check is
		// defensive: it keeps the redirect off any other method if a route
		// under the same path ever accepts one.
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			r.URL.Path == webUIPath && !r.URL.Query().Has("userId") {
			if id, err := iam.FromContext(r.Context()); err == nil && id.ID != "" {
				q := r.URL.Query()
				q.Set("userId", id.ID)
				http.Redirect(w, r, webUIPath+"?"+q.Encode(), http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
