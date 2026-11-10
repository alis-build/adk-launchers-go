package service_test

import (
	"context"
	"iter"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/generator"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// stubRubricJudge returns a fixed judge response for every prompt.
type stubRubricJudge struct{ response string }

func (s stubRubricJudge) GenerateJudgeResponse(context.Context, string, models.JudgeModelOptions) (string, error) {
	return s.response, nil
}

func TestPerformInferenceSessionIDMatchesRunnerSession(t *testing.T) {
	ctx := context.Background()
	sessSvc := session.InMemoryService()
	const wantSessionID = generator.EvalSessionIDPrefix + "fixed-eval-session"

	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{
			{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)},
		},
	})

	a, err := agent.New(agent.Config{
		Name: "app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "app"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: sessSvc,
	}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	svc := &service.LocalEvalService{
		Generator:    &generator.Generator{Runtime: rt},
		Sets:         sets,
		SimProvider:  simulation.UserSimulatorProvider{},
		Sessions:     sessSvc,
		NewSessionID: func() string { return wantSessionID },
	}
	inf, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName: "app", EvalSetID: "set1",
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].Status != service.InferenceStatusSuccess {
		t.Fatalf("inf = %+v", inf)
	}
	if inf[0].SessionID != wantSessionID {
		t.Fatalf("SessionID = %q, want %q", inf[0].SessionID, wantSessionID)
	}

	resp, err := sessSvc.Get(ctx, &session.GetRequest{
		AppName:   "app",
		UserID:    "test_user_id",
		SessionID: wantSessionID,
	})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	if resp == nil || resp.Session == nil || resp.Session.Events().Len() == 0 {
		t.Fatal("expected inference session to exist with events")
	}
}

func TestPerformInferenceRunSessionStateMergedIntoRunner(t *testing.T) {
	ctx := context.Background()
	sessSvc := session.InMemoryService()
	const wantSessionID = generator.EvalSessionIDPrefix + "session-state-eval"

	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		SessionInput: &models.SessionInput{
			State: map[string]any{"idea_name": "ideas/case"},
		},
		Conversation: []models.Invocation{
			{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)},
		},
	})

	a, err := agent.New(agent.Config{
		Name: "app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "app"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: sessSvc,
	}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	svc := &service.LocalEvalService{
		Generator:    &generator.Generator{Runtime: rt},
		Sets:         sets,
		SimProvider:  simulation.UserSimulatorProvider{},
		Sessions:     sessSvc,
		NewSessionID: func() string { return wantSessionID },
	}
	inf, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName:   "app",
		EvalSetID: "set1",
		SessionState: map[string]any{
			"idea_name":    "ideas/run",
			"account_name": "accounts/run",
		},
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].Status != service.InferenceStatusSuccess {
		t.Fatalf("inf = %+v", inf)
	}

	resp, err := sessSvc.Get(ctx, &session.GetRequest{
		AppName:   "app",
		UserID:    "test_user_id",
		SessionID: wantSessionID,
	})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	idea, err := resp.Session.State().Get("idea_name")
	if err != nil {
		t.Fatalf("idea_name state: %v", err)
	}
	if idea != "ideas/case" {
		t.Fatalf("idea_name = %v, want ideas/case (case overrides run)", idea)
	}
	account, err := resp.Session.State().Get("account_name")
	if err != nil {
		t.Fatalf("account_name state: %v", err)
	}
	if account != "accounts/run" {
		t.Fatalf("account_name = %v, want accounts/run", account)
	}
}

func TestLocalEvalServiceEvaluate(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	call := &genai.FunctionCall{Name: "lookup", Args: map[string]any{}}
	expected := models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:      genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse:    genai.NewContentFromText("ok", genai.RoleModel),
			IntermediateData: models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{call}}),
		}},
	}
	_ = sets.AddEvalCase("app", "set1", expected)

	a, _ := agent.New(agent.Config{
		Name: "agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "agent"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	rt, _ := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "agent")

	svc := &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	inf, err := svc.PerformInference(ctx, service.InferenceRequest{
		AppName: "app", EvalSetID: "set1",
	})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].Status != service.InferenceStatusSuccess {
		t.Fatalf("inf = %+v", inf)
	}

	actualCall := &genai.FunctionCall{Name: "lookup", Args: map[string]any{}}
	inf[0].Inferences[0].IntermediateData = models.IntermediateDataField(models.IntermediateData{ToolUses: []*genai.FunctionCall{actualCall}})

	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricToolTrajectoryAvgScore,
				Threshold:  0.5,
				Criterion: models.CriterionField(models.ToolTrajectoryCriterion{
					BaseCriterion: models.BaseCriterion{Threshold: 0.5},
				}),
			}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("results = %+v", results)
	}
}

