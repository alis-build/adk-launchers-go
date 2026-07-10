package metrics

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/genai"
)

// fakeJudge is a deterministic JudgeClient that returns preset responses in
// order and records every prompt it sees.
type fakeJudge struct {
	mu        sync.Mutex
	responses []string
	err       error
	prompts   []string
	calls     int
}

func (f *fakeJudge) GenerateJudgeResponse(_ context.Context, prompt string, _ models.JudgeModelOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return "", f.err
	}
	if len(f.responses) == 0 {
		return "", errors.New("fakeJudge: no more responses")
	}
	if f.calls >= len(f.responses) {
		return f.responses[len(f.responses)-1], nil
	}
	resp := f.responses[f.calls]
	f.calls++
	return resp, nil
}

func stringPtr(s string) *string { return &s }

func rubric(id, text string) models.Rubric {
	return models.Rubric{
		RubricID:      id,
		RubricContent: models.RubricContent{TextProperty: stringPtr(text)},
	}
}

func rubricMetric(name string, threshold float64, rubrics []models.Rubric) models.EvalMetric {
	return models.EvalMetric{
		MetricName: name,
		Threshold:  threshold,
		Criterion: models.CriterionField(models.RubricsBasedCriterion{
			BaseCriterion: models.BaseCriterion{Threshold: threshold},
			Rubrics:       rubrics,
		}),
	}
}

// 1. Prompt-shape: the final-response prompt includes each rubric text plus the
// actual user and final-response strings.
func TestRubricFinalResponsePromptShape(t *testing.T) {
	rubrics := []models.Rubric{
		rubric("r1", "be helpful"),
		rubric("r2", "be concise"),
	}
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: ok\nVerdict: yes\n\nProperty: be concise\nRationale: ok\nVerdict: no\n",
	}}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, rubrics))
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}

	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("what is the weather?", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("it is sunny", genai.RoleModel),
	}
	expected := models.Invocation{
		UserContent:   genai.NewContentFromText("what is the weather?", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("sunny", genai.RoleModel),
	}
	if _, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil); err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}

	if len(fake.prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(fake.prompts))
	}
	p := fake.prompts[0]
	for _, want := range []string{
		"be helpful",
		"be concise",
		"what is the weather?",
		"it is sunny",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q\nprompt:\n%s", want, p)
		}
	}
}

// 2. Parser happy path: two rubrics parse to 1.0/0.0 with rationales, mean 0.5.
func TestParseRubricVerdictsHappyPath(t *testing.T) {
	rubrics := []models.Rubric{
		rubric("r1", "be helpful"),
		rubric("r2", "be concise"),
	}
	resp := `Property: be helpful
Rationale: The answer addressed the request.
Verdict: yes

Property: be concise
Rationale: A bit long.
Verdict: no
`
	mean, scores, err := parseRubricVerdicts(resp, rubrics)
	if err != nil {
		t.Fatalf("parseRubricVerdicts: %v", err)
	}
	if math.Abs(mean-0.5) > 1e-9 {
		t.Fatalf("mean = %v, want 0.5", mean)
	}
	if len(scores) != 2 {
		t.Fatalf("scores = %d, want 2", len(scores))
	}
	if scores[0].RubricID != "r1" || scores[0].Score == nil || *scores[0].Score != 1.0 {
		t.Fatalf("r1 score = %+v", scores[0])
	}
	if scores[1].RubricID != "r2" || scores[1].Score == nil || *scores[1].Score != 0.0 {
		t.Fatalf("r2 score = %+v", scores[1])
	}
	if scores[0].Rationale == nil || *scores[0].Rationale != "The answer addressed the request." {
		t.Fatalf("r1 rationale = %+v", scores[0].Rationale)
	}
	if scores[1].Rationale == nil || *scores[1].Rationale != "A bit long." {
		t.Fatalf("r2 rationale = %+v", scores[1].Rationale)
	}
}

