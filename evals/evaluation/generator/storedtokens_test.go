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

// storedTokensProbe runs an agent that reports whether its run may use
// tooloauth's stored tokens.
func storedTokensProbe(t *testing.T, g func(*adkrun.Runtime) *Generator) string {
	t.Helper()
	probe, err := agent.New(agent.Config{
		Name: "probe-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				text := "stored tokens used"
				if tooloauth.StoredTokensDisabled(ctx) {
					text = "stored tokens disabled"
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
	inv, err := g(rt).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if len(inv) != 1 || inv[0].FinalResponse == nil {
		t.Fatalf("invocations = %+v", inv)
	}
	return inv[0].FinalResponse.Parts[0].Text
}

// Evals run as the caller, so a connected user's real account would be used
// by an eval of an OAuth tool; by default it is not.
func TestGenerateInferences_storedTokensDisabledByDefault(t *testing.T) {
	got := storedTokensProbe(t, func(rt *adkrun.Runtime) *Generator { return &Generator{Runtime: rt} })
	if got != "stored tokens disabled" {
		t.Errorf("probe = %q, want stored tokens disabled", got)
	}
}

func TestGenerateInferences_storedTokensOptIn(t *testing.T) {
	got := storedTokensProbe(t, func(rt *adkrun.Runtime) *Generator {
		return &Generator{Runtime: rt, UseStoredOAuthTokens: true}
	})
	if got != "stored tokens used" {
		t.Errorf("probe = %q, want stored tokens used", got)
	}
}
