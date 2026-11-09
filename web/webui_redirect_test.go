package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	iam "go.alis.build/iam/v3"
	"google.golang.org/adk/v2/cmd/launcher"
)

// fakeWebUISublauncher serves the web UI prefix the way ADK's webui
// sublauncher does: a GET/HEAD PathPrefix subrouter under /ui/.
type fakeWebUISublauncher struct{}

func (fakeWebUISublauncher) Keyword() string                        { return "webui" }
func (fakeWebUISublauncher) Parse(args []string) ([]string, error)  { return args, nil }
func (fakeWebUISublauncher) CommandLineSyntax() string              { return "webui" }
func (fakeWebUISublauncher) SimpleDescription() string              { return "fake web UI" }
func (fakeWebUISublauncher) UserMessage(_ string, _ func(v ...any)) {}

func (fakeWebUISublauncher) SetupSubrouters(router *mux.Router, _ *launcher.Config) error {
	ui := router.Methods(http.MethodGet, http.MethodHead).PathPrefix("/ui/").Subrouter()
	ui.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return nil
}

func TestWebUIUserRedirect(t *testing.T) {
	l := NewLauncher(fakeWebUISublauncher{}).(*webLauncher)
	if _, err := l.Parse([]string{"webui"}); err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	router, err := l.buildRouter(&launcher.Config{})
	if err != nil {
		t.Fatalf("buildRouter() failed: %v", err)
	}

	for _, tc := range []struct {
		name      string
		method    string
		target    string
		identity  string
		wantCode  int
		wantQuery url.Values // checked only for a redirect
	}{
		{name: "page without userId", method: http.MethodGet, target: "/ui/", identity: "123", wantCode: http.StatusFound, wantQuery: url.Values{"userId": {"123"}}},
		{name: "other query kept", method: http.MethodGet, target: "/ui/?app=x", identity: "123", wantCode: http.StatusFound, wantQuery: url.Values{"app": {"x"}, "userId": {"123"}}},
		{name: "HEAD redirected too", method: http.MethodHead, target: "/ui/", identity: "123", wantCode: http.StatusFound, wantQuery: url.Values{"userId": {"123"}}},
		{name: "userId already set", method: http.MethodGet, target: "/ui/?userId=9", identity: "123", wantCode: http.StatusOK},
		{name: "anonymous", method: http.MethodGet, target: "/ui/", wantCode: http.StatusOK},
		{name: "asset", method: http.MethodGet, target: "/ui/assets/x.js", identity: "123", wantCode: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://localhost"+tc.target, nil)
			if tc.identity != "" {
				req = req.WithContext((&iam.Identity{ID: tc.identity, Type: iam.User}).Context(req.Context()))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.target, rec.Code, tc.wantCode)
			}
			if tc.wantCode != http.StatusFound {
				return
			}
			loc, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatalf("Location %q does not parse: %v", rec.Header().Get("Location"), err)
			}
			if loc.Path != "/ui/" {
				t.Errorf("Location path = %q, want /ui/", loc.Path)
			}
			if got := loc.Query(); got.Encode() != tc.wantQuery.Encode() {
				t.Errorf("Location query = %q, want %q", got.Encode(), tc.wantQuery.Encode())
			}
		})
	}
}
