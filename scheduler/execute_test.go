package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	pb "go.alis.build/common/alis/agui/scheduler/v1"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/iam/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeScheduler struct {
	pb.UnimplementedSchedulerServiceServer

	cron      *pb.Cron
	update    *pb.UpdateCronRequest
	updateErr error
}

func (f *fakeScheduler) GetCron(context.Context, *pb.GetCronRequest) (*pb.Cron, error) {
	if f.cron == nil {
		return nil, status.Error(codes.NotFound, "cron not found")
	}
	return f.cron, nil
}

func (f *fakeScheduler) UpdateCron(_ context.Context, req *pb.UpdateCronRequest) (*pb.Cron, error) {
	f.update = req
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return req.GetCron(), nil
}

type recordedRun struct {
	userID     string
	sessionID  string
	appName    string
	prompt     string
	stateDelta map[string]any
}

type fakeRunner struct {
	runs      []recordedRun
	nextSess  string
	runErr    error
	done      chan struct{}
}

func (f *fakeRunner) RunCron(_ context.Context, req adkrun.RunRequest) (string, error) {
	if f.runErr != nil {
		return "", f.runErr
	}
	f.runs = append(f.runs, recordedRun{
		userID:     req.UserID,
		sessionID:  req.SessionID,
		appName:    req.AppName,
		prompt:     req.NewMessage.Parts[0].Text,
		stateDelta: req.StateDelta,
	})
	if f.nextSess == "" {
		f.nextSess = "adk-session-1"
	}
	if f.done != nil {
		f.done <- struct{}{}
	}
	return f.nextSess, nil
}

type testObserver struct {
	onStarted  func(context.Context, *pb.Cron)
	onFinished func(context.Context, *pb.Cron, error)
}

func (o *testObserver) OnCronStarted(ctx context.Context, cron *pb.Cron) {
	if o.onStarted != nil {
		o.onStarted(ctx, cron)
	}
}

func (o *testObserver) OnCronFinished(ctx context.Context, cron *pb.Cron, err error) {
	if o.onFinished != nil {
		o.onFinished(ctx, cron, err)
	}
}

// callCronHandler builds a cronHandler and invokes it with the given cron ID.
func callCronHandler(t *testing.T, svc *fakeScheduler, runner *fakeRunner, cfg *cronConfig, cronID string) *httptest.ResponseRecorder {
	t.Helper()
	handler := cronHandler(svc, runner, cfg.systemIdentity, cfg)
	body, _ := json.Marshal(map[string]string{"id": cronID})
	req := httptest.NewRequest(http.MethodPost, "/handler", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	if err := handler(rec, req); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func testCronConfig() *cronConfig {
	return &cronConfig{
		defaultAppName: "my.agent",
		systemIdentity: &iam.Identity{
			ID:    "system@test",
			Email: "system@test",
			Type:  iam.ServiceAccount,
		},
	}
}

// TestSmoke_executeCron runs cron tick → ADK prompts → UpdateCron (in-process smoke).
func TestSmoke_executeCron(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/smoke-1",
			Owner:  "users/alice",
			Email:  "alice@example.com",
			Prompt: "daily check-in",
			Type:   pb.Cron_TYPE_CRON,
			Thread: "",
		},
	}
	runner := &fakeRunner{nextSess: "sess-smoke"}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}

	if len(runner.runs) != 1 || runner.runs[0].prompt != "daily check-in" {
		t.Fatalf("runs = %#v", runner.runs)
	}
	if runner.runs[0].userID != "alice" {
		t.Fatalf("userID = %q, want alice", runner.runs[0].userID)
	}
	if runner.runs[0].appName != "my.agent" {
		t.Fatalf("appName = %q", runner.runs[0].appName)
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron")
	}
	if svc.update.GetCron().GetThread() != "threads/sess-smoke" {
		t.Fatalf("thread = %q", svc.update.GetCron().GetThread())
	}
	if svc.update.GetCron().GetLastRunTime() == nil {
		t.Fatal("expected last_run_time")
	}
	if !slices.Contains(svc.update.GetUpdateMask().GetPaths(), "thread") {
		t.Fatalf("update mask = %v", svc.update.GetUpdateMask().GetPaths())
	}
}

