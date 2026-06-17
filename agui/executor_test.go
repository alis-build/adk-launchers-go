package agui

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type getCountSessionService struct {
	session.Service
	gets int
}

func (s *getCountSessionService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	s.gets++
	return s.Service.Get(ctx, req)
}

func testExecutorRuntimeWithEvents(t *testing.T, run func(agent.InvocationContext) iter.Seq2[*session.Event, error]) (*adkrun.Runtime, session.Service) {
	t.Helper()

	a, err := agent.New(agent.Config{
		Name:        "test-app",
		Description: "test agent for executor tests",
		Run:         run,
	})
	if err != nil {
		t.Fatalf("create test agent: %v", err)
	}

	svc := session.InMemoryService()
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: svc,
	}, "test-app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt, svc
}

func testExecutorRuntime(t *testing.T) (*adkrun.Runtime, session.Service) {
	t.Helper()
	return testExecutorRuntimeWithEvents(t, func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {}
	})
}

func TestExecuteContext_LazySessionLoad(t *testing.T) {
	svc := &getCountSessionService{Service: session.InMemoryService()}
	ctx := context.Background()
	_, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "test-app", UserID: "user-1", SessionID: "thread-1",
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	execCtx := newExecuteContext(ctx, &types.RunAgentInput{}, nil, "user-1", "thread-1", "run-1", "test-app", false, svc)
	_ = execCtx.ReadonlyState()
	_ = execCtx.Events()
	_ = execCtx.ReadonlyState()

	if svc.gets != 1 {
		t.Fatalf("session Get calls = %d, want 1 (cached)", svc.gets)
	}
}

func TestExecuteContext_EmptyViewsWithoutSessionService(t *testing.T) {
	execCtx := newExecuteContext(context.Background(), &types.RunAgentInput{}, nil, "user-1", "thread-1", "run-1", "test-app", false, nil)
	if _, err := execCtx.ReadonlyState().Get("missing"); !errors.Is(err, session.ErrStateKeyNotExist) {
		t.Fatalf("ReadonlyState().Get() err = %v, want ErrStateKeyNotExist", err)
	}
	if execCtx.Events().Len() != 0 {
		t.Fatalf("Events().Len() = %d, want 0", execCtx.Events().Len())
	}
}

func TestDefaultExecutor_RunLifecycle(t *testing.T) {
	rt, svc := testExecutorRuntime(t)
	l := newTestLauncher("test-app", svc)
	l.runtime = rt

	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	exec := deps.NewDefault(ExecutorConfig{})

	execCtx := newExecuteContext(
		context.Background(),
		&types.RunAgentInput{Messages: []types.Message{{Role: "user", Content: "hello"}}},
		nil, "user-1", "thread-1", "run-1", "test-app", false, svc,
	)

	var typesSeen []events.EventType
	for ev, err := range exec.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		typesSeen = append(typesSeen, ev.Type())
	}

	if len(typesSeen) < 2 {
		t.Fatalf("event count = %d, want at least RunStarted and RunFinished", len(typesSeen))
	}
	if typesSeen[0] != events.EventTypeRunStarted {
		t.Fatalf("first event = %v, want RUN_STARTED", typesSeen[0])
	}
	if typesSeen[len(typesSeen)-1] != events.EventTypeRunFinished {
		t.Fatalf("last event = %v, want RUN_FINISHED", typesSeen[len(typesSeen)-1])
	}
}

func TestDefaultExecutor_BeforeExecuteCallbackAbort(t *testing.T) {
	rt, svc := testExecutorRuntime(t)
	l := newTestLauncher("test-app", svc)
	l.runtime = rt

	wantErr := errors.New("before abort")
	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	exec := deps.NewDefault(ExecutorConfig{
		BeforeExecuteCallback: func(context.Context, ExecutorContext) (context.Context, error) {
			return nil, wantErr
		},
	})

	execCtx := newExecuteContext(
		context.Background(),
		&types.RunAgentInput{Messages: []types.Message{{Role: "user", Content: "hello"}}},
		nil, "user-1", "thread-1", "run-1", "test-app", false, svc,
	)

	var gotRunError bool
	for ev, err := range exec.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Execute iterator error: %v", err)
		}
		if ev.Type() == events.EventTypeRunError {
			gotRunError = true
		}
	}
	if !gotRunError {
		t.Fatal("expected RunError event from BeforeExecuteCallback abort")
	}
}