// 3. Off-format reply: parser rejects prose that has no Property/Verdict lines.
func TestParseRubricVerdictsOffFormat(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	_, _, err := parseRubricVerdicts("# Sorry, I don't have an answer.", rubrics)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// 4. End-to-end: llmJudgeEvaluator returns PerInvocationResult with populated
// RubricScores and correct mean when the judge returns a valid response.
func TestRubricEvaluatorEndToEnd(t *testing.T) {
	rubrics := []models.Rubric{
		rubric("r1", "be helpful"),
		rubric("r2", "be concise"),
	}
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: ok\nVerdict: yes\n\nProperty: be concise\nRationale: too long\nVerdict: no\n",
	}}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, rubrics))
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
	}
	expected := models.Invocation{FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel)}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if result.OverallScore == nil || math.Abs(*result.OverallScore-0.5) > 1e-9 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
	if len(result.PerInvocationResults) != 1 {
		t.Fatalf("per = %d", len(result.PerInvocationResults))
	}
	if len(result.PerInvocationResults[0].RubricScores) != 2 {
		t.Fatalf("rubric scores = %+v", result.PerInvocationResults[0].RubricScores)
	}
}

// 5. Unknown rubric IDs: judge responds about a property not in the rubric
// list; parser skips it and known rubrics still score.
func TestParseRubricVerdictsUnknownProperty(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	resp := `Property: be helpful
Rationale: ok
Verdict: yes

Property: not in list
Rationale: n/a
Verdict: no
`
	mean, scores, err := parseRubricVerdicts(resp, rubrics)
	if err != nil {
		t.Fatalf("parseRubricVerdicts: %v", err)
	}
	if mean != 1.0 {
		t.Fatalf("mean = %v, want 1.0", mean)
	}
	if len(scores) != 1 || scores[0].RubricID != "r1" {
		t.Fatalf("scores = %+v", scores)
	}
}

// 6. AppDetails-populated prompt: developer instructions and tool declarations
// appear in the rendered prompt.
func TestRubricFinalResponsePromptUsesAppDetails(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: ok\nVerdict: yes\n",
	}}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, rubrics))
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}

	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		IntermediateData: models.InvocationEventsField(models.InvocationEvents{
			InvocationEvents: []models.InvocationEvent{{Author: "root"}},
		}),
		AppDetails: &models.AppDetails{
			AgentDetails: map[string]models.AgentDetails{
				"root": {
					Name:         "root",
					Instructions: "Be helpful.",
					ToolDeclarations: []*genai.Tool{
						{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup", Description: "look things up"}}},
					},
				},
			},
		},
	}
	expected := models.Invocation{FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel)}
	if _, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil); err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(fake.prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(fake.prompts))
	}
	p := fake.prompts[0]
	if !strings.Contains(p, "Be helpful.") {
		t.Fatalf("prompt missing developer instructions:\n%s", p)
	}
	if !strings.Contains(p, "lookup") {
		t.Fatalf("prompt missing tool declaration:\n%s", p)
	}
	if strings.Contains(p, "Agent has no tools.") {
		t.Fatalf("prompt used fallback despite AppDetails being populated:\n%s", p)
	}
}

// 7. Majority-vote aggregation: three samples per invocation with mixed
// verdicts collapse to the per-rubric majority.
func TestMajorityVoteAggregation(t *testing.T) {
	rubrics := []models.Rubric{
		rubric("r1", "be helpful"),
		rubric("r2", "be concise"),
	}
	// Three samples:
	//  Sample 1: r1=yes, r2=no
	//  Sample 2: r1=no,  r2=no
	//  Sample 3: r1=yes, r2=yes
	// Expected majority: r1=yes (2 vs 1), r2=no (2 vs 1). Mean = 0.5.
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: r1s1\nVerdict: yes\n\nProperty: be concise\nRationale: r2s1\nVerdict: no\n",
		"Property: be helpful\nRationale: r1s2\nVerdict: no\n\nProperty: be concise\nRationale: r2s2\nVerdict: no\n",
		"Property: be helpful\nRationale: r1s3\nVerdict: yes\n\nProperty: be concise\nRationale: r2s3\nVerdict: yes\n",
	}}
	metric := rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, rubrics)
	// Inject NumSamples via LlmJudgeCriterion-shaped criterion.
	metric.Criterion = models.CriterionField(models.RubricsBasedCriterion{
		BaseCriterion:     models.BaseCriterion{Threshold: 0.5},
		JudgeModelOptions: models.JudgeModelOptions{NumSamples: 3},
		Rubrics:           rubrics,
	})
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
	}
	expected := models.Invocation{FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel)}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(result.PerInvocationResults) != 1 {
		t.Fatalf("per = %d", len(result.PerInvocationResults))
	}
	rs := result.PerInvocationResults[0].RubricScores
	byID := make(map[string]float64)
	for _, r := range rs {
		if r.Score != nil {
			byID[r.RubricID] = *r.Score
		}
	}
	if byID["r1"] != 1.0 {
		t.Fatalf("r1 = %v, want 1.0", byID["r1"])
	}
	if byID["r2"] != 0.0 {
		t.Fatalf("r2 = %v, want 0.0", byID["r2"])
	}
	if result.OverallScore == nil || math.Abs(*result.OverallScore-0.5) > 1e-9 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}