func TestSmoke_executeCron_agentID(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:    "crons/agent",
			Owner:   "users/alice",
			Prompt:  "tick",
			Type:    pb.Cron_TYPE_CRON,
			AgentId: "finance-agent",
		},
	}
	runner := &fakeRunner{nextSess: "sess-agent"}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	if len(runner.runs) != 1 || runner.runs[0].appName != "finance-agent" {
		t.Fatalf("runs = %#v", runner.runs)
	}
}

func TestSmoke_executeCron_initialPromptThenMain(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:          "crons/recurring",
			Owner:         "users/bob",
			Prompt:        "tick",
			InitialPrompt: "bootstrap",
			Type:          pb.Cron_TYPE_CRON,
		},
	}
	runner := &fakeRunner{nextSess: "sess-recur"}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "bob"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	if len(runner.runs) != 2 {
		t.Fatalf("runs = %#v", runner.runs)
	}
	if runner.runs[0].prompt != "bootstrap" || runner.runs[1].prompt != "tick" {
		t.Fatalf("unexpected prompts: %#v", runner.runs)
	}
}

func TestSmoke_executeCron_typeAtArchives(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/once",
			Owner:  "users/alice",
			Prompt: "run once",
			Type:   pb.Cron_TYPE_AT,
		},
	}
	runner := &fakeRunner{nextSess: "sess-at"}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	if svc.update.GetCron().GetState() != pb.Cron_STATE_ARCHIVED {
		t.Fatalf("state = %v", svc.update.GetCron().GetState())
	}
	paths := svc.update.GetUpdateMask().GetPaths()
	for _, want := range []string{"state", "archive_time", "last_failure_time", "last_failure_message"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("update mask missing %q: %v", want, paths)
		}
	}
	if svc.update.GetCron().GetLastFailureTime() != nil {
		t.Fatal("expected last_failure_time cleared on success")
	}
	if svc.update.GetCron().GetLastFailureMessage() != "" {
		t.Fatalf("expected last_failure_message cleared on success, got %q", svc.update.GetCron().GetLastFailureMessage())
	}
}

func TestSmoke_executeCron_successClearsPriorFailure(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/recover",
			Owner:  "users/alice",
			Prompt: "tick",
			Type:   pb.Cron_TYPE_CRON,
		},
	}
	runner := &fakeRunner{nextSess: "sess-recover"}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	// Success must mask failure fields so SchedulerService Prune clears any prior values.
	paths := svc.update.GetUpdateMask().GetPaths()
	for _, want := range []string{"last_failure_time", "last_failure_message"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("update mask missing %q: %v", want, paths)
		}
	}
	if svc.update.GetCron().GetLastFailureTime() != nil {
		t.Fatal("update must not set last_failure_time on success; clearing is via Prune")
	}
	if svc.update.GetCron().GetLastFailureMessage() != "" {
		t.Fatal("update must not set last_failure_message on success; clearing is via Prune")
	}
}

func TestSmoke_executeCron_typeAtFailureArchivesWithError(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/once-fail",
			Owner:  "users/alice",
			Prompt: "run once",
			Type:   pb.Cron_TYPE_AT,
		},
	}
	runner := &fakeRunner{runErr: errors.New("agent callback failed")}

	err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice")
	if err == nil {
		t.Fatal("expected executeCron error")
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron after failed TYPE_AT run")
	}
	if svc.update.GetCron().GetState() != pb.Cron_STATE_ARCHIVED {
		t.Fatalf("state = %v", svc.update.GetCron().GetState())
	}
	if svc.update.GetCron().GetLastFailureTime() == nil {
		t.Fatal("expected last_failure_time")
	}
	if svc.update.GetCron().GetLastFailureMessage() == "" {
		t.Fatal("expected last_failure_message")
	}
	if svc.update.GetCron().GetLastRunTime() != nil {
		t.Fatal("expected no last_run_time on failed run")
	}
	paths := svc.update.GetUpdateMask().GetPaths()
	for _, want := range []string{"state", "archive_time", "last_failure_time", "last_failure_message"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("update mask missing %q: %v", want, paths)
		}
	}
}

