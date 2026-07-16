package simulation_test

import (
	"context"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/genai"
)

func TestStaticUserSimulator(t *testing.T) {
	conv := []models.Invocation{
		{UserContent: genai.NewContentFromText("message 1", genai.RoleUser)},
		{UserContent: genai.NewContentFromText("message 2", genai.RoleUser)},
	}
	sim := simulation.NewStaticUserSimulator(conv)

	msg1, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg1.Status != simulation.StatusSuccess || msg1.UserMessage.Parts[0].Text != "message 1" {
		t.Fatalf("msg1 = %+v, err = %v", msg1, err)
	}
	msg2, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg2.UserMessage.Parts[0].Text != "message 2" {
		t.Fatalf("msg2 = %+v, err = %v", msg2, err)
	}
	msg3, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg3.Status != simulation.StatusStopSignalDetected {
		t.Fatalf("msg3 = %+v, err = %v", msg3, err)
	}
}

func TestUserSimulatorProviderStatic(t *testing.T) {
	p := simulation.UserSimulatorProvider{}
	c := models.EvalCase{
		EvalID:       "c1",
		Conversation: []models.Invocation{{UserContent: genai.NewContentFromText("hi", genai.RoleUser)}},
	}
	sim, err := p.Provide(c)
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	msg, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || msg.Status != simulation.StatusSuccess {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

func TestUserSimulatorProviderScenarioRequiresFactory(t *testing.T) {
	p := simulation.UserSimulatorProvider{}
	c := models.EvalCase{
		EvalID: "c1",
		ConversationScenario: &models.ConversationScenario{
			StartingPrompt:   "hi",
			ConversationPlan: "plan",
		},
	}
	_, err := p.Provide(c)
	if err == nil {
		t.Fatal("expected error without LLM factory")
	}
}
