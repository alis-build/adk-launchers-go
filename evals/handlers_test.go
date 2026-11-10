package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	iam "go.alis.build/iam/v3"
	alismux "go.alis.build/mux"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func newHandlerTestLauncher(t *testing.T) (*evalsLauncher, storage.EvalSetsManager) {
	t.Helper()
	dir := t.TempDir()
	sets := storage.NewLocalEvalSetsManager(dir)
	results := storage.NewLocalEvalSetResultsManager(dir)
	a, err := agent.New(agent.Config{
		Name: "my_app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "agent"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	l := NewLauncher(
		WithAgentsDir(dir),
		WithEvalSetsManager(sets),
		WithEvalSetResultsManager(results),
	).(*evalsLauncher)
	l.launcherCfg = &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}
	l.setsManager = sets
	l.resultsManager = results
	return l, sets
}

func callHandler(t *testing.T, fn func(http.ResponseWriter, *http.Request) error, method, path string, body any, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	if err := fn(rec, req); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

// callHandlerAs runs h as the identity with id ("" for anonymous) and returns
// the recorder and the handler's error without failing the test.
func callHandlerAs(t *testing.T, h alismux.Func, method, target, id string, body any, pathValues map[string]string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	if id != "" {
		req = req.WithContext((&iam.Identity{ID: id, Type: iam.User}).Context(req.Context()))
	}
	rec := httptest.NewRecorder()
	return rec, h(rec, req)
}

// seedSession creates session sessionID for user with one user and one agent
// turn, so add-session has a conversation to copy.
func seedSession(t *testing.T, l *evalsLauncher, user, sessionID string) {
	t.Helper()
	ctx := context.Background()
	svc := l.launcherCfg.SessionService
	created, err := svc.Create(ctx, &session.CreateRequest{AppName: "my_app", UserID: user, SessionID: sessionID})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	ev := session.NewEvent(ctx, "inv1")
	ev.Author = "user"
	ev.Content = genai.NewContentFromText("hello", genai.RoleUser)
	if err := svc.AppendEvent(ctx, created.Session, ev); err != nil {
		t.Fatalf("AppendEvent(user): %v", err)
	}
	agentEv := session.NewEvent(ctx, "inv1")
	agentEv.Author = "agent"
	agentEv.Content = genai.NewContentFromText("hi", genai.RoleModel)
	if err := svc.AppendEvent(ctx, created.Session, agentEv); err != nil {
		t.Fatalf("AppendEvent(agent): %v", err)
	}
}

func TestCreateAndListEvalSets(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)

	rec := callHandler(t, l.createEvalSetHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets", map[string]any{
		"eval_set": map[string]any{
			"eval_set_id":          "set_a",
			"model_execution_mode": "live",
			"tool_execution_mode":  "live",
			"eval_cases":           []any{},
		},
	}, map[string]string{"app_name": "my_app"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created models.EvalSet
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.EvalSetID != "set_a" {
		t.Fatalf("eval_set_id = %q", created.EvalSetID)
	}
	if created.ModelExecutionMode == nil || *created.ModelExecutionMode != "live" {
		t.Fatalf("model_execution_mode = %v", created.ModelExecutionMode)
	}

	rec = callHandler(t, l.listEvalSetsHandler(), http.MethodGet, "/dev/apps/my_app/eval-sets", nil, map[string]string{"app_name": "my_app"})
	var resp ListEvalSetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.EvalSetIDs) != 1 || resp.EvalSetIDs[0] != "set_a" {
		t.Fatalf("eval set ids = %v", resp.EvalSetIDs)
	}

	rec = callHandler(t, l.listEvalSetsLegacyHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets", nil, map[string]string{"app_name": "my_app"})
	var legacy []string
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if len(legacy) != 1 || legacy[0] != "set_a" {
		t.Fatalf("legacy ids = %v", legacy)
	}
}

func TestEvalCaseCRUD(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	case1 := models.EvalCase{
		EvalID:       "case1",
		Conversation: []models.Invocation{},
	}
	_ = sets.AddEvalCase("my_app", "set1", case1)

	pathValues := map[string]string{
		"app_name":     "my_app",
		"eval_set_id":  "set1",
		"eval_case_id": "case1",
	}

	rec := callHandler(t, l.getEvalCaseHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets/set1/evals/case1", nil, pathValues)
	if rec.Code != http.StatusOK {
		t.Fatalf("get case status = %d", rec.Code)
	}

	rec = callHandler(t, l.listEvalCasesHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets/set1/evals", nil, map[string]string{
		"app_name": "my_app", "eval_set_id": "set1",
	})
	var ids []string
	_ = json.Unmarshal(rec.Body.Bytes(), &ids)
	if len(ids) != 1 || ids[0] != "case1" {
		t.Fatalf("case ids = %v", ids)
	}

	rec = callHandler(t, l.deleteEvalCaseHandler(), http.MethodDelete, "/dev/apps/my_app/eval_sets/set1/evals/case1", nil, pathValues)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
}

func TestAddSessionToEvalSet(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	seedSession(t, l, "user1", "sess1")

	rec, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/add_session", "user1", map[string]string{
		"evalId": "from_session", "sessionId": "sess1", "userId": "user1",
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("add session: status = %d, err = %v, body = %s", rec.Code, err, rec.Body.String())
	}

	got, err := sets.GetEvalCase("my_app", "set1", "from_session")
	if err != nil || got == nil || len(got.Conversation) == 0 {
		t.Fatalf("eval case = %+v err = %v", got, err)
	}
	if got.SessionInput == nil || got.SessionInput.UserID != "user1" {
		t.Errorf("stored session_input.user_id = %+v, want user1", got.SessionInput)
	}
}