func TestSmoke_executeCron_typeCronFailureStaysActive(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/recurring-fail",
			Owner:  "users/alice",
			Prompt: "tick",
			Type:   pb.Cron_TYPE_CRON,
		},
	}
	runner := &fakeRunner{runErr: errors.New("agent failed")}

	err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice")
	if err == nil {
		t.Fatal("expected executeCron error")
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron after failed TYPE_CRON run")
	}
	// Recurring failure updates only failure fields via the field mask; the
	// partial Cron message must leave State at its zero value (STATE_UNSPECIFIED)
	// so nothing outside the mask can accidentally clobber the stored state.
	if got := svc.update.GetCron().GetState(); got != pb.Cron_STATE_UNSPECIFIED {
		t.Fatalf("state = %v, want STATE_UNSPECIFIED in partial update", got)
	}
	if svc.update.GetCron().GetArchiveTime() != nil {
		t.Fatal("expected no archive_time on recurring failure")
	}
	if svc.update.GetCron().GetLastFailureTime() == nil {
		t.Fatal("expected last_failure_time")
	}
	paths := svc.update.GetUpdateMask().GetPaths()
	for _, want := range []string{"last_failure_time", "last_failure_message"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("update mask missing %q: %v", want, paths)
		}
	}
	if slices.Contains(paths, "state") || slices.Contains(paths, "archive_time") {
		t.Fatalf("recurring failure should not archive: %v", paths)
	}
}

// Configuration errors (empty/unknown app name) must not archive a TYPE_AT cron
// even though the schedule is one-shot: the agent is misconfigured, not the
// schedule, and archival would hide the failure from operators after a redeploy.
func TestSmoke_executeCron_validateFailureDoesNotArchiveTypeAt(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/misconfigured",
			Owner:  "users/alice",
			Prompt: "run once",
			Type:   pb.Cron_TYPE_AT,
		},
	}
	runner := &fakeRunner{}
	cfg := testCronConfig()
	cfg.defaultAppName = "" // force ValidateCronAppName to reject with "app name is required"

	err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice")
	if err == nil {
		t.Fatal("expected executeCron error for missing app name")
	}
	if len(runner.runs) != 0 {
		t.Fatalf("expected agent not to run, got %#v", runner.runs)
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron to record the validation failure")
	}
	if svc.update.GetCron().GetState() == pb.Cron_STATE_ARCHIVED {
		t.Fatal("TYPE_AT must not be archived on a validation/config error")
	}
	if svc.update.GetCron().GetArchiveTime() != nil {
		t.Fatal("expected no archive_time on validation failure")
	}
	if svc.update.GetCron().GetLastFailureTime() == nil {
		t.Fatal("expected last_failure_time")
	}
	if svc.update.GetCron().GetLastFailureMessage() == "" {
		t.Fatal("expected last_failure_message")
	}
	paths := svc.update.GetUpdateMask().GetPaths()
	if slices.Contains(paths, "state") || slices.Contains(paths, "archive_time") {
		t.Fatalf("validation failure must not touch state/archive_time mask: %v", paths)
	}
}

// A failure to persist a run failure must be surfaced to the CronObserver so
// operators can distinguish "agent failed but we recorded it" from "agent
// failed and we also lost the record".
func TestSmoke_executeCron_persistFailureVisibleToObserver(t *testing.T) {
	agentErr := errors.New("agent boom")
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/persist-fail-on-failure",
			Owner:  "users/alice",
			Prompt: "tick",
			Type:   pb.Cron_TYPE_CRON,
		},
		updateErr: status.Error(codes.Unavailable, "spanner transient"),
	}
	runner := &fakeRunner{runErr: agentErr}

	var observedErr error
	cfg := testCronConfig()
	cfg.observer = &testObserver{onFinished: func(_ context.Context, _ *pb.Cron, err error) {
		observedErr = err
	}}

	err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice")
	if err == nil {
		t.Fatal("expected executeCron error")
	}
	if !errors.Is(err, agentErr) {
		t.Fatalf("returned err = %v, want wraps %v", err, agentErr)
	}
	if observedErr == nil {
		t.Fatal("observer should see combined error")
	}
	if !errors.Is(observedErr, agentErr) {
		t.Fatalf("observer err = %v, want wraps agent err %v", observedErr, agentErr)
	}
	if !strings.Contains(observedErr.Error(), "persist cron failure") {
		t.Fatalf("observer err = %q, want to include persist failure", observedErr.Error())
	}
}