// 8. Per-turn simulator parser: valid/invalid/almost/prose paths.
func TestParseIsValidJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{"true", `{"is_valid": true, "criteria":[]}`, 1.0, false},
		{"false", `{"is_valid": false}`, 0.0, false},
		{"valid string", `{"is_valid": "valid"}`, 1.0, false},
		{"almost", `{"is_valid": "almost"}`, 0.0, false},
		{"partially", `{"is_valid": "partially"}`, 0.0, false},
		{"prose", `Sorry, I don't have an answer.`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIsValidJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseIsValidJSON: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// 9. Per-turn simulator full flow: turn 1 is deterministic; intermediate turns
// and stop-signal turn each hit the judge.
func TestPerTurnSimulatorFlowAllPass(t *testing.T) {
	fake := &fakeJudge{responses: []string{
		`{"is_valid": true}`,
		`{"is_valid": true}`,
		`{"is_valid": true}`, // stop-signal turn
	}}
	metric := models.EvalMetric{
		MetricName: models.MetricPerTurnUserSimulatorQualityV1,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.LlmBackedUserSimulatorCriterion{
			LlmAsAJudgeCriterion: models.LlmAsAJudgeCriterion{
				BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			},
			StopSignal: "</finished>",
		}),
	}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}

	scenario := &models.ConversationScenario{
		StartingPrompt:   "hi",
		ConversationPlan: "ask two questions",
	}
	actual := []models.Invocation{
		{UserContent: genai.NewContentFromText("hi", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("what is the weather?", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("thanks", genai.RoleUser)},
	}
	result, err := ev.EvaluateInvocations(context.Background(), actual, nil, scenario)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if fake.calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 intermediate + 1 stop-signal)", fake.calls)
	}
	if len(result.PerInvocationResults) != 3 {
		t.Fatalf("per = %d, want 3", len(result.PerInvocationResults))
	}
	if s := result.PerInvocationResults[0].Score; s == nil || *s != 1.0 {
		t.Fatalf("turn1 score = %+v", s)
	}
	if result.OverallScore == nil || *result.OverallScore != 1.0 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
	if result.OverallEvalStatus != models.EvalStatusPassed {
		t.Fatalf("status = %v", result.OverallEvalStatus)
	}
}