func TestAddSessionRefusesOtherUser(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	seedSession(t, l, "user1", "sess1")

	_, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/add_session", "user2", map[string]string{
		"evalId": "copied", "sessionId": "sess1", "userId": "user1",
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if got := alismux.StatusCode(err); got != http.StatusForbidden {
		t.Fatalf("add session as user2 for user1: status = %d (err %v), want %d", got, err, http.StatusForbidden)
	}
	if c, _ := sets.GetEvalCase("my_app", "set1", "copied"); c != nil {
		t.Errorf("eval case written for a refused request: %+v", c)
	}
}

// TestAddSessionRefusesIdentityWithoutID covers the handler's own check, which
// backs up the route middleware: an empty ID would read user "" sessions.
func TestAddSessionRefusesIdentityWithoutID(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")

	req := httptest.NewRequest(http.MethodPost, "/dev/apps/my_app/eval_sets/set1/add_session", strings.NewReader(`{"evalId":"blank","sessionId":"sess1"}`))
	req.SetPathValue("app_name", "my_app")
	req.SetPathValue("eval_set_id", "set1")
	req = req.WithContext((&iam.Identity{Email: "a@x.com", Type: iam.User}).Context(req.Context()))
	err := l.addSessionHandler()(httptest.NewRecorder(), req)
	if got := alismux.StatusCode(err); got != http.StatusUnauthorized {
		t.Fatalf("add session with an empty identity ID: status = %d (err %v), want %d", got, err, http.StatusUnauthorized)
	}
	if c, _ := sets.GetEvalCase("my_app", "set1", "blank"); c != nil {
		t.Errorf("eval case written for a refused request: %+v", c)
	}
}

func TestAddSessionDefaultsToCaller(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	seedSession(t, l, "user1", "sess1")

	rec, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/add_session", "user1", map[string]string{
		"evalId": "no_user", "sessionId": "sess1",
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("add session without userId: status = %d, err = %v", rec.Code, err)
	}
	got, err := sets.GetEvalCase("my_app", "set1", "no_user")
	if err != nil || got == nil || got.SessionInput == nil || got.SessionInput.UserID != "user1" {
		t.Errorf("stored case = %+v err = %v, want session_input.user_id user1", got, err)
	}
}

