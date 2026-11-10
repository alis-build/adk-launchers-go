package generator

import (
	"context"
	"testing"
	"time"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// The agent's own after-model callbacks run during eval inference, as in
// production: the eval's request recorder must not end the chain.
func TestGenerateInferencesRunsAgentAfterModelCallbacks(t *testing.T) {
	clock := &manualClock{t: time.Unix(1000, 0)}
	var calls int
	a, err := llmagent.New(llmagent.Config{
		Name: "agent", Model: slowModel{clock: clock},
		Tools: []tool.Tool{mustTool(t)},
		AfterModelCallbacks: []llmagent.AfterModelCallback{func(_ agent.Context, _ *model.LLMResponse, _ error) (*model.LLMResponse, error) {
			calls++
			return nil, nil
		}},
	})
	if err != nil {
		t.Fatalf("llmagent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}, "agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sim := simulation.NewStaticUserSimulator([]models.Invocation{{UserContent: genai.NewContentFromText("go", genai.RoleUser)}})

	if _, err := (&Generator{Runtime: rt, Now: clock.Now}).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim}); err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if calls != 2 {
		t.Errorf("agent after-model callback ran %d times, want 2 (one per model call)", calls)
	}
}

func mustTool(t *testing.T) tool.Tool {
	t.Helper()
	tl, err := functiontool.New(functiontool.Config{Name: "work", Description: "Works."},
		func(agent.Context, struct{}) (map[string]any, error) { return map[string]any{"ok": true}, nil })
	if err != nil {
		t.Fatalf("functiontool.New: %v", err)
	}
	return tl
}
