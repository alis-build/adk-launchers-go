package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestMetricsInfo(t *testing.T) {
	l, _ := newHandlerTestLauncher(t)
	rec := callHandler(t, l.metricsInfoHandler(), http.MethodGet, "/dev/apps/my_app/metrics-info", nil, map[string]string{"app_name": "my_app"})
	var resp ListMetricsInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.MetricsInfo) < 10 {
		t.Fatalf("expected prebuilt metrics, got %d", len(resp.MetricsInfo))
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
