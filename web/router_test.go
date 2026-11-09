package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/telemetry"
)

// fakeSublauncher mounts one POST route that decodes its body as JSON, so an
// oversized body is observable as a 400, and records the launcher.Config it is
// handed so tests can see what the web launcher threads through.
type fakeSublauncher struct {
	keyword string

	// contributes is appended to config.AllowedOrigins during setup, the way
	// ADK's api sublauncher adds the web UI origin.
	contributes string

	seenOrigins        []string
	seenMaxPayloadSize int64
}

func (f *fakeSublauncher) Keyword() string                        { return f.keyword }
func (f *fakeSublauncher) Parse(args []string) ([]string, error)  { return args, nil }
func (f *fakeSublauncher) CommandLineSyntax() string              { return f.keyword }
func (f *fakeSublauncher) SimpleDescription() string              { return "fake sublauncher" }
func (f *fakeSublauncher) UserMessage(_ string, _ func(v ...any)) {}

func (f *fakeSublauncher) SetupSubrouters(router *mux.Router, config *launcher.Config) error {
	f.seenOrigins = slices.Clone(config.AllowedOrigins)
	f.seenMaxPayloadSize = config.MaxPayloadSize
	if f.contributes != "" {
		config.AllowedOrigins = append(config.AllowedOrigins, f.contributes)
	}
	router.HandleFunc("/"+f.keyword+"/echo", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("decode: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}).Methods(http.MethodPost)
	return nil
}

// newTestLauncher returns a web launcher with subs active and args parsed.
func newTestLauncher(t *testing.T, args []string, subs ...*fakeSublauncher) *webLauncher {
	t.Helper()
	sublaunchers := make([]Sublauncher, len(subs))
	for i, s := range subs {
		sublaunchers[i] = s
	}
	l := NewLauncher(sublaunchers...).(*webLauncher)
	for _, s := range subs {
		args = append(args, s.keyword)
	}
	if _, err := l.Parse(args); err != nil {
		t.Fatalf("Parse(%v) failed: %v", args, err)
	}
	return l
}

func postEcho(router http.Handler, keyword, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/"+keyword+"/echo", strings.NewReader(body))
	router.ServeHTTP(rec, req)
	return rec
}

// TestBuildRouterLimitsRequestBodies covers the -max_request_body_size limit on
// routes sublaunchers mount: removing the middleware turns the oversized
// request below into a 204, because its body is valid JSON.
func TestBuildRouterLimitsRequestBodies(t *testing.T) {
	const limit = 1024
	oversized := `{"data":"` + strings.Repeat("a", 4*limit) + `"}`

	for _, tc := range []struct {
		name   string
		args   []string
		config *launcher.Config
	}{
		{name: "from the flag", args: []string{fmt.Sprintf("-max_request_body_size=%d", limit)}, config: &launcher.Config{}},
		// The flag left at zero must not reset the base router to the 10 MiB
		// default while sublaunchers see the embedder's value.
		{name: "from the embedder", config: &launcher.Config{MaxPayloadSize: limit}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := &fakeSublauncher{keyword: "fake"}
			router, err := newTestLauncher(t, tc.args, sub).buildRouter(tc.config)
			if err != nil {
				t.Fatalf("buildRouter() failed: %v", err)
			}

			if rec := postEcho(router, "fake", `{"data":"small"}`); rec.Code != http.StatusNoContent {
				t.Errorf("small body: status = %d, want %d (%s)", rec.Code, http.StatusNoContent, rec.Body)
			}
			rec := postEcho(router, "fake", oversized)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request body too large") {
				t.Errorf("oversized body: status = %d, body %q; want %d naming the body limit", rec.Code, rec.Body, http.StatusBadRequest)
			}
		})
	}
}

// TestBuildRouterThreadsMaxPayloadSize covers the value sublaunchers see, which
// is what ADK's api sublauncher hands its REST server.
func TestBuildRouterThreadsMaxPayloadSize(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flag   int64
		caller int64
		want   int64
	}{
		{name: "flag", flag: 20 << 20, want: 20 << 20},
		{name: "neither set keeps the ADK default", want: 0},
		{name: "embedder value kept when the flag is unset", caller: 1 << 20, want: 1 << 20},
		{name: "flag wins over the embedder", flag: 20 << 20, caller: 1 << 20, want: 20 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := &fakeSublauncher{keyword: "fake"}
			args := []string{fmt.Sprintf("-max_request_body_size=%d", tc.flag)}
			if _, err := newTestLauncher(t, args, sub).buildRouter(&launcher.Config{MaxPayloadSize: tc.caller}); err != nil {
				t.Fatalf("buildRouter() failed: %v", err)
			}
			if sub.seenMaxPayloadSize != tc.want {
				t.Errorf("sublauncher saw MaxPayloadSize = %d, want %d", sub.seenMaxPayloadSize, tc.want)
			}
		})
	}
}