// TestEvaluatePartialFailureNoZeroResults ensures that when an inference
// references a missing eval case, Evaluate does not emit zero-valued
// EvalCaseResult entries (which would bucket under EvalSetID=="" and cause
// SaveEvalSetResult to fail silently).
func TestEvaluatePartialFailureNoZeroResults(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}},
	})

	// Two inferences: one references the existing case, the other references
	// a missing case so evaluateSingle returns an error.
	inf := []service.InferenceResult{
		{AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1", Status: service.InferenceStatusSuccess, Inferences: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
		}}},
		{AppName: "app", EvalSetID: "set1", EvalCaseID: "missing", SessionID: "s2", Status: service.InferenceStatusSuccess, Inferences: []models.Invocation{}},
	}

	svc := &service.LocalEvalService{
		Sets:        sets,
		Registry:    metrics.DefaultRegistry,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	results, _ := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricResponseMatchScore,
				Threshold:  0.5,
			}},
		},
	})
	// Missing eval case yields a FAILED result; persistence/save errors may
	// also populate err without dropping the successful case.
	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per inference), got %d: %+v", len(results), results)
	}
	if results[0].EvalID != "case1" || results[0].FinalEvalStatus != models.EvalStatusPassed {
		t.Fatalf("case1 result = %+v", results[0])
	}
	if results[1].EvalID != "missing" || results[1].FinalEvalStatus != models.EvalStatusFailed {
		t.Fatalf("missing case result = %+v", results[1])
	}
}

// TestEvaluateSingleTurnRubricPopulatesOverallDetails is the wire-side
// regression guard for the "rubric: []" bug: computeMetricResults populates
// EvalMetricResult.Details.RubricScores only when the evaluator surfaces
// OverallRubricScores. Before the aggregatePerInvocationRubrics fix,
// single-turn rubric evaluators (final_response, tool_use) left it nil.
func TestEvaluateSingleTurnRubricPopulatesOverallDetails(t *testing.T) {
	ctx := context.Background()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID: "case1",
		Conversation: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		}},
	})

	textProp := "be helpful"
	rubric := models.Rubric{
		RubricID:      "r1",
		RubricContent: models.RubricContent{TextProperty: &textProp},
	}
	reg := metrics.NewDefaultRegistry()
	reg.SetConfig(metrics.RegistryConfig{
		JudgeClient: stubRubricJudge{response: "Property: be helpful\nRationale: ok\nVerdict: yes\n"},
	})

	svc := &service.LocalEvalService{
		Sets:        sets,
		Registry:    reg,
		SimProvider: simulation.UserSimulatorProvider{},
	}
	inf := []service.InferenceResult{{
		AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1",
		Status: service.InferenceStatusSuccess,
		Inferences: []models.Invocation{{
			UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
			FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		}},
	}}
	results, err := svc.Evaluate(ctx, service.EvaluateRequest{
		InferenceResults: inf,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: []models.EvalMetric{{
				MetricName: models.MetricRubricBasedFinalResponseQualityV1,
				Threshold:  0.5,
				Criterion: models.CriterionField(models.RubricsBasedCriterion{
					BaseCriterion: models.BaseCriterion{Threshold: 0.5},
					Rubrics:       []models.Rubric{rubric},
				}),
			}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	overall := results[0].OverallEvalMetricResults
	if len(overall) != 1 {
		t.Fatalf("overall = %+v", overall)
	}
	if overall[0].Details == nil {
		t.Fatal("overall.Details is nil; wire renders rubric: [] (regression)")
	}
	if len(overall[0].Details.RubricScores) != 1 {
		t.Fatalf("overall.Details.RubricScores = %+v", overall[0].Details.RubricScores)
	}
	got := overall[0].Details.RubricScores[0]
	if got.RubricID != "r1" || got.Score == nil || *got.Score != 1.0 {
		t.Fatalf("rubric score = %+v", got)
	}
}

// TestRunInferenceOverridesStoredUserAndApp checks that inference runs as the
// request's user on the runtime's app, whatever the stored case says.
func TestRunInferenceOverridesStoredUserAndApp(t *testing.T) {
	ctx := context.Background()
	sessSvc := session.InMemoryService()
	const sessionID = generator.EvalSessionIDPrefix + "override"

	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID:       "case1",
		SessionInput: &models.SessionInput{UserID: "someone", AppName: "other"},
		Conversation: []models.Invocation{{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)}},
	})

	a, err := agent.New(agent.Config{
		Name: "app",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "app"
				ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: sessSvc}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	svc := &service.LocalEvalService{
		Generator:    &generator.Generator{Runtime: rt},
		Sets:         sets,
		SimProvider:  simulation.UserSimulatorProvider{},
		Sessions:     sessSvc,
		NewSessionID: func() string { return sessionID },
	}

	inf, err := svc.PerformInference(ctx, service.InferenceRequest{AppName: "app", EvalSetID: "set1", UserID: "caller"})
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	if len(inf) != 1 || inf[0].Status != service.InferenceStatusSuccess {
		t.Fatalf("inf = %+v", inf)
	}
	if inf[0].UserID != "caller" {
		t.Errorf("InferenceResult.UserID = %q, want caller", inf[0].UserID)
	}

	if resp, err := sessSvc.Get(ctx, &session.GetRequest{AppName: "app", UserID: "caller", SessionID: sessionID}); err != nil || resp == nil || resp.Session == nil {
		t.Errorf("no inference session under app %q user %q: %v", "app", "caller", err)
	}
	for _, other := range []struct{ app, user string }{{"app", "someone"}, {"other", "someone"}, {"other", "caller"}} {
		if resp, err := sessSvc.Get(ctx, &session.GetRequest{AppName: other.app, UserID: other.user, SessionID: sessionID}); err == nil && resp != nil && resp.Session != nil {
			t.Errorf("inference session found under app %q user %q, want none", other.app, other.user)
		}
	}

	stored, err := sets.GetEvalCase("app", "set1", "case1")
	if err != nil || stored == nil || stored.SessionInput == nil || stored.SessionInput.UserID != "someone" || stored.SessionInput.AppName != "other" {
		t.Errorf("stored case changed: %+v (err %v)", stored, err)
	}
}

