package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/session"
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
				ev := session.NewEvent(ctx.InvocationID())
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
	ctx := context.Background()
	svc := l.launcherCfg.SessionService
	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "my_app", UserID: "user1", SessionID: "sess1",
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	ev := session.NewEvent("inv1")
	ev.Author = "user"
	ev.Content = genai.NewContentFromText("hello", genai.RoleUser)
	_ = svc.AppendEvent(ctx, createResp.Session, ev)
	agentEv := session.NewEvent("inv1")
	agentEv.Author = "agent"
	agentEv.Content = genai.NewContentFromText("hi", genai.RoleModel)
	_ = svc.AppendEvent(ctx, createResp.Session, agentEv)

	rec := callHandler(t, l.addSessionHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/add_session", map[string]string{
		"evalId": "from_session", "sessionId": "sess1", "userId": "user1",
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add session status = %d body = %s", rec.Code, rec.Body.String())
	}

	got, err := sets.GetEvalCase("my_app", "set1", "from_session")
	if err != nil || got == nil || len(got.Conversation) == 0 {
		t.Fatalf("eval case = %+v err = %v", got, err)
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

	rec := callHandler(t, l.runEvalLegacyHandler(), http.MethodPost, "/dev/apps/my_app/eval_sets/set1/run_eval", map[string]any{
		"evalMetrics": []map[string]any{{
			"metricName": models.MetricResponseMatchScore,
			"threshold":  0.5,
		}},
	}, map[string]string{"app_name": "my_app", "eval_set_id": "set1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("run eval status = %d body = %s", rec.Code, rec.Body.String())
	}
	var results []models.RunEvalResult
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("results = %+v", results)
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
