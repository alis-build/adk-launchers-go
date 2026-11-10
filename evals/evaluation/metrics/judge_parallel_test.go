package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// gatedJudge is a JudgeClient whose calls block until released, counting how
// many are in flight at once. Calls whose prompt contains failOn fail.
type gatedJudge struct {
	inFlight, maxInFlight, calls atomic.Int32
	arrived                      chan struct{}
	release                      chan struct{}
	failOn                       string
	answer                       string
}

func newGatedJudge() *gatedJudge {
	return &gatedJudge{arrived: make(chan struct{}, 64), release: make(chan struct{}), answer: `{"is_the_agent_response_valid":"valid"}`}
}

func (g *gatedJudge) GenerateJudgeResponse(ctx context.Context, prompt string, _ models.JudgeModelOptions) (string, error) {
	g.calls.Add(1)
	n := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		m := g.maxInFlight.Load()
		if n <= m || g.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	g.arrived <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if g.failOn != "" && strings.Contains(prompt, g.failOn) {
		return "", errors.New("judge unavailable")
	}
	return g.answer, nil
}

func (g *gatedJudge) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-g.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a judge call")
	}
}

func (g *gatedJudge) releaseOne(t *testing.T) {
	t.Helper()
	select {
	case g.release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out releasing a judge call")
	}
}

// judgeEvaluator returns the registry's evaluator for metric JSON, judged by c.
func judgeEvaluator(t *testing.T, c JudgeClient, metricJSON string) (Evaluator, error) {
	t.Helper()
	var m models.EvalMetric
	if err := json.Unmarshal([]byte(metricJSON), &m); err != nil {
		t.Fatalf("decode metric: %v", err)
	}
	r := NewDefaultRegistry()
	r.SetConfig(RegistryConfig{JudgeClient: c})
	return r.GetEvaluator(m)
}

func judgedInvocations(users ...string) (actual, expected []models.Invocation) {
	for _, u := range users {
		inv := models.Invocation{UserContent: genai.NewContentFromText(u, genai.RoleUser), FinalResponse: genai.NewContentFromText("answer", genai.RoleModel)}
		actual = append(actual, inv)
		expected = append(expected, inv)
	}
	return actual, expected
}

// evaluateInBackground runs e over invocations for users and returns a
// channel yielding its result.
func evaluateInBackground(e Evaluator, users ...string) <-chan EvaluationResult {
	actual, expected := judgedInvocations(users...)
	done := make(chan EvaluationResult, 1)
	go func() {
		res, _ := e.EvaluateInvocations(context.Background(), actual, expected, nil)
		done <- res
	}()
	return done
}

func waitResult(t *testing.T, done <-chan EvaluationResult) EvaluationResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the evaluation")
		return EvaluationResult{}
	}
}

// Acceptance 8; mirrors adk-python test_llm_as_judge.py:394.
func TestJudgeCallsOverlapUpToParallelismLimit(t *testing.T) {
	g := newGatedJudge()
	e, err := judgeEvaluator(t, g, `{"metricName":"final_response_match_v2","threshold":0.5,"criterion":{"threshold":0.5,"judgeModelOptions":{"numSamples":3,"parallelismLimit":2}}}`)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	done := evaluateInBackground(e, "u0", "u1")
	// Two calls fill the limit; then each release lets one more in.
	g.waitArrived(t)
	g.waitArrived(t)
	for i := range 6 {
		if n := g.inFlight.Load(); n > 2 {
			t.Fatalf("in flight = %d, want at most 2", n)
		}
		g.releaseOne(t)
		if i < 4 {
			g.waitArrived(t)
		}
	}
	res := waitResult(t, done)
	if got := g.maxInFlight.Load(); got != 2 {
		t.Errorf("max in flight = %d, want 2", got)
	}
	if got := g.calls.Load(); got != 6 {
		t.Errorf("calls = %d, want 6", got)
	}
	if len(res.PerInvocationResults) != 2 || res.OverallScore == nil || *res.OverallScore != 1 {
		t.Errorf("result = %+v, want two passing invocations", res)
	}
}