func twoTurnSet(t *testing.T) storage.EvalSetsManager {
	t.Helper()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	turn := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
	}
	_ = sets.AddEvalCase("app", "set1", models.EvalCase{EvalID: "case1", Conversation: []models.Invocation{turn, turn}})
	return sets
}

// TestEvaluateFailedCaseCarriesUserID covers the evaluate error path: adk-python
// sets user_id on every case result, failures included, and adk-web loads the
// eval session under it.
func TestEvaluateFailedCaseCarriesUserID(t *testing.T) {
	svc := &service.LocalEvalService{Sets: twoTurnSet(t), Registry: metrics.DefaultRegistry}
	turn := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
	}
	// One actual invocation for a two-turn case: evaluation errors.
	results, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{
			AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1",
			UserID: "user-7", Inferences: []models.Invocation{turn}, Status: service.InferenceStatusSuccess,
		}},
		EvaluateConfig: service.EvaluateConfig{EvalMetrics: []models.EvalMetric{{MetricName: models.MetricResponseMatchScore, Threshold: 0.7}}},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(results) != 1 || results[0].FinalEvalStatus != models.EvalStatusFailed {
		t.Fatalf("results = %+v, want one FAILED result", results)
	}
	if results[0].UserID == nil || *results[0].UserID != "user-7" {
		t.Errorf("failed result UserID = %v, want user-7", results[0].UserID)
	}
}

// TestSavedEvalResultIDHasAppSetTimestampShape pins the result ID adk-web
// rebuilds from a deep link: ${app}_${evalSetId}_${timestamp}, with Go's
// integer timestamp.
func TestSavedEvalResultIDHasAppSetTimestampShape(t *testing.T) {
	results := storage.NewLocalEvalSetResultsManager(t.TempDir())
	svc := &service.LocalEvalService{Sets: twoTurnSet(t), Registry: metrics.DefaultRegistry, Results: results}
	turn := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("ok", genai.RoleModel),
	}
	if _, err := svc.Evaluate(context.Background(), service.EvaluateRequest{
		InferenceResults: []service.InferenceResult{{
			AppName: "app", EvalSetID: "set1", EvalCaseID: "case1", SessionID: "s1",
			UserID: "user-7", Inferences: []models.Invocation{turn, turn}, Status: service.InferenceStatusSuccess,
		}},
		EvaluateConfig: service.EvaluateConfig{EvalMetrics: []models.EvalMetric{{MetricName: models.MetricResponseMatchScore, Threshold: 0.7}}},
	}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	ids, err := results.ListEvalSetResults("app")
	if err != nil || len(ids) != 1 {
		t.Fatalf("ListEvalSetResults = %v, %v; want one", ids, err)
	}
	if !regexp.MustCompile(`^app_set1_[0-9]+$`).MatchString(ids[0]) {
		t.Errorf("result ID = %q, want app_set1_<integer seconds>", ids[0])
	}
	saved, err := results.GetEvalSetResult("app", ids[0])
	if err != nil || saved == nil {
		t.Fatalf("GetEvalSetResult: %v", err)
	}
	if want := "app_set1_" + strconv.FormatInt(int64(saved.CreationTimestamp), 10); ids[0] != want {
		t.Errorf("result ID = %q, want %q (app, set and creationTimestamp)", ids[0], want)
	}
}