func TestTruncateCronFailureMessage(t *testing.T) {
	t.Run("short passthrough", func(t *testing.T) {
		got := truncateCronFailureMessage("boom")
		if got != "boom" {
			t.Fatalf("got %q, want %q", got, "boom")
		}
	})
	t.Run("trims whitespace", func(t *testing.T) {
		got := truncateCronFailureMessage("   boom \n")
		if got != "boom" {
			t.Fatalf("got %q, want %q", got, "boom")
		}
	})
	t.Run("empty after trim", func(t *testing.T) {
		if got := truncateCronFailureMessage("   \t\n"); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("long ascii truncated", func(t *testing.T) {
		in := strings.Repeat("a", maxCronFailureMessageLen+50)
		got := truncateCronFailureMessage(in)
		if len(got) != maxCronFailureMessageLen {
			t.Fatalf("len = %d, want %d", len(got), maxCronFailureMessageLen)
		}
		if !utf8.ValidString(got) {
			t.Fatal("truncated string is not valid UTF-8")
		}
	})
	t.Run("multi-byte rune boundary", func(t *testing.T) {
		// "€" is 3 bytes in UTF-8. Fill the buffer with € so the naive byte
		// cut at maxCronFailureMessageLen lands mid-rune.
		euro := "€"
		count := (maxCronFailureMessageLen / len(euro)) + 5
		in := strings.Repeat(euro, count)
		got := truncateCronFailureMessage(in)
		if len(got) > maxCronFailureMessageLen {
			t.Fatalf("len = %d, exceeds cap %d", len(got), maxCronFailureMessageLen)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("truncated string is not valid UTF-8: bytes=%x", []byte(got))
		}
		if len(got)%len(euro) != 0 {
			t.Fatalf("length %d not aligned to %d-byte rune boundary", len(got), len(euro))
		}
	})
}

func TestCronHandler_sync_smoke(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/handler-1",
			Owner:  "users/alice",
			Prompt: "hello",
			Type:   pb.Cron_TYPE_CRON,
		},
	}
	runner := &fakeRunner{nextSess: "sess-handler"}
	cfg := testCronConfig()
	cfg.syncExecution = true

	rec := callCronHandler(t, svc, runner, cfg, "handler-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs = %#v", runner.runs)
	}
	if runner.runs[0].userID != "alice" {
		t.Fatalf("userID = %q, want alice", runner.runs[0].userID)
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron after sync handler")
	}

	var resp cronResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "OK" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestCronHandler_archivedCronSkipsRun(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/archived",
			Owner:  "users/alice",
			Prompt: "noop",
			State:  pb.Cron_STATE_ARCHIVED,
		},
	}
	runner := &fakeRunner{}
	cfg := testCronConfig()
	cfg.syncExecution = true

	rec := callCronHandler(t, svc, runner, cfg, "archived")
	_ = rec
	if len(runner.runs) != 0 {
		t.Fatalf("expected no runs, got %#v", runner.runs)
	}
	if svc.update != nil {
		t.Fatal("expected no UpdateCron for archived cron")
	}
}

func TestSmoke_executeCron_updateCronFailsReturnsNil(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/persist-fail",
			Owner:  "users/alice",
			Prompt: "hello",
			Type:   pb.Cron_TYPE_CRON,
		},
		updateErr: status.Error(codes.Unavailable, "spanner transient"),
	}
	runner := &fakeRunner{nextSess: "sess-persist"}

	var observedErr error
	cfg := testCronConfig()
	cfg.observer = &testObserver{onFinished: func(_ context.Context, _ *pb.Cron, err error) {
		observedErr = err
	}}

	err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice")
	if err != nil {
		t.Fatalf("executeCron should return nil on persist failure, got %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("agent should have run, runs = %#v", runner.runs)
	}
	if observedErr == nil {
		t.Fatal("observer should see persist error")
	}
}