// TestBuildRouterAllowedOrigins covers -allow_origins reaching sublaunchers in
// order, without writing into the caller's slice.
func TestBuildRouterAllowedOrigins(t *testing.T) {
	const (
		fromCaller = "https://caller.example.com"
		fromFlag   = "https://flag.example.com"
		fromFirst  = "https://first.example.com"
	)
	// Spare capacity, so an append into the caller's array would land where
	// this test can see it.
	callerOrigins := make([]string, 1, 4)
	callerOrigins[0] = fromCaller

	first := &fakeSublauncher{keyword: "first", contributes: fromFirst}
	second := &fakeSublauncher{keyword: "second"}
	l := newTestLauncher(t, []string{"-allow_origins", fromFlag}, first, second)
	if _, err := l.buildRouter(&launcher.Config{AllowedOrigins: callerOrigins}); err != nil {
		t.Fatalf("buildRouter() failed: %v", err)
	}

	if want := []string{fromCaller, fromFlag}; !slices.Equal(first.seenOrigins, want) {
		t.Errorf("first sublauncher saw AllowedOrigins = %q, want %q", first.seenOrigins, want)
	}
	if want := []string{fromCaller, fromFlag, fromFirst}; !slices.Equal(second.seenOrigins, want) {
		t.Errorf("second sublauncher saw AllowedOrigins = %q, want %q", second.seenOrigins, want)
	}
	if spare := callerOrigins[1:cap(callerOrigins)]; slices.ContainsFunc(spare, func(s string) bool { return s != "" }) {
		t.Errorf("buildRouter wrote %q into the spare capacity of the caller's AllowedOrigins", spare)
	}
}

// TestBuildRouterServesHealth covers the root /health route probes rely on.
func TestBuildRouterServesHealth(t *testing.T) {
	router, err := newTestLauncher(t, nil, &fakeSublauncher{keyword: "fake"}).buildRouter(&launcher.Config{})
	if err != nil {
		t.Fatalf("buildRouter() failed: %v", err)
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "http://localhost/health", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s /health: status = %d, want %d", method, rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s /health: Content-Type = %q, want application/json", method, got)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost/health", nil))
	if got, want := rec.Body.String(), `{"status":"ok"}`+"\n"; got != want {
		t.Errorf("GET /health body = %q, want %q", got, want)
	}
}

// TestBuildRouterNeedsASublauncher keeps the error that lists the choices.
func TestBuildRouterNeedsASublauncher(t *testing.T) {
	l := NewLauncher(&fakeSublauncher{keyword: "fake"}).(*webLauncher)
	if _, err := l.Parse(nil); err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	_, err := l.buildRouter(&launcher.Config{})
	if err == nil || !strings.Contains(err.Error(), "fake") {
		t.Errorf("buildRouter() error = %v, want the missing-sublauncher error listing %q", err, "fake")
	}
}

func TestApplyServiceDefaults(t *testing.T) {
	t.Run("fills session and memory but never artifact", func(t *testing.T) {
		config := &launcher.Config{}
		applyServiceDefaults(config)

		if config.SessionService == nil {
			t.Error("SessionService is nil, want an in-memory default")
		}
		if config.MemoryService == nil {
			t.Error("MemoryService is nil, want an in-memory default: ADK's REST handlers panic without one")
		}
		// An in-memory artifact service on Cloud Run loses saved files on
		// restart and across instances; a missing one fails loudly instead.
		if config.ArtifactService != nil {
			t.Errorf("ArtifactService = %T, want nil left for the deployment to configure", config.ArtifactService)
		}
	})

	t.Run("keeps supplied services", func(t *testing.T) {
		sessions := session.InMemoryService()
		memories := memory.InMemoryService()
		artifacts := artifact.InMemoryService()
		config := &launcher.Config{SessionService: sessions, MemoryService: memories, ArtifactService: artifacts}
		applyServiceDefaults(config)

		if config.SessionService != sessions || config.MemoryService != memories || config.ArtifactService != artifacts {
			t.Error("applyServiceDefaults replaced a service the caller supplied")
		}
	})
}

// TestTelemetryOptions covers the -otel_to_cloud flag no longer overriding an
// embedder's telemetry.WithOtelToCloud(true) when the flag is left unset.
func TestTelemetryOptions(t *testing.T) {
	// Spare capacity, so an append into the caller's array would land where
	// this test can see it.
	callerOpts := make([]telemetry.Option, 1, 4)
	callerOpts[0] = telemetry.WithOtelToCloud(true)
	config := &launcher.Config{TelemetryOptions: callerOpts}

	if got := telemetryOptions(config, false); len(got) != 1 {
		t.Errorf("flag unset: got %d options, want only the embedder's 1, so its WithOtelToCloud(true) stands", len(got))
	}
	if got := telemetryOptions(config, true); len(got) != 2 {
		t.Errorf("flag set: got %d options, want the embedder's 1 plus WithOtelToCloud(true)", len(got))
	}
	if spare := callerOpts[1:cap(callerOpts)]; slices.ContainsFunc(spare, func(o telemetry.Option) bool { return o != nil }) {
		t.Error("telemetryOptions wrote into the spare capacity of the caller's TelemetryOptions")
	}
}

// Compile-time check that the fake satisfies the contract the launcher takes.
var _ Sublauncher = (*fakeSublauncher)(nil)