// 10. Per-turn simulator stop-signal failure overwrites the last real turn.
func TestPerTurnSimulatorStopSignalOverwrite(t *testing.T) {
	fake := &fakeJudge{responses: []string{
		`{"is_valid": true}`,
		`{"is_valid": true}`,
		`{"is_valid": false}`, // stop-signal turn fails
	}}
	metric := models.EvalMetric{
		MetricName: models.MetricPerTurnUserSimulatorQualityV1,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.LlmBackedUserSimulatorCriterion{
			LlmAsAJudgeCriterion: models.LlmAsAJudgeCriterion{
				BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			},
			StopSignal: "</finished>",
		}),
	}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	scenario := &models.ConversationScenario{StartingPrompt: "hi", ConversationPlan: "plan"}
	actual := []models.Invocation{
		{UserContent: genai.NewContentFromText("hi", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("q1", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("q2", genai.RoleUser)},
	}
	result, err := ev.EvaluateInvocations(context.Background(), actual, nil, scenario)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	last := result.PerInvocationResults[len(result.PerInvocationResults)-1]
	if last.EvalStatus != models.EvalStatusFailed {
		t.Fatalf("last status = %v, want FAILED", last.EvalStatus)
	}
	// Overall = num_valid / num_evaluated: turn1(pass, 1.0) + turn2(pass, 1.0) + turn3(fail overwrite, 0.0) => 2/3.
	if result.OverallScore == nil || math.Abs(*result.OverallScore-2.0/3.0) > 1e-9 {
		t.Fatalf("overall = %+v", result.OverallScore)
	}
}

// Majority-vote aggregator recomputes EvalStatus from the aggregated score so
// a [Failed, Passed, Passed] sample set reports Passed (not the sample[0]
// status). Guards against the code-review Critical #1 regression.
func TestMajorityVoteRecomputesEvalStatus(t *testing.T) {
	failed := func() PerInvocationResult {
		score := 0.0
		return PerInvocationResult{
			RubricScores: []models.RubricScore{{RubricID: "r1", Score: &score}},
			Score:        &score,
			EvalStatus:   models.EvalStatusFailed,
		}
	}
	passed := func() PerInvocationResult {
		score := 1.0
		return PerInvocationResult{
			RubricScores: []models.RubricScore{{RubricID: "r1", Score: &score}},
			Score:        &score,
			EvalStatus:   models.EvalStatusPassed,
		}
	}
	out := majorityVoteAggregate([]PerInvocationResult{failed(), passed(), passed()}, 0.5)
	if out.Score == nil || *out.Score != 1.0 {
		t.Fatalf("score = %+v", out.Score)
	}
	if out.EvalStatus != models.EvalStatusPassed {
		t.Fatalf("status = %v, want Passed (fix recomputes from aggregated score)", out.EvalStatus)
	}
}

// Verdict matching rejects false-positive prefixes and substrings ("yesterday"
// contains "yes", "not applicable" contains "no"). Guards against the
// code-review Warning #4 regression.
func TestParseRubricVerdictsExactMatch(t *testing.T) {
	rubrics := []models.Rubric{
		rubric("r1", "prop yes"),
		rubric("r2", "prop no"),
	}
	resp := `Property: prop yes
Rationale: n/a
Verdict: yesterday

Property: prop no
Rationale: n/a
Verdict: not applicable
`
	// Both verdicts should be unparseable (no exact yes/no match), leaving
	// counted=0 and returning an error.
	_, _, err := parseRubricVerdicts(resp, rubrics)
	if err == nil {
		t.Fatal("expected parse error: 'yesterday'/'not applicable' must not count as verdicts")
	}
}

// Verdict matching tolerates common wrapping punctuation such as "[[yes]]".
func TestParseRubricVerdictsBracketedVerdict(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	resp := `Property: be helpful
Rationale: ok
Verdict: [[yes]]
`
	mean, scores, err := parseRubricVerdicts(resp, rubrics)
	if err != nil {
		t.Fatalf("parseRubricVerdicts: %v", err)
	}
	if mean != 1.0 || len(scores) != 1 || scores[0].Score == nil || *scores[0].Score != 1.0 {
		t.Fatalf("mean = %v, scores = %+v", mean, scores)
	}
}

// Rubric evaluator threads per-invocation rubrics through the parser so
// judges scoring only invocation-scoped rubrics still populate RubricScores.
// Guards against the code-review Warning #3 regression.
func TestRubricEvaluatorHonorsInvocationRubrics(t *testing.T) {
	invRubric := rubric("inv-1", "be friendly")
	fake := &fakeJudge{responses: []string{
		"Property: be friendly\nRationale: ok\nVerdict: yes\n",
	}}
	// Empty criterion rubrics; the invocation carries the only rubric.
	metric := rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, nil)
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
		Rubrics:       []models.Rubric{invRubric},
	}
	expected := models.Invocation{FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel)}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(result.PerInvocationResults) != 1 {
		t.Fatalf("per = %d", len(result.PerInvocationResults))
	}
	rs := result.PerInvocationResults[0].RubricScores
	if len(rs) != 1 || rs[0].RubricID != "inv-1" || rs[0].Score == nil || *rs[0].Score != 1.0 {
		t.Fatalf("invocation rubric not threaded: %+v", rs)
	}
}