func TestCronHandler_sync_updateCronFailsReturns200(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/sync-persist",
			Owner:  "users/alice",
			Prompt: "hello",
			Type:   pb.Cron_TYPE_CRON,
		},
		updateErr: status.Error(codes.Unavailable, "spanner transient"),
	}
	runner := &fakeRunner{nextSess: "sess-sync-persist"}
	cfg := testCronConfig()
	cfg.syncExecution = true

	rec := callCronHandler(t, svc, runner, cfg, "sync-persist")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even on persist failure", rec.Code)
	}
}

func TestCronHandler_async_smoke(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/async-1",
			Owner:  "users/alice",
			Prompt: "async hello",
			Type:   pb.Cron_TYPE_CRON,
		},
	}
	done := make(chan struct{}, 1)
	runner := &fakeRunner{nextSess: "sess-async", done: done}
	cfg := testCronConfig()

	rec := callCronHandler(t, svc, runner, cfg, "async-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	// Wait for the async goroutine to complete.
	<-done

	if len(runner.runs) != 1 {
		t.Fatalf("runs = %#v", runner.runs)
	}
	if runner.runs[0].userID != "alice" {
		t.Fatalf("userID = %q, want alice", runner.runs[0].userID)
	}
}

// recordingInterceptor captures ordered BeforeRun/AfterRun calls across
// multiple interceptors so a test can assert chain order and short-circuit
// behavior. The pointer receiver mutates cfg.runInterceptors state by index.
type recordingInterceptor struct {
	label    string
	log      *[]string
	beforeFn func(*CronRunContext) error
	afterFn  func(sessionID string, err error) error
}

func (r *recordingInterceptor) BeforeRun(ctx context.Context, run *CronRunContext) (context.Context, error) {
	*r.log = append(*r.log, "before:"+r.label)
	if r.beforeFn != nil {
		if err := r.beforeFn(run); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}

func (r *recordingInterceptor) AfterRun(_ context.Context, _ *CronRunContext, sessionID string, err error) error {
	_ = sessionID
	_ = err
	*r.log = append(*r.log, "after:"+r.label)
	if r.afterFn != nil {
		return r.afterFn(sessionID, err)
	}
	return nil
}

func newCronForInterceptorTest(name string) *pb.Cron {
	return &pb.Cron{
		Name:   "crons/" + name,
		Owner:  "users/alice",
		Prompt: "hello",
		Type:   pb.Cron_TYPE_CRON,
	}
}

func TestExecuteCron_runInterceptorMutatesPromptAndAppName(t *testing.T) {
	svc := &fakeScheduler{cron: newCronForInterceptorTest("hook")}
	runner := &fakeRunner{nextSess: "sess-hook"}
	cfg := testCronConfig()
	log := []string{}
	cfg.runInterceptors = []CronRunInterceptor{
		&recordingInterceptor{
			label: "a",
			log:   &log,
			beforeFn: func(run *CronRunContext) error {
				run.Prompt = "cron: " + run.Prompt
				run.AppName = "other-agent"
				run.StateDelta = map[string]any{"tenant": "acme"}
				return nil
			},
		},
	}

	if err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs = %#v", runner.runs)
	}
	got := runner.runs[0]
	if got.prompt != "cron: hello" {
		t.Fatalf("prompt = %q, want %q", got.prompt, "cron: hello")
	}
	if got.appName != "other-agent" {
		t.Fatalf("appName = %q, want other-agent", got.appName)
	}
	if got.stateDelta == nil || got.stateDelta["tenant"] != "acme" {
		t.Fatalf("stateDelta = %#v, want tenant=acme", got.stateDelta)
	}
}

func TestExecuteCron_runInterceptorChainOrder(t *testing.T) {
	svc := &fakeScheduler{cron: newCronForInterceptorTest("chain")}
	runner := &fakeRunner{nextSess: "sess-chain"}
	cfg := testCronConfig()
	log := []string{}
	cfg.runInterceptors = []CronRunInterceptor{
		&recordingInterceptor{label: "a", log: &log},
		nil, // nil interceptor must be skipped
		&recordingInterceptor{label: "b", log: &log},
	}

	if err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	want := []string{"before:a", "before:b", "after:a", "after:b"}
	if !slices.Equal(log, want) {
		t.Fatalf("chain order = %v, want %v", log, want)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("expected exactly one ADK run, got %d", len(runner.runs))
	}
}

