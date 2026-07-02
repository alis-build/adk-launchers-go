package simulation_test

import (
	"context"
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type stubGenerator struct {
	responses []string
	calls     int
}

func (s *stubGenerator) GenerateContent(_ context.Context, _, _ string) (string, error) {
	if s.calls >= len(s.responses) {
		return "", nil
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

func TestLlmBackedUserSimulatorStartingPrompt(t *testing.T) {
	scenario := &models.ConversationScenario{
		StartingPrompt:   "Book a flight",
		ConversationPlan: "Ask for a flight to NYC",
	}
	sim, err := simulation.NewLlmBackedUserSimulator(simulation.LlmBackedUserSimulatorConfig{}, scenario, &stubGenerator{})
	if err != nil {
		t.Fatalf("NewLlmBackedUserSimulator: %v", err)
	}
	msg, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg.Status != simulation.StatusSuccess || msg.UserMessage.Parts[0].Text != "Book a flight" {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestLlmBackedUserSimulatorStopSignal(t *testing.T) {
	scenario := &models.ConversationScenario{
		StartingPrompt:   "hi",
		ConversationPlan: "plan",
	}
	gen := &stubGenerator{responses: []string{"done </finished>"}}
	sim, err := simulation.NewLlmBackedUserSimulator(simulation.LlmBackedUserSimulatorConfig{}, scenario, gen)
	if err != nil {
		t.Fatalf("NewLlmBackedUserSimulator: %v", err)
	}
	if _, err := sim.GetNextUserMessage(context.Background(), nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	msg, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg.Status != simulation.StatusStopSignalDetected {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestLlmBackedUserSimulatorTurnLimit(t *testing.T) {
	scenario := &models.ConversationScenario{StartingPrompt: "hi", ConversationPlan: "plan"}
	sim, err := simulation.NewLlmBackedUserSimulator(simulation.LlmBackedUserSimulatorConfig{
		MaxAllowedInvocations: 1,
	}, scenario, &stubGenerator{})
	if err != nil {
		t.Fatalf("NewLlmBackedUserSimulator: %v", err)
	}
	if _, err := sim.GetNextUserMessage(context.Background(), nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	msg, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg.Status != simulation.StatusTurnLimitReached {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestLlmBackedUserSimulatorLLMResponse(t *testing.T) {
	scenario := &models.ConversationScenario{StartingPrompt: "hi", ConversationPlan: "plan"}
	gen := &stubGenerator{responses: []string{"next question"}}
	sim, err := simulation.NewLlmBackedUserSimulator(simulation.LlmBackedUserSimulatorConfig{}, scenario, gen)
	if err != nil {
		t.Fatalf("NewLlmBackedUserSimulator: %v", err)
	}
	if _, err := sim.GetNextUserMessage(context.Background(), nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	ev := session.NewEvent(t.Context(), "inv1")
	ev.Author = "agent"
	ev.Content = genai.NewContentFromText("How can I help?", genai.RoleModel)
	msg, err := sim.GetNextUserMessage(context.Background(), []*session.Event{ev})
	if err != nil || msg.Status != simulation.StatusSuccess || msg.UserMessage.Parts[0].Text != "next question" {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestUserSimulatorProviderLlmBacked(t *testing.T) {
	factory := &simulation.DefaultLlmBackedFactory{
		Generator: &stubGenerator{responses: []string{"follow up"}},
	}
	p := simulation.UserSimulatorProvider{LLM: factory}
	c := models.EvalCase{
		EvalID: "c1",
		ConversationScenario: &models.ConversationScenario{
			StartingPrompt:   "start",
			ConversationPlan: "plan",
		},
	}
	sim, err := p.Provide(c)
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if _, err := sim.GetNextUserMessage(context.Background(), nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	msg, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg.UserMessage.Parts[0].Text != "follow up" {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestGetLlmBackedUserSimulatorPromptIncludesPlan(t *testing.T) {
	prompt, err := simulation.GetLlmBackedUserSimulatorPrompt("my plan", "agent: hi", "</finished>", "", "")
	if err != nil {
		t.Fatalf("GetLlmBackedUserSimulatorPrompt: %v", err)
	}
	if !strings.Contains(prompt, "my plan") || !strings.Contains(prompt, "agent: hi") {
		t.Fatalf("prompt missing fields: %q", prompt)
	}
}