func TestJudgeParallelismLimitDefaultsToOne(t *testing.T) {
	g := newGatedJudge()
	e, err := judgeEvaluator(t, g, `{"metricName":"final_response_match_v2","threshold":0.5,"criterion":{"threshold":0.5,"judgeModelOptions":{"numSamples":3}}}`)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	done := evaluateInBackground(e, "u0", "u1")
	for range 6 {
		g.waitArrived(t)
		if n := g.inFlight.Load(); n != 1 {
			t.Errorf("in flight = %d, want 1", n)
		}
		g.releaseOne(t)
	}
	waitResult(t, done)
	if got, calls := g.maxInFlight.Load(), g.calls.Load(); got != 1 || calls != 6 {
		t.Errorf("max in flight = %d over %d calls, want 1 over 6", got, calls)
	}
}

// Mirrors adk-python test_llm_as_judge.py:445: a failed sample marks only
// its own invocation NOT_EVALUATED.
func TestJudgeSampleFailureMarksOnlyThatInvocation(t *testing.T) {
	g := newGatedJudge()
	g.failOn = "alpha-fails"
	close(g.release)
	e, err := judgeEvaluator(t, g, `{"metricName":"final_response_match_v2","threshold":0.5,"criterion":{"threshold":0.5,"judgeModelOptions":{"numSamples":2}}}`)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual, expected := judgedInvocations("alpha-fails", "beta-ok")
	res, err := e.EvaluateInvocations(context.Background(), actual, expected, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	p := res.PerInvocationResults
	if len(p) != 2 || p[0].Score != nil || p[0].EvalStatus != models.EvalStatusNotEvaluated ||
		p[1].Score == nil || *p[1].Score != 1 || p[1].EvalStatus != models.EvalStatusPassed {
		t.Errorf("per invocation = %+v, want [NOT_EVALUATED, PASSED 1.0]", p)
	}
}

// Mirrors adk-python test_llm_as_judge.py:548-551.
func TestJudgeRejectsParallelismLimitBelowOne(t *testing.T) {
	for _, metric := range []string{"final_response_match_v2", "rubric_based_final_response_quality_v1", "hallucinations_v1", "per_turn_user_simulator_quality_v1"} {
		for _, n := range []string{"0", "-1"} {
			_, err := judgeEvaluator(t, newGatedJudge(), `{"metricName":"`+metric+`","threshold":0.5,"criterion":{"threshold":0.5,"judgeModelOptions":{"parallelismLimit":`+n+`}}}`)
			if err == nil || !strings.Contains(err.Error(), "parallelismLimit must be at least 1") {
				t.Errorf("%s limit %s: error = %v", metric, n, err)
			}
		}
	}
}

// judgedTurn returns the user response a per-turn simulator prompt asks
// about: the text after its last "# Generated User Response" heading (the
// prompt also embeds the conversation history).
func judgedTurn(prompt string) string {
	_, after, _ := strings.Cut(prompt[strings.LastIndex(prompt, "# Generated User Response"):], "\n")
	return strings.TrimSpace(after)
}

// turnJudge answers a simulator judge prompt by the turn it asks about, and
// {"is_valid": true} for turns it does not name.
type turnJudge map[string]string

func (j turnJudge) GenerateJudgeResponse(_ context.Context, prompt string, _ models.JudgeModelOptions) (string, error) {
	if a, ok := j[judgedTurn(prompt)]; ok {
		return a, nil
	}
	return `{"is_valid": true}`, nil
}

// simulatorEvaluator returns the per-turn simulator evaluator for criterion
// JSON, judged by c.
func simulatorEvaluator(t *testing.T, c JudgeClient, criterion string) Evaluator {
	t.Helper()
	e, err := judgeEvaluator(t, c, `{"metricName":"per_turn_user_simulator_quality_v1","threshold":0.5,"criterion":`+criterion+`}`)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	return e
}

func simulatorTurns(texts ...string) []models.Invocation {
	var out []models.Invocation
	for _, s := range texts {
		out = append(out, models.Invocation{UserContent: genai.NewContentFromText(s, genai.RoleUser)})
	}
	return out
}

var simulatorScenario = &models.ConversationScenario{StartingPrompt: "start", ConversationPlan: "plan"}

// Mirrors adk-python test_per_turn_user_simulation_quality_v1.py:797.
func TestSimulatorJudgeCallsUpToParallelismLimit(t *testing.T) {
	g := newGatedJudge()
	g.answer = `{"is_valid": true}`
	e := simulatorEvaluator(t, g, `{"threshold":0.5,"stopSignal":"</finished>","judgeModelOptions":{"numSamples":2,"parallelismLimit":3}}`)
	done := make(chan EvaluationResult, 1)
	go func() {
		res, _ := e.EvaluateInvocations(context.Background(), simulatorTurns("start", "t1", "t2", "t3"), nil, simulatorScenario)
		done <- res
	}()
	// Three intermediate turns plus the stop-signal turn, two samples each.
	for range 3 {
		g.waitArrived(t)
	}
	for i := range 8 {
		if n := g.inFlight.Load(); n > 3 {
			t.Fatalf("in flight = %d, want at most 3", n)
		}
		g.releaseOne(t)
		if i < 5 {
			g.waitArrived(t)
		}
	}
	waitResult(t, done)
	if got, calls := g.maxInFlight.Load(), g.calls.Load(); got != 3 || calls != 8 {
		t.Errorf("max in flight = %d over %d calls, want 3 over 8", got, calls)
	}
}

// heldJudge holds every simulator judge call until the test releases it,
// reporting each call's turn on arrived.
type heldJudge struct {
	arrived chan heldCall
	answers turnJudge
}

type heldCall struct {
	turn    string
	release chan struct{}
}

func (j *heldJudge) GenerateJudgeResponse(ctx context.Context, prompt string, opts models.JudgeModelOptions) (string, error) {
	c := heldCall{turn: judgedTurn(prompt), release: make(chan struct{})}
	j.arrived <- c
	select {
	case <-c.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return j.answers.GenerateJudgeResponse(ctx, prompt, opts)
}

// Mirrors adk-python test_per_turn_user_simulation_quality_v1.py:867 for its
// out-of-order release; the scores are derived from the Go rules.
func TestSimulatorJudgeKeepsTurnOrder(t *testing.T) {
	j := &heldJudge{arrived: make(chan heldCall, 8), answers: turnJudge{"t1": `{"is_valid": false}`, "t3": `{"is_valid": false}`}}
	e := simulatorEvaluator(t, j, `{"threshold":0.5,"stopSignal":"</finished>","judgeModelOptions":{"parallelismLimit":4}}`)
	done := make(chan EvaluationResult, 1)
	go func() {
		res, _ := e.EvaluateInvocations(context.Background(), simulatorTurns("start", "t1", "t2", "t3"), nil, simulatorScenario)
		done <- res
	}()
	var calls []heldCall
	for range 4 {
		select {
		case c := <-j.arrived:
			calls = append(calls, c)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for judge calls")
		}
	}
	for i := len(calls) - 1; i >= 0; i-- {
		close(calls[i].release)
	}
	res := waitResult(t, done)
	want := []float64{1, 0, 1, 0}
	if len(res.PerInvocationResults) != len(want) {
		t.Fatalf("per invocation = %+v", res.PerInvocationResults)
	}
	for i, w := range want {
		if s := res.PerInvocationResults[i].Score; s == nil || *s != w {
			t.Errorf("turn %d score = %v, want %v", i, s, w)
		}
	}
}

// erringJudge fails the call for turn t1 once two other calls are waiting,
// and makes every other call wait for cancellation.
type erringJudge struct {
	waiting, cancelled atomic.Int32
	inFlight           atomic.Int32
}

func (j *erringJudge) GenerateJudgeResponse(ctx context.Context, prompt string, _ models.JudgeModelOptions) (string, error) {
	j.inFlight.Add(1)
	defer j.inFlight.Add(-1)
	if judgedTurn(prompt) == "t1" {
		for j.waiting.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		return "", errors.New("judge down")
	}
	j.waiting.Add(1)
	<-ctx.Done()
	j.cancelled.Add(1)
	return "", ctx.Err()
}

// Mirrors adk-python test_per_turn_user_simulation_quality_v1.py:1003.
func TestSimulatorJudgeCancelsSiblingsOnError(t *testing.T) {
	j := &erringJudge{}
	e := simulatorEvaluator(t, j, `{"threshold":0.5,"stopSignal":"</finished>","judgeModelOptions":{"parallelismLimit":3}}`)
	errc := make(chan error, 1)
	go func() {
		_, err := e.EvaluateInvocations(context.Background(), simulatorTurns("start", "t1", "t2"), nil, simulatorScenario)
		errc <- err
	}()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "judge down") {
			t.Errorf("error = %v, want the judge's error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation still running 5 s after a judge call failed")
	}
	if got, left := j.cancelled.Load(), j.inFlight.Load(); got != 2 || left != 0 {
		t.Errorf("cancelled = %d, in flight = %d; want 2 and 0", got, left)
	}
}