func TestDefaultExecutor_AfterEventCallbackAbort(t *testing.T) {
	rt, svc := testExecutorRuntimeWithEvents(t, func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			ev := session.NewEvent("inv1")
			ev.Content = genai.NewContentFromText("hi", genai.RoleModel)
			ev.Partial = true
			yield(ev, nil)
		}
	})
	l := newTestLauncher("test-app", svc)
	l.runtime = rt

	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	exec := deps.NewDefault(ExecutorConfig{
		AfterEventCallback: func(ExecutorContext, *session.Event, []events.Event) error {
			return fmt.Errorf("after event abort")
		},
	})

	execCtx := newExecuteContext(
		context.Background(),
		&types.RunAgentInput{Messages: []types.Message{{Role: "user", Content: "hello"}}},
		nil, "user-1", "thread-1", "run-1", "test-app", false, svc,
	)

	var gotRunError bool
	for ev, err := range exec.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Execute iterator error: %v", err)
		}
		if ev.Type() == events.EventTypeRunError {
			gotRunError = true
		}
	}
	if !gotRunError {
		t.Fatal("expected RunError from AfterEventCallback abort")
	}
}

func TestDefaultExecutor_AfterExecuteCallback(t *testing.T) {
	rt, svc := testExecutorRuntime(t)
	l := newTestLauncher("test-app", svc)
	l.runtime = rt

	var called bool
	deps := ExecutorDeps{Launcher: l, Runtime: rt, Config: l.config}
	exec := deps.NewDefault(ExecutorConfig{
		AfterExecuteCallback: func(ExecutorContext, error) error {
			called = true
			return nil
		},
	})

	execCtx := newExecuteContext(
		context.Background(),
		&types.RunAgentInput{Messages: []types.Message{{Role: "user", Content: "hello"}}},
		nil, "user-1", "thread-1", "run-1", "test-app", false, svc,
	)
	for range exec.Execute(context.Background(), execCtx) {
	}
	if !called {
		t.Fatal("AfterExecuteCallback was not invoked")
	}
}

func TestYieldSink_StopsOnFalseYield(t *testing.T) {
	var emitted int
	sink := newYieldSink(func(events.Event, error) bool {
		emitted++
		return emitted < 1
	})
	sink.Emit(events.NewRunStartedEvent("t1", "r1"))
	sink.Emit(events.NewRunFinishedEvent("t1", "r1"))
	if !sink.Stopped() {
		t.Fatal("yieldSink.Stopped() = false, want true after consumer stopped")
	}
	if emitted != 1 {
		t.Fatalf("yield calls = %d, want 1", emitted)
	}
}

func TestCustomExecutor_WithExecutor(t *testing.T) {
	l := NewLauncher("test-app", WithExecutor(func(_ ExecutorDeps) AgentExecutor {
		return agentExecutorFunc(func(context.Context, ExecutorContext) iter.Seq2[events.Event, error] {
			return func(yield func(events.Event, error) bool) {
				yield(events.NewRunStartedEvent("t1", "r1"), nil)
				yield(events.NewRunFinishedEvent("t1", "r1"), nil)
			}
		})
	})).(*aguiLauncher)

	cfg := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(mustTestAgent(t)),
		SessionService: session.InMemoryService(),
	}
	if err := l.mountHostRoutes(cfg); err != nil {
		t.Fatalf("mountHostRoutes: %v", err)
	}
	if l.executor == nil {
		t.Fatal("executor not configured after mountHostRoutes")
	}

	execCtx := newExecuteContext(context.Background(), &types.RunAgentInput{}, &http.Request{}, "u", "t", "r", "test-app", false, cfg.SessionService)
	var count int
	for range l.executor.Execute(context.Background(), execCtx) {
		count++
	}
	if count != 2 {
		t.Fatalf("custom executor yielded %d events, want 2", count)
	}
}

type agentExecutorFunc func(context.Context, ExecutorContext) iter.Seq2[events.Event, error]

func (f agentExecutorFunc) Execute(ctx context.Context, execCtx ExecutorContext) iter.Seq2[events.Event, error] {
	return f(ctx, execCtx)
}

func mustTestAgent(t *testing.T) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: "test-app",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return a
}