// recordingSessions records the user of every session call it passes on.
type recordingSessions struct {
	session.Service
	mu    sync.Mutex
	users []string
}

func (r *recordingSessions) record(user string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, user)
}

func (r *recordingSessions) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	r.record(req.UserID)
	return r.Service.Get(ctx, req)
}

func (r *recordingSessions) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	r.record(req.UserID)
	return r.Service.Create(ctx, req)
}

func (r *recordingSessions) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	r.record(req.UserID)
	return r.Service.List(ctx, req)
}

func (r *recordingSessions) AppendEvent(ctx context.Context, s session.Session, ev *session.Event) error {
	r.record(s.UserID())
	return r.Service.AppendEvent(ctx, s, ev)
}

// pinnedService is a service over set1 with one static case, case1, whose
// session input is input; its sessions are sessions (nil for none).
func pinnedService(t *testing.T, sessions session.Service, input *models.SessionInput) *service.LocalEvalService {
	t.Helper()
	sets := storage.NewInMemoryEvalSetsManager()
	_, _ = sets.CreateEvalSet("app", "set1")
	if err := sets.AddEvalCase("app", "set1", models.EvalCase{
		EvalID:       "case1",
		SessionInput: input,
		Conversation: []models.Invocation{{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)}},
	}); err != nil {
		t.Fatalf("AddEvalCase: %v", err)
	}
	a, err := agent.New(agent.Config{Name: "app", Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			ev := session.NewEvent(ctx, ctx.InvocationID())
			ev.Author = "app"
			ev.Content = genai.NewContentFromText("ok", genai.RoleModel)
			yield(ev, nil)
		}
	}})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	runnerSessions := sessions
	if runnerSessions == nil {
		runnerSessions = session.InMemoryService()
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: runnerSessions}, "app")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return &service.LocalEvalService{Generator: &generator.Generator{Runtime: rt}, Sets: sets, Registry: metrics.NewDefaultRegistry(), SimProvider: simulation.UserSimulatorProvider{}, Sessions: sessions}
}