// TestMetricsInfoMatchesAdkPython compares every metric Go registers with
// adk-python's captured metrics-info entry (testdata/adkpy/metrics_info.json),
// field for field.
func TestMetricsInfoMatchesAdkPython(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	rec, err := callHandlerAs(t, l.metricsInfoHandler(), http.MethodGet, "/dev/apps/my_app/metrics-info", "user1", nil, map[string]string{"app_name": "my_app"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("metrics-info: status = %d, err = %v", rec.Code, err)
	}
	byName := func(t *testing.T, raw []byte, where string) map[string]any {
		t.Helper()
		var resp struct {
			MetricsInfo []map[string]any `json:"metricsInfo"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("decode %s: %v", where, err)
		}
		out := make(map[string]any, len(resp.MetricsInfo))
		for _, m := range resp.MetricsInfo {
			out[m["metricName"].(string)] = m
		}
		return out
	}
	captured, err := os.ReadFile(filepath.Join("testdata", "adkpy", "metrics_info.json"))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	want := byName(t, captured, "capture")
	got := byName(t, rec.Body.Bytes(), "Go response")
	if len(got) < 13 {
		t.Fatalf("Go registers %d metrics, want at least 13", len(got))
	}
	for name, g := range got {
		w, ok := want[name]
		if !ok {
			t.Errorf("metric %q is not in adk-python's metrics-info", name)
			continue
		}
		if diff := cmp.Diff(w, g); diff != "" {
			t.Errorf("metric %q differs from adk-python (-adk-python +go):\n%s", name, diff)
		}
	}
}

func TestRunEvalDeterministicMetric(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})

	rec, err := callHandlerAs(t, l.runEvalLegacyHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/run_eval", "user1", map[string]any{
		"evalMetrics": []map[string]any{{
			"metricName": models.MetricResponseMatchScore,
			"threshold":  0.5,
		}},
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run eval: status = %d, err = %v, body = %s", rec.Code, err, rec.Body.String())
	}
	var results []models.RunEvalResult
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("results = %+v", results)
	}
}

// TestRunEvalRunsAsCaller checks that a run executes as the caller on the
// route's app, not as the user and app stored on the case.
func TestRunEvalRunsAsCaller(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID:       "case1",
		SessionInput: &models.SessionInput{UserID: "someone", AppName: "other"},
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})

	rec, err := callHandlerAs(t, l.runEvalLegacyHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/run_eval", "caller", map[string]any{
		"evalMetrics": []map[string]any{{"metricName": models.MetricResponseMatchScore, "threshold": 0.5}},
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run eval: status = %d, err = %v, body = %s", rec.Code, err, rec.Body.String())
	}

	ids, err := l.resultsManager.ListEvalSetResults("my_app")
	if err != nil || len(ids) != 1 {
		t.Fatalf("ListEvalSetResults = %v, %v; want one result", ids, err)
	}
	res, err := l.resultsManager.GetEvalSetResult("my_app", ids[0])
	if err != nil || res == nil || len(res.EvalCaseResults) != 1 {
		t.Fatalf("GetEvalSetResult = %+v, %v", res, err)
	}
	got := res.EvalCaseResults[0]
	if got.UserID == nil || *got.UserID != "caller" {
		gotUser := "<nil>"
		if got.UserID != nil {
			gotUser = *got.UserID
		}
		t.Errorf("evalCaseResults[0].userId = %q, want caller", gotUser)
	}
	if len(got.SessionDetails) == 0 {
		t.Error("evalCaseResults[0].sessionDetails is empty, want the caller's inference session")
	}
}

func TestGetEvalSet(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")

	rec := callHandler(t, l.getEvalSetHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets/set1", nil, map[string]string{
		"app_name": "my_app", "eval_set_id": "set1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("get eval set status = %d", rec.Code)
	}
}

func TestCreateEvalSetLegacyWebuiPath(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)

	// Bundled adk-web POSTs to /api/apps/{app}/eval_sets/{id} (no /dev segment).
	rec := callHandler(t, l.createEvalSetLegacyHandler(), http.MethodPost, "/apps/my_app/eval_sets/webui_set", nil, map[string]string{
		"app_name": "my_app", "eval_set_id": "webui_set",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}

	got, err := sets.GetEvalSet("my_app", "webui_set")
	if err != nil || got == nil {
		t.Fatalf("GetEvalSet: err = %v got = %+v", err, got)
	}
}

func TestCreateEvalSetPersistsMetadataRoundTrip(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)

	rec := callHandler(t, l.createEvalSetHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets", map[string]any{
		"eval_set": map[string]any{
			"eval_set_id":          "set_a",
			"model_execution_mode": "live",
			"tool_execution_mode":  "live",
			"description":          "test description",
			"eval_cases":           []any{},
		},
	}, map[string]string{"app_name": "my_app"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}

	stored, err := sets.GetEvalSet("my_app", "set_a")
	if err != nil {
		t.Fatalf("GetEvalSet: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored eval set")
	}
	if stored.ModelExecutionMode == nil || *stored.ModelExecutionMode != "live" {
		t.Fatalf("stored model_execution_mode = %v", stored.ModelExecutionMode)
	}
	if stored.ToolExecutionMode == nil || *stored.ToolExecutionMode != "live" {
		t.Fatalf("stored tool_execution_mode = %v", stored.ToolExecutionMode)
	}
	if stored.Description == nil || *stored.Description != "test description" {
		t.Fatalf("stored description = %v", stored.Description)
	}
}

func TestGetEvalResultRejectsPathTraversal(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	handler := l.getEvalResultHandler()
	for _, id := range []string{"../foo", "a/b", "..", ".", "with\\backslash"} {
		req := httptest.NewRequest(http.MethodGet, "/dev/apps/my_app/eval-results/"+id, nil)
		req.SetPathValue("app_name", "my_app")
		req.SetPathValue("eval_result_id", id)
		rec := httptest.NewRecorder()
		if err := handler(rec, req); err == nil && rec.Code == http.StatusOK {
			t.Fatalf("expected error for id=%q, got 200", id)
		}
	}
}

func TestBucketFromURI(t *testing.T) {
	tests := []struct {
		uri     string
		want    string
		wantErr bool
	}{
		{"gs://my-bucket", "my-bucket", false},
		{"gs://my-bucket/prefix", "my-bucket", false},
		{"s3://my-bucket", "", true},
		{"http://example.com", "", true},
		{"my-bucket", "", true},
		{"gs://", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := bucketFromURI(tt.uri)
		if tt.wantErr {
			if err == nil {
				t.Errorf("bucketFromURI(%q) = %q, want error", tt.uri, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("bucketFromURI(%q) error: %v", tt.uri, err)
			continue
		}
		if got != tt.want {
			t.Errorf("bucketFromURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestDeleteEvalSet(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")

	rec := callHandler(t, l.deleteEvalSetHandler(), http.MethodDelete, "/dev/apps/my_app/eval_sets/set1", nil, map[string]string{
		"app_name": "my_app", "eval_set_id": "set1",
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete eval set status = %d", rec.Code)
	}
	got, _ := sets.GetEvalSet("my_app", "set1")
	if got != nil {
		t.Fatal("expected eval set to be deleted")
	}
}

func assertLegacyListJSONEmptyArray(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", rec.Body.String())
	}
}

func TestLegacyListEvalResultsEmptyReturnsJSONArray(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	rec := callHandler(t, l.listEvalResultsLegacyHandler(), http.MethodGet, "/dev/apps/my_app/eval_results", nil, map[string]string{
		"app_name": "my_app",
	})
	assertLegacyListJSONEmptyArray(t, rec)
}

func TestLegacyListEvalResultsNilSliceReturnsJSONArray(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	l.resultsManager = nilSliceResultsManager{}
	rec := callHandler(t, l.listEvalResultsLegacyHandler(), http.MethodGet, "/dev/apps/my_app/eval_results", nil, map[string]string{
		"app_name": "my_app",
	})
	assertLegacyListJSONEmptyArray(t, rec)
}

func TestLegacyListEvalSetsEmptyReturnsJSONArray(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "my_app"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sets := storage.NewLocalEvalSetsManager(dir)
	results := storage.NewLocalEvalSetResultsManager(dir)
	l := NewLauncher(
		WithAgentsDir(dir),
		WithEvalSetsManager(sets),
		WithEvalSetResultsManager(results),
	).(*evalsLauncher)
	l.setsManager = sets
	l.resultsManager = results

	rec := callHandler(t, l.listEvalSetsLegacyHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets", nil, map[string]string{
		"app_name": "my_app",
	})
	assertLegacyListJSONEmptyArray(t, rec)
}

func TestLegacyListEvalCasesEmptyReturnsJSONArray(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")

	rec := callHandler(t, l.listEvalCasesHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets/set1/evals", nil, map[string]string{
		"app_name": "my_app", "eval_set_id": "set1",
	})
	assertLegacyListJSONEmptyArray(t, rec)
}

func TestListEvalResultsNilSliceReturnsJSONArray(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	l.resultsManager = nilSliceResultsManager{}
	rec := callHandler(t, l.listEvalResultsHandler(), http.MethodGet, "/dev/apps/my_app/eval-results", nil, map[string]string{
		"app_name": "my_app",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["evalResultIds"].([]any); !ok {
		t.Fatalf("evalResultIds = %v (%T), want array", m["evalResultIds"], m["evalResultIds"])
	}
}

// TestGetEvalResultNormalizesLegacyIntermediateData reproduces the exact
// property chain the webui dereferences without optional chaining
// (eval-tab: actualInvocation.intermediateData.toolUses) against a stored
// result file containing "intermediateData": null.
func TestGetEvalResultNormalizesLegacyIntermediateData(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	dir := t.TempDir()
	l.resultsManager = storage.NewLocalEvalSetResultsManager(dir)

	histDir := filepath.Join(dir, "my_app", ".adk", "eval_history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fixture := `{
		"evalSetResultId": "my_app_set_a_res1",
		"evalSetId": "set_a",
		"evalCaseResults": [{
			"evalSetId": "set_a",
			"evalId": "case_1",
			"finalEvalStatus": 2,
			"overallEvalMetricResults": [],
			"evalMetricResultPerInvocation": [{
				"actualInvocation": {"userContent": {"parts": [{"text": "q"}]}, "intermediateData": null},
				"expectedInvocation": {"userContent": {"parts": [{"text": "q"}]}},
				"evalMetricResults": []
			}],
			"sessionId": "s1"
		}]
	}`
	path := filepath.Join(histDir, "my_app_set_a_res1.evalset_result.json")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := callHandler(t, l.getEvalResultHandler(), http.MethodGet, "/dev/apps/my_app/eval-results/my_app_set_a_res1", nil, map[string]string{
		"app_name": "my_app", "eval_result_id": "my_app_set_a_res1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	caseResults, ok := m["evalCaseResults"].([]any)
	if !ok || len(caseResults) != 1 {
		t.Fatalf("evalCaseResults = %v", m["evalCaseResults"])
	}
	perInv := caseResults[0].(map[string]any)["evalMetricResultPerInvocation"].([]any)
	for _, key := range []string{"actualInvocation", "expectedInvocation"} {
		inv, ok := perInv[0].(map[string]any)[key].(map[string]any)
		if !ok {
			t.Fatalf("%s = %v", key, perInv[0])
		}
		data, ok := inv["intermediateData"].(map[string]any)
		if !ok {
			t.Fatalf("%s.intermediateData = %v (%T), want object", key, inv["intermediateData"], inv["intermediateData"])
		}
		if _, ok := data["toolUses"].([]any); !ok {
			t.Fatalf("%s.intermediateData.toolUses = %v (%T), want array", key, data["toolUses"], data["toolUses"])
		}
	}
}

// TestGetEvalResultDefaultsMissingCaseResults covers foreign result files
// written without an evalCaseResults key; the webui maps over the field
// unguarded so it must serialize as [] rather than null.
func TestGetEvalResultDefaultsMissingCaseResults(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	dir := t.TempDir()
	l.resultsManager = storage.NewLocalEvalSetResultsManager(dir)

	histDir := filepath.Join(dir, "my_app", ".adk", "eval_history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fixture := `{"evalSetResultId": "my_app_set_a_res2", "evalSetId": "set_a"}`
	path := filepath.Join(histDir, "my_app_set_a_res2.evalset_result.json")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := callHandler(t, l.getEvalResultHandler(), http.MethodGet, "/dev/apps/my_app/eval-results/my_app_set_a_res2", nil, map[string]string{
		"app_name": "my_app", "eval_result_id": "my_app_set_a_res2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["evalCaseResults"].([]any); !ok {
		t.Fatalf("evalCaseResults = %v (%T), want array", m["evalCaseResults"], m["evalCaseResults"])
	}
}

// nilSliceResultsManager simulates GCS ListEvalSetResults returning a nil slice on success.
type nilSliceResultsManager struct{}

func (nilSliceResultsManager) SaveEvalSetResult(string, string, []models.EvalCaseResult) (*models.EvalSetResult, error) {
	return nil, nil
}

func (nilSliceResultsManager) GetEvalSetResult(string, string) (*models.EvalSetResult, error) {
	return nil, nil
}

func (nilSliceResultsManager) ListEvalSetResults(string) ([]string, error) {
	return nil, nil
}

// credentialCall is an adk_request_credential call whose auth config carries
// an OAuth2 client secret and PKCE verifier, as in adk-python's
// oauth2_auth_config_dict fixture.
func credentialCall() *genai.FunctionCall {
	return &genai.FunctionCall{
		ID:   "fc-1",
		Name: "adk_request_credential",
		Args: map[string]any{
			"functionCallId": "call-1",
			"authConfig": map[string]any{
				"exchangedAuthCredential": map[string]any{
					"authType": "oauth2",
					"oauth2": map[string]any{
						"clientId":     "public-client-id",
						"clientSecret": "s3cret-client",
						"codeVerifier": "s3cret-verifier",
					},
				},
			},
		},
	}
}

func TestGetEvalCaseRedactsSecrets(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
			IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{credentialCall()}}),
		}},
	})

	rec, err := callHandlerAs(t, l.getEvalCaseHandler(), http.MethodGet, "/dev/apps/my_app/eval_sets/set1/evals/case1", "user1", nil,
		map[string]string{"app_name": "my_app", "eval_set_id": "set1", "eval_case_id": "case1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get eval case: status = %d, err = %v", rec.Code, err)
	}
	body := rec.Body.String()
	for _, secret := range []string{"s3cret-client", "s3cret-verifier"} {
		if strings.Contains(body, secret) {
			t.Errorf("response contains %q", secret)
		}
	}
	for _, kept := range []string{"adk_request_credential", "public-client-id"} {
		if !strings.Contains(body, kept) {
			t.Errorf("response lost %q: %s", kept, body)
		}
	}

	stored, err := sets.GetEvalCase("my_app", "set1", "case1")
	if err != nil || stored == nil {
		t.Fatalf("GetEvalCase: %v", err)
	}
	raw, _ := json.Marshal(stored)
	if !strings.Contains(string(raw), "s3cret-client") {
		t.Error("stored case lost its secret; redaction must only touch the response")
	}
}

func TestGetEvalResultRedactsSecrets(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	details, _ := json.Marshal(map[string]any{"events": []any{map[string]any{
		"content": map[string]any{"parts": []any{map[string]any{"functionCall": credentialCall()}}},
	}}})
	saved, err := l.resultsManager.SaveEvalSetResult("my_app", "set1", []models.EvalCaseResult{{
		EvalSetID: "set1", EvalID: "case1", SessionDetails: details,
	}})
	if err != nil {
		t.Fatalf("SaveEvalSetResult: %v", err)
	}

	rec, err := callHandlerAs(t, l.getEvalResultHandler(), http.MethodGet, "/dev/apps/my_app/eval_results/"+saved.EvalSetResultID, "user1", nil,
		map[string]string{"app_name": "my_app", "eval_result_id": saved.EvalSetResultID})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get eval result: status = %d, err = %v", rec.Code, err)
	}
	body := rec.Body.String()
	for _, secret := range []string{"s3cret-client", "s3cret-verifier"} {
		if strings.Contains(body, secret) {
			t.Errorf("response contains %q", secret)
		}
	}
	for _, kept := range []string{"adk_request_credential", "public-client-id"} {
		if !strings.Contains(body, kept) {
			t.Errorf("response lost %q: %s", kept, body)
		}
	}

	stored, err := l.resultsManager.GetEvalSetResult("my_app", saved.EvalSetResultID)
	if err != nil || stored == nil {
		t.Fatalf("GetEvalSetResult: %v", err)
	}
	raw, _ := json.Marshal(stored)
	if !strings.Contains(string(raw), "s3cret-client") {
		t.Error("stored result lost its secret; redaction must only touch the response")
	}
}

func TestGetEvalSetRedactsSecrets(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
			IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{credentialCall()}}),
		}},
	})

	rec, err := callHandlerAs(t, l.getEvalSetHandler(), http.MethodGet, "/dev/apps/my_app/eval-sets/set1", "user1", nil,
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get eval set: status = %d, err = %v", rec.Code, err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "s3cret-client") {
		t.Error("response contains the client secret")
	}
	if !strings.Contains(body, "adk_request_credential") {
		t.Errorf("response lost the credential call: %s", body)
	}
}

// runSetWithOneCase stores set1 with case1, a one-turn case the echo agent passes.
func runSetWithOneCase(t *testing.T) (*evalsLauncher, storage.EvalSetsManager) {
	t.Helper()
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})
	return l, sets
}

func assertNoResults(t *testing.T, l *evalsLauncher) {
	t.Helper()
	ids, err := l.resultsManager.ListEvalSetResults("my_app")
	if err != nil && !storageNotFound(err) {
		t.Fatalf("ListEvalSetResults: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("eval results after a refused run = %v, want none", ids)
	}
}

func TestRunRejectsLiveModelConfig(t *testing.T) {
	l, _ := runSetWithOneCase(t)
	_, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
		json.RawMessage(`{"eval_case_ids": ["case1"], "eval_metrics": [{"metricName": "response_match_score", "threshold": 0.7}], "live_model_config": {}}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if alismux.StatusCode(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "live_model_config is not supported yet") {
		t.Errorf("run with live_model_config: err = %v, want 400 naming live_model_config", err)
	}
	assertNoResults(t, l)
}

func TestRunRejectsUserSimulatorConfig(t *testing.T) {
	l, _ := runSetWithOneCase(t)
	_, err := callHandlerAs(t, l.runEvalLegacyHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/run_eval", "user1",
		json.RawMessage(`{"evalMetrics": [{"metricName": "response_match_score", "threshold": 0.7}], "userSimulatorConfig": {"type": "llm_audio"}}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if alismux.StatusCode(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "user_simulator_config is not supported yet") {
		t.Errorf("run with userSimulatorConfig: err = %v, want 400 naming user_simulator_config", err)
	}
	assertNoResults(t, l)
}

func TestRunAcceptsNullLiveModelConfig(t *testing.T) {
	l, _ := runSetWithOneCase(t)
	rec, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
		json.RawMessage(`{"eval_case_ids": ["case1"], "eval_metrics": [{"metricName": "response_match_score", "threshold": 0.7}], "live_model_config": null}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run with null live_model_config: status = %d, err = %v", rec.Code, err)
	}
}

// keysOf returns the sorted keys of a decoded JSON object.
func keysOf(t *testing.T, v any, where string) []string {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want a JSON object", where, v)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// metricNamed returns the metric result named name from a decoded list.
func metricNamed(t *testing.T, list any, name, where string) any {
	t.Helper()
	items, _ := list.([]any)
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["metricName"] == name {
			return m
		}
	}
	t.Fatalf("%s has no %q metric", where, name)
	return nil
}

// TestRunAdkWebMainBodyMatchesAdkPythonShape posts adk-web main's run body and
// compares the response's keys with adk-python's captured response
// (testdata/adkpy/run_eval_response.json) at the levels adk-web reads.
func TestRunAdkWebMainBodyMatchesAdkPythonShape(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	for _, id := range []string{"case1", "case2"} {
		_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
			EvalID: id,
			Conversation: []models.Invocation{{
				UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
				FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
			}},
		})
	}
	rec, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
		json.RawMessage(`{"eval_case_ids": ["case2"], "eval_metrics": [{"metricName": "response_match_score", "threshold": 0.7}]}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run: status = %d, err = %v", rec.Code, err)
	}

	var got, want map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode Go response: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "adkpy", "run_eval_response.json"))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode capture: %v", err)
	}

	gotRes, _ := got["runEvalResults"].([]any)
	if len(gotRes) != 1 {
		t.Fatalf("runEvalResults = %v, want one result", got["runEvalResults"])
	}
	g0 := gotRes[0].(map[string]any)
	if g0["evalId"] != "case2" || g0["finalEvalStatus"] != float64(models.EvalStatusPassed) {
		t.Errorf("result evalId = %v, finalEvalStatus = %v; want case2, %d", g0["evalId"], g0["finalEvalStatus"], models.EvalStatusPassed)
	}
	w0 := want["runEvalResults"].([]any)[0].(map[string]any)
	gPer := g0["evalMetricResultPerInvocation"].([]any)[0]
	wPer := w0["evalMetricResultPerInvocation"].([]any)[0]

	for _, level := range []struct {
		where     string
		got, want any
	}{
		{"top level", got, want},
		{"runEvalResults[0]", g0, w0},
		{"overall response_match_score", metricNamed(t, g0["overallEvalMetricResults"], "response_match_score", "Go overall"), metricNamed(t, w0["overallEvalMetricResults"], "response_match_score", "capture overall")},
		{"evalMetricResultPerInvocation[0]", gPer, wPer},
		{"per-invocation response_match_score", metricNamed(t, gPer.(map[string]any)["evalMetricResults"], "response_match_score", "Go per-invocation"), metricNamed(t, wPer.(map[string]any)["evalMetricResults"], "response_match_score", "capture per-invocation")},
	} {
		if diff := cmp.Diff(keysOf(t, level.want, "capture "+level.where), keysOf(t, level.got, "Go "+level.where)); diff != "" {
			t.Errorf("%s keys differ from adk-python (-adk-python +go):\n%s", level.where, diff)
		}
	}
}

func TestRunEvalLegacyBundleBodyStillWorks(t *testing.T) {
	l, _ := runSetWithOneCase(t)
	rec, err := callHandlerAs(t, l.runEvalLegacyHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/run_eval", "user1",
		json.RawMessage(`{"evalIds": ["case1"], "evalMetrics": [{"metricName": "response_match_score", "threshold": 0.7}]}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("legacy run: status = %d, err = %v", rec.Code, err)
	}
	var results []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("legacy response is not a bare array: %v (%s)", err, rec.Body)
	}
	if len(results) != 1 || results[0]["evalId"] != "case1" {
		t.Errorf("legacy results = %v, want one result for case1", results)
	}
}

// Mirrors adk-python test_fast_api.py's add-session missing-session case.
func TestAddSessionMissingSessionIs404(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/add-session", "user1",
		map[string]string{"evalId": "test_eval_case_id", "sessionId": "session_that_does_not_exist"},
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if alismux.StatusCode(err) != http.StatusNotFound || !strings.Contains(err.Error(), "Session not found") {
		t.Errorf("add-session for a missing session: err = %v, want 404 Session not found", err)
	}
}

// Mirrors adk-python test_fast_api.py's add-session missing-eval-set case.
func TestAddSessionMissingEvalSetIs404(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	seedSession(t, l, "user1", "sess1")
	_, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/missing_eval_set/add-session", "user1",
		map[string]string{"evalId": "c1", "sessionId": "sess1"},
		map[string]string{"app_name": "my_app", "eval_set_id": "missing_eval_set"})
	if alismux.StatusCode(err) != http.StatusNotFound {
		t.Errorf("add-session to a missing eval set: err = %v, want 404", err)
	}
	if c, _ := sets.GetEvalCase("my_app", "missing_eval_set", "c1"); c != nil {
		t.Errorf("eval case written for a missing set: %+v", c)
	}
}