// Stop-signal overwrite preserves the last real turn's ActualInvocation while
// copying Score/EvalStatus from the synthetic stop-signal turn. Guards
// against the code-review Critical #2 regression.
func TestPerTurnSimulatorStopSignalPreservesActualInvocation(t *testing.T) {
	fake := &fakeJudge{responses: []string{
		`{"is_valid": true}`,
		`{"is_valid": true}`,
		`{"is_valid": false}`,
	}}
	metric := models.EvalMetric{
		MetricName: models.MetricPerTurnUserSimulatorQualityV1,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.LlmBackedUserSimulatorCriterion{
			LlmAsAJudgeCriterion: models.LlmAsAJudgeCriterion{
				BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			},
			StopSignal: "</finished>",
		}),
	}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	scenario := &models.ConversationScenario{StartingPrompt: "hi", ConversationPlan: "plan"}
	q2 := genai.NewContentFromText("q2", genai.RoleUser)
	actual := []models.Invocation{
		{InvocationID: "inv-1", UserContent: genai.NewContentFromText("hi", genai.RoleUser)},
		{InvocationID: "inv-2", UserContent: genai.NewContentFromText("q1", genai.RoleUser)},
		{InvocationID: "inv-3", UserContent: q2},
	}
	result, err := ev.EvaluateInvocations(context.Background(), actual, nil, scenario)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	last := result.PerInvocationResults[len(result.PerInvocationResults)-1]
	if last.ActualInvocation.InvocationID != "inv-3" {
		t.Fatalf("last invocation clobbered by stop-signal proxy: id=%q", last.ActualInvocation.InvocationID)
	}
	if last.EvalStatus != models.EvalStatusFailed {
		t.Fatalf("last status = %v, want Failed", last.EvalStatus)
	}
}

// 11. Per-turn simulator persona-variant prompt: persona description and
// behaviour names appear in the rendered prompt.
func TestPerTurnSimulatorPersonaPromptShape(t *testing.T) {
	fake := &fakeJudge{responses: []string{
		`{"is_valid": true}`,
		`{"is_valid": true}`,
	}}
	metric := models.EvalMetric{
		MetricName: models.MetricPerTurnUserSimulatorQualityV1,
		Threshold:  0.5,
		Criterion: models.CriterionField(models.LlmBackedUserSimulatorCriterion{
			LlmAsAJudgeCriterion: models.LlmAsAJudgeCriterion{
				BaseCriterion: models.BaseCriterion{Threshold: 0.5},
			},
			StopSignal: "</finished>",
		}),
	}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(metric)
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	scenario := &models.ConversationScenario{
		StartingPrompt:   "hi",
		ConversationPlan: "plan",
		UserPersona: &models.UserPersona{
			Description: "friendly retiree who prefers concise answers",
			Behaviors: []models.PersonaBehavior{
				{
					Name:             "Politeness",
					Description:      "The user greets the agent politely.",
					ViolationRubrics: []string{"The user is rude to the agent."},
				},
				{
					Name:             "Brevity",
					Description:      "The user asks short questions.",
					ViolationRubrics: []string{"The user writes long messages."},
				},
			},
		},
	}
	actual := []models.Invocation{
		{UserContent: genai.NewContentFromText("hi", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("q1", genai.RoleUser)},
	}
	if _, err := ev.EvaluateInvocations(context.Background(), actual, nil, scenario); err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(fake.prompts) < 1 {
		t.Fatalf("no prompts captured")
	}
	p := fake.prompts[0]
	for _, want := range []string{
		"friendly retiree",
		"Politeness",
		"Brevity",
		"The user is rude to the agent.",
		"The user writes long messages.",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q\nprompt:\n%s", want, p)
		}
	}
}

// Single-turn rubric evaluators must populate OverallRubricScores so
// downstream service.computeMetricResults can surface Details.RubricScores on
// the overall EvalMetricResult. Guards against the wire-side rubric: []
// regression.
func TestRubricEvaluatorPopulatesOverallRubricScoresSingleInvocation(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: ok\nVerdict: yes\n",
	}}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.5, rubrics))
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	actual := models.Invocation{
		UserContent:   genai.NewContentFromText("hi", genai.RoleUser),
		FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel),
	}
	expected := models.Invocation{FinalResponse: genai.NewContentFromText("hi!", genai.RoleModel)}
	result, err := ev.EvaluateInvocations(context.Background(), []models.Invocation{actual}, []models.Invocation{expected}, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(result.OverallRubricScores) != 1 {
		t.Fatalf("OverallRubricScores = %+v, want 1 entry", result.OverallRubricScores)
	}
	got := result.OverallRubricScores[0]
	if got.RubricID != "r1" || got.Score == nil || *got.Score != 1.0 {
		t.Fatalf("overall rubric = %+v", got)
	}
	if got.Rationale == nil || !strings.Contains(*got.Rationale, "aggregated score") {
		t.Fatalf("overall rationale = %+v", got.Rationale)
	}
}