// prepareSession creates session fixed under user with state and one user
// event, "earlier turn".
func prepareSession(t *testing.T, sessions session.Service, user string, state map[string]any) {
	t.Helper()
	ctx := context.Background()
	resp, err := sessions.Create(ctx, &session.CreateRequest{AppName: "app", UserID: user, SessionID: "fixed", State: state})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ev := session.NewEvent(ctx, "earlier")
	ev.Author = "user"
	ev.Content = genai.NewContentFromText("earlier turn", genai.RoleUser)
	if err := sessions.AppendEvent(ctx, resp.Session, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
}

func getSession(t *testing.T, sessions session.Service, user string) session.Session {
	t.Helper()
	resp, err := sessions.Get(context.Background(), &session.GetRequest{AppName: "app", UserID: user, SessionID: "fixed"})
	if err != nil || resp == nil {
		return nil
	}
	return resp.Session
}

func inferOne(t *testing.T, svc *service.LocalEvalService, req service.InferenceRequest) []service.InferenceResult {
	t.Helper()
	req.AppName, req.EvalSetID = "app", "set1"
	inf, err := svc.PerformInference(context.Background(), req)
	if err != nil {
		t.Fatalf("PerformInference: %v", err)
	}
	for _, r := range inf {
		if r.Status != service.InferenceStatusSuccess {
			t.Fatalf("inference = %+v", r)
		}
	}
	return inf
}

// Acceptance 7, first half; mirrors adk-python test_evaluation_generator.py:1159.
func TestPinnedSessionOfCallerIsReused(t *testing.T) {
	sessions := session.InMemoryService()
	prepareSession(t, sessions, "caller", map[string]any{"prepared_by": "caller"})
	svc := pinnedService(t, sessions, &models.SessionInput{SessionID: "fixed", State: map[string]any{"prepared_by": "case"}})
	inf := inferOne(t, svc, service.InferenceRequest{UserID: "caller"})
	if inf[0].SessionID != "fixed" {
		t.Errorf("session ID = %q, want fixed", inf[0].SessionID)
	}
	s := getSession(t, sessions, "caller")
	if s == nil {
		t.Fatal("pinned session gone")
	}
	if v, _ := s.State().Get("prepared_by"); v != "caller" {
		t.Errorf("prepared_by = %v, want caller (the case state must not overwrite it)", v)
	}
	if s.Events().Len() < 3 || s.Events().At(0).Content.Parts[0].Text != "earlier turn" {
		t.Errorf("events = %d, first %+v; want the earlier turn kept and the new turn added", s.Events().Len(), s.Events().At(0).Content)
	}
}

// Acceptance 7, second half: a case naming another user's session never
// reads or writes it; the caller gets a fresh session of that ID.
func TestPinnedSessionOfAnotherUserIsNeverRead(t *testing.T) {
	inner := session.InMemoryService()
	prepareSession(t, inner, "other-user", map[string]any{"secret": "s"})
	rec := &recordingSessions{Service: inner}
	svc := pinnedService(t, rec, &models.SessionInput{UserID: "other-user", SessionID: "fixed", State: map[string]any{"prepared_by": "case"}})
	inf := inferOne(t, svc, service.InferenceRequest{UserID: "caller"})
	// Scoring reads the run's session back; that must be the caller's too.
	if _, err := svc.Evaluate(context.Background(), service.EvaluateRequest{InferenceResults: inf, EvaluateConfig: service.EvaluateConfig{
		EvalMetrics: []models.EvalMetric{{MetricName: "response_match_score", Threshold: 0.1}},
	}}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for _, u := range rec.users {
		if u == "other-user" {
			t.Fatalf("session calls were made as other-user: %v", rec.users)
		}
	}
	if s := getSession(t, inner, "other-user"); s == nil || s.Events().Len() != 1 {
		t.Errorf("other-user's session changed")
	}
	s := getSession(t, inner, "caller")
	if s == nil {
		t.Fatal("no session fixed under the caller")
	}
	if v, _ := s.State().Get("prepared_by"); v != "case" {
		t.Errorf("prepared_by = %v, want the case's state on the new session", v)
	}
	if v, err := s.State().Get("secret"); err == nil && v != nil {
		t.Errorf("caller's session has other-user's secret")
	}
}

// The agenteval path has no caller: the case's own user is looked up.
func TestPinnedSessionWithoutCallerUsesCaseUser(t *testing.T) {
	sessions := session.InMemoryService()
	prepareSession(t, sessions, "u", map[string]any{"prepared_by": "caller"})
	svc := pinnedService(t, sessions, &models.SessionInput{UserID: "u", SessionID: "fixed", State: map[string]any{"prepared_by": "case"}})
	inf := inferOne(t, svc, service.InferenceRequest{})
	if inf[0].SessionID != "fixed" {
		t.Errorf("session ID = %q, want fixed", inf[0].SessionID)
	}
	if v, _ := getSession(t, sessions, "u").State().Get("prepared_by"); v != "caller" {
		t.Errorf("prepared_by = %v, want caller", v)
	}
}

func TestPinnedSessionWithoutSessionService(t *testing.T) {
	svc := pinnedService(t, nil, &models.SessionInput{SessionID: "fixed"})
	if inf := inferOne(t, svc, service.InferenceRequest{UserID: "caller"}); inf[0].SessionID != "fixed" {
		t.Errorf("session ID = %q, want fixed", inf[0].SessionID)
	}
}

// Mirrors adk-python test_local_eval_service.py:315.
func TestNumRunsDropsPinnedSession(t *testing.T) {
	svc := pinnedService(t, session.InMemoryService(), &models.SessionInput{SessionID: "fixed"})
	inf := inferOne(t, svc, service.InferenceRequest{UserID: "caller", InferenceConfig: service.InferenceConfig{NumRuns: 2}})
	if len(inf) != 2 || inf[0].SessionID == "fixed" || inf[1].SessionID == "fixed" || inf[0].SessionID == inf[1].SessionID {
		t.Errorf("session IDs = %q, %q; want two fresh, different ones", inf[0].SessionID, inf[len(inf)-1].SessionID)
	}
}