func TestExecuteCron_beforeRunErrorAbortsAndSkipsSubsequent(t *testing.T) {
	svc := &fakeScheduler{cron: newCronForInterceptorTest("abort")}
	runner := &fakeRunner{nextSess: "sess-abort"}
	cfg := testCronConfig()
	log := []string{}
	sentinel := errors.New("interceptor: abort")
	cfg.runInterceptors = []CronRunInterceptor{
		&recordingInterceptor{
			label:    "a",
			log:      &log,
			beforeFn: func(*CronRunContext) error { return sentinel },
		},
		&recordingInterceptor{label: "b", log: &log},
	}

	err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("executeCron err = %v, want wraps %v", err, sentinel)
	}
	if len(runner.runs) != 0 {
		t.Fatalf("expected ADK not to run when BeforeRun aborts, got %#v", runner.runs)
	}
	// After the aborting BeforeRun, neither the next BeforeRun nor any AfterRun
	// should fire: the chain short-circuits and the ADK dispatch is skipped.
	want := []string{"before:a"}
	if !slices.Equal(log, want) {
		t.Fatalf("chain = %v, want %v", log, want)
	}
	// A BeforeRun abort is still a run failure and must be recorded on the cron
	// via last_failure_time / last_failure_message so operators can see it,
	// without touching state/archive_time (TYPE_CRON must remain active).
	if svc.update == nil {
		t.Fatal("expected UpdateCron to record the BeforeRun-abort failure")
	}
	if svc.update.GetCron().GetLastFailureTime() == nil {
		t.Fatal("expected last_failure_time")
	}
	paths := svc.update.GetUpdateMask().GetPaths()
	for _, want := range []string{"last_failure_time", "last_failure_message"} {
		if !slices.Contains(paths, want) {
			t.Fatalf("update mask missing %q: %v", want, paths)
		}
	}
	if slices.Contains(paths, "state") || slices.Contains(paths, "archive_time") {
		t.Fatalf("recurring failure must not archive: %v", paths)
	}
}

func TestExecuteCron_afterRunErrorIsLoggedAndDoesNotFailTick(t *testing.T) {
	svc := &fakeScheduler{cron: newCronForInterceptorTest("after-err")}
	runner := &fakeRunner{nextSess: "sess-after-err"}
	cfg := testCronConfig()
	log := []string{}
	cfg.runInterceptors = []CronRunInterceptor{
		&recordingInterceptor{
			label:   "a",
			log:     &log,
			afterFn: func(string, error) error { return errors.New("first after failed") },
		},
		&recordingInterceptor{label: "b", log: &log},
	}

	if err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	// Both AfterRun hooks must fire even though the first returned an error;
	// the tick itself must succeed and UpdateCron must run.
	want := []string{"before:a", "before:b", "after:a", "after:b"}
	if !slices.Equal(log, want) {
		t.Fatalf("chain = %v, want %v", log, want)
	}
	if svc.update == nil {
		t.Fatal("expected UpdateCron after AfterRun error")
	}
}

// nilContextInterceptor returns (nil, nil) from BeforeRun to verify the
// executor does not overwrite the incoming context with nil (which would
// otherwise panic downstream in metadata.NewOutgoingContext).
type nilContextInterceptor struct{}

func (nilContextInterceptor) BeforeRun(_ context.Context, _ *CronRunContext) (context.Context, error) {
	return nil, nil
}

func (nilContextInterceptor) AfterRun(context.Context, *CronRunContext, string, error) error {
	return nil
}

func TestExecuteCron_nilReturnedContextDoesNotPanic(t *testing.T) {
	svc := &fakeScheduler{cron: newCronForInterceptorTest("nil-ctx")}
	runner := &fakeRunner{nextSess: "sess-nil-ctx"}
	cfg := testCronConfig()
	cfg.runInterceptors = []CronRunInterceptor{nilContextInterceptor{}}

	if err := executeCron(context.Background(), svc, runner, cfg, svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs = %#v", runner.runs)
	}
}

var _ pb.SchedulerServiceServer = (*fakeScheduler)(nil)
