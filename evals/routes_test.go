package evals

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/storage"

	alismux "go.alis.build/mux"
)

// TestEveryEvalRouteRequiresIdentity mounts the evals routes on the
// process-wide host mux, once per test binary, and sends an anonymous request
// to each one. Every route must refuse it before its handler runs.
// routeTest is the launcher whose routes are on the process-wide host mux.
// The mux panics on a duplicate pattern, so it is mounted once per test
// binary, which also lets the test run under -count=N.
var routeTest struct {
	once sync.Once
	l    *evalsLauncher
	sets storage.EvalSetsManager
	err  error
}

func TestEveryEvalRouteRequiresIdentity(t *testing.T) {
	routeTest.once.Do(func() {
		routeTest.l, routeTest.sets = newHandlerTestLauncher(t)
		routeTest.err = routeTest.l.mountHostRoutes(routeTest.l.launcherCfg)
	})
	if routeTest.err != nil {
		t.Fatalf("mountHostRoutes() failed: %v", routeTest.err)
	}
	l, sets := routeTest.l, routeTest.sets

	routes := l.routes()
	// 13 hyphen routes on the dev base, plus the 12 legacy underscore routes
	// mounted on both the dev base and the bundled web UI's base.
	if got, want := len(routes), 13+12*2; got != want {
		t.Errorf("len(routes()) = %d, want %d", got, want)
	}

	fill := strings.NewReplacer(
		"{app_name}", "my_app",
		"{eval_set_id}", "set1",
		"{eval_case_id}", "case1",
		"{eval_result_id}", "result1",
	)
	for _, r := range routes {
		t.Run(r.method+" "+r.pattern, func(t *testing.T) {
			req := httptest.NewRequest(r.method, "http://localhost"+fill.Replace(r.pattern), strings.NewReader(`{"eval_set":{"eval_set_id":"set1"}}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			alismux.HTTPHandler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("anonymous %s %s = %d, want %d", r.method, req.URL.Path, rec.Code, http.StatusUnauthorized)
			}
		})
	}

	// The create routes above carried a valid body; none may have run. A
	// missing app directory means nothing was ever written for it.
	ids, err := sets.ListEvalSets("my_app")
	if err != nil && !storageNotFound(err) {
		t.Fatalf("ListEvalSets() failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("eval sets after anonymous requests = %v, want none", ids)
	}
}
