package generator

import (
	"context"
	"iter"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Eval runs have nobody to answer a sign-in pause, so tools needing OAuth
// must fail with "connect <Provider> in chat first" instead of pausing.
func TestGenerateInferences_runIsNonInteractive(t *testing.T) {
	probe, err := agent.New(agent.Config{
		Name: "probe-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				text := "interactive"
				if tooloauth.IsNonInteractive(ctx) {
					text = "non-interactive"
				}
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "probe-agent"
				ev.Content = genai.NewContentFromText(text, genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(probe), SessionService: session.InMemoryService()}, "probe-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sim := simulation.NewStaticUserSimulator([]models.Invocation{{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)}})

	inv, err := (&Generator{Runtime: rt}).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if len(inv) != 1 || inv[0].FinalResponse == nil || inv[0].FinalResponse.Parts[0].Text != "non-interactive" {
		t.Errorf("final response = %+v, want non-interactive", inv)
	}
}
