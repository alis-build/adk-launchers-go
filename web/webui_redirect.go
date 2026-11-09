package web

import (
	"net/http"

	iam "go.alis.build/iam/v3"
)

// webUIPath is the page ADK's webui sublauncher serves the web UI from. ADK
// fixes this prefix; it is not configurable.
const webUIPath = "/ui/"

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
			if id, err := iam.FromContext(r.Context()); err == nil {
				q := r.URL.Query()
				q.Set("userId", id.ID)
				http.Redirect(w, r, webUIPath+"?"+q.Encode(), http.StatusFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