// Two invocations scoring the same rubric 1.0 and 0.0 must aggregate to 0.5
// on the overall slot. Confirms the mean-across-invocations semantics.
func TestRubricEvaluatorAggregatesOverallRubricScoresMean(t *testing.T) {
	rubrics := []models.Rubric{rubric("r1", "be helpful")}
	fake := &fakeJudge{responses: []string{
		"Property: be helpful\nRationale: ok\nVerdict: yes\n",
		"Property: be helpful\nRationale: nope\nVerdict: no\n",
	}}
	reg := NewDefaultRegistry()
	reg.SetConfig(RegistryConfig{JudgeClient: fake})
	ev, err := reg.GetEvaluator(rubricMetric(models.MetricRubricBasedFinalResponseQualityV1, 0.4, rubrics))
	if err != nil {
		t.Fatalf("GetEvaluator: %v", err)
	}
	mkInv := func(text string) models.Invocation {
		return models.Invocation{
			UserContent:   genai.NewContentFromText("q", genai.RoleUser),
			FinalResponse: genai.NewContentFromText(text, genai.RoleModel),
		}
	}
	actual := []models.Invocation{mkInv("first"), mkInv("second")}
	expected := []models.Invocation{
		{FinalResponse: genai.NewContentFromText("first", genai.RoleModel)},
		{FinalResponse: genai.NewContentFromText("second", genai.RoleModel)},
	}
	result, err := ev.EvaluateInvocations(context.Background(), actual, expected, nil)
	if err != nil {
		t.Fatalf("EvaluateInvocations: %v", err)
	}
	if len(result.OverallRubricScores) != 1 {
		t.Fatalf("OverallRubricScores = %+v", result.OverallRubricScores)
	}
	got := result.OverallRubricScores[0]
	if got.Score == nil || math.Abs(*got.Score-0.5) > 1e-9 {
		t.Fatalf("overall r1 mean = %+v, want 0.5", got.Score)
	}
}

// aggregatePerInvocationRubrics preserves first-seen rubric ordering across
// invocations. Deterministic ordering matters because downstream renderers
// (e.g. the wire mapper in go.alis.build/evals) rely on stable indexing to
// correlate rubric entries across runs.
func TestAggregatePerInvocationRubricsOrdering(t *testing.T) {
	one := 1.0
	zero := 0.0
	per := []PerInvocationResult{
		{RubricScores: []models.RubricScore{
			{RubricID: "r2", Score: &one},
			{RubricID: "r1", Score: &zero},
		}},
		{RubricScores: []models.RubricScore{
			{RubricID: "r3", Score: &one},
			{RubricID: "r1", Score: &one},
		}},
	}
	got := aggregatePerInvocationRubrics(per)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantOrder := []string{"r2", "r1", "r3"}
	for i, id := range wantOrder {
		if got[i].RubricID != id {
			t.Fatalf("order[%d] = %q, want %q (full: %+v)", i, got[i].RubricID, id, got)
		}
	}
}

// Nil per-invocation rubric scores must be skipped so an unparseable verdict
// doesn't crash the aggregator, dilute the mean, or introduce zero-count
// divide-by-zero paths.
func TestAggregatePerInvocationRubricsSkipsNilScores(t *testing.T) {
	one := 1.0
	per := []PerInvocationResult{
		{RubricScores: []models.RubricScore{{RubricID: "r1", Score: &one}}},
		{RubricScores: []models.RubricScore{{RubricID: "r1", Score: nil}}},
		{RubricScores: []models.RubricScore{{RubricID: "r1", Score: &one}}},
	}
	got := aggregatePerInvocationRubrics(per)
	if len(got) != 1 || got[0].RubricID != "r1" {
		t.Fatalf("got = %+v", got)
	}
	if got[0].Score == nil || *got[0].Score != 1.0 {
		t.Fatalf("nil should have been skipped; score = %+v", got[0].Score)
	}

	// A rubric with only nil scores across all invocations must drop out of
	// the overall list entirely rather than surfacing a nil-score entry.
	perAllNil := []PerInvocationResult{
		{RubricScores: []models.RubricScore{{RubricID: "r1", Score: nil}}},
		{RubricScores: []models.RubricScore{{RubricID: "r1", Score: nil}}},
	}
	if out := aggregatePerInvocationRubrics(perAllNil); out != nil {
		t.Fatalf("all-nil case returned %+v, want nil", out)
	}
}