// failingGetSessions is a session service whose Get fails for a reason other
// than a missing session.
type failingGetSessions struct{ session.Service }

func (failingGetSessions) Get(context.Context, *session.GetRequest) (*session.GetResponse, error) {
	return nil, errors.New("session backend unavailable")
}

// A backend failure is not a missing session: adk-python answers 404 only
// when the session is absent.
func TestAddSessionSessionBackendErrorIs500(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	l.launcherCfg.SessionService = failingGetSessions{l.launcherCfg.SessionService}
	_, err := callHandlerAs(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/add-session", "user1",
		map[string]string{"evalId": "c1", "sessionId": "sess1"},
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if got := alismux.StatusCode(err); got != http.StatusInternalServerError {
		t.Errorf("add-session with a failing session backend: status %d (err %v), want 500", got, err)
	}
}

// Acceptance 2: efficiency is reported on every run without being asked for.
func TestRunEvalReportsEfficiencyMetricsUnrequested(t *testing.T) {
	l, sets := newHandlerTestLauncher(t)
	_, _ = sets.CreateEvalSet("my_app", "set1")
	_ = sets.AddEvalCase("my_app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})
	rec, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
		json.RawMessage(`{"eval_metrics": [{"metricName": "tool_trajectory_avg_score", "threshold": 1.0}]}`),
		map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run: status = %d, err = %v, body = %s", rec.Code, err, rec.Body.String())
	}
	var got struct {
		RunEvalResults []struct {
			FinalEvalStatus          int `json:"finalEvalStatus"`
			OverallEvalMetricResults []struct {
				MetricName string `json:"metricName"`
				EvalStatus int    `json:"evalStatus"`
			} `json:"overallEvalMetricResults"`
		} `json:"runEvalResults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.RunEvalResults) != 1 {
		t.Fatalf("results = %+v, want one", got)
	}
	r := got.RunEvalResults[0]
	statuses := map[string]int{}
	for _, m := range r.OverallEvalMetricResults {
		statuses[m.MetricName] = m.EvalStatus
	}
	for _, name := range []string{"tool_call_count_v1", "inference_call_count_v1", "token_usage_v1", "invocation_duration_v1"} {
		if s, ok := statuses[name]; !ok || s != int(models.EvalStatusInformational) {
			t.Errorf("%s status = %v (present %v), want 4", name, s, ok)
		}
	}
	if r.FinalEvalStatus != statuses["tool_trajectory_avg_score"] {
		t.Errorf("case status = %d, want the trajectory's %d", r.FinalEvalStatus, statuses["tool_trajectory_avg_score"])
	}
}

// runGated starts a run of set1 with body in the background and returns a
// channel that yields the handler's result.
func runGated(t *testing.T, l *evalsLauncher, body string) <-chan error {
	t.Helper()
	// Checked here because callHandlerAs fails the test on a body it cannot
	// encode, and t.Fatal must not run on the goroutine below.
	if !json.Valid([]byte(body)) {
		t.Fatalf("invalid body %s", body)
	}
	done := make(chan error, 1)
	go func() {
		rec, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
			json.RawMessage(body), map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
		if err == nil && rec.Code != http.StatusOK {
			err = fmt.Errorf("status %d: %s", rec.Code, rec.Body.String())
		}
		done <- err
	}()
	return done
}

// Positive control for the next test: by default all three cases run at
// once.
func TestRunEvalRunsCasesConcurrentlyByDefault(t *testing.T) {
	g := newGate()
	done := runGated(t, gatedLauncher(t, g, metrics.DefaultRegistry), `{"eval_metrics":[{"metricName":"response_match_score","threshold":0.5}]}`)
	for range 3 {
		g.waitArrived(t)
	}
	close(g.release)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// Acceptance 4: parallelism 1 runs the cases one at a time.
func TestRunEvalParallelismOneRunsCasesOneAtATime(t *testing.T) {
	g := newGate()
	done := runGated(t, gatedLauncher(t, g, metrics.DefaultRegistry), `{"eval_metrics":[{"metricName":"response_match_score","threshold":0.5}],"parallelism":1}`)
	g.runOneAtATime(t, 3)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// Positive control for the next test: by default all three cases are scored
// at once.
func TestRunEvalScoresCasesConcurrentlyByDefault(t *testing.T) {
	g := newGate()
	done := runGated(t, gatedLauncher(t, nil, gatedRegistry(g)), `{"eval_metrics":[{"metricName":"gated","threshold":0.5}]}`)
	for range 3 {
		g.waitArrived(t)
	}
	close(g.release)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// Parallelism 1 also scores the cases one at a time.
func TestRunEvalParallelismOneScoresCasesOneAtATime(t *testing.T) {
	g := newGate()
	done := runGated(t, gatedLauncher(t, nil, gatedRegistry(g)), `{"eval_metrics":[{"metricName":"gated","threshold":0.5}],"parallelism":1}`)
	g.runOneAtATime(t, 3)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunEvalRejectsParallelismBelowOne(t *testing.T) {
	l := gatedLauncher(t, nil, metrics.DefaultRegistry)
	_, err := callHandlerAs(t, l.runEvalHandler(), http.MethodPost, "/dev/apps/my_app/eval-sets/set1/run", "user1",
		json.RawMessage(`{"eval_metrics":[],"parallelism":0}`), map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if alismux.StatusCode(err) != http.StatusBadRequest || !strings.Contains(err.Error(), "parallelism must be at least 1") {
		t.Errorf("error = %v, want a 400 naming parallelism", err)
	}
}
