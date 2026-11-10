package generator

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// manualClock only moves when told to.
type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// slowModel takes 3 s per call: it calls the "work" tool, then answers
// "done" once the tool's response is back.
type slowModel struct{ clock *manualClock }

func (m slowModel) Name() string { return "slow" }

func (m slowModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.clock.advance(3 * time.Second)
		last := req.Contents[len(req.Contents)-1]
		for _, p := range last.Parts {
			if p != nil && p.FunctionResponse != nil {
				yield(&model.LLMResponse{Content: genai.NewContentFromText("done", genai.RoleModel)}, nil)
				return
			}
		}
		yield(&model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call-1", Name: "work", Args: map[string]any{}}},
		}}}, nil)
	}
}

// Mirrors adk-python test_evaluation_generator.py:2655: a turn of two 3 s
// model calls around a 1 s tool is 7 s, 6 of them model time and 1 tool time.
func TestGenerateInferencesSplitsToolTimeFromModelTime(t *testing.T) {
	clock := &manualClock{t: time.Unix(1000, 0)}
	work, err := functiontool.New(functiontool.Config{Name: "work", Description: "Works."},
		func(agent.Context, struct{}) (map[string]any, error) {
			clock.advance(time.Second)
			return map[string]any{"ok": true}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New: %v", err)
	}
	a, err := llmagent.New(llmagent.Config{Name: "agent", Model: slowModel{clock: clock}, Tools: []tool.Tool{work}})
	if err != nil {
		t.Fatalf("llmagent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}, "agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sim := simulation.NewStaticUserSimulator([]models.Invocation{{UserContent: genai.NewContentFromText("go", genai.RoleUser)}})

	inv, err := (&Generator{Runtime: rt, Now: clock.Now}).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}

	if len(inv) != 1 {
		t.Fatalf("invocations = %d, want 1", len(inv))
	}
	for name, got := range map[string]*float64{"Duration": inv[0].Duration, "ModelDuration": inv[0].ModelDuration, "ToolDuration": inv[0].ToolDuration} {
		want := map[string]float64{"Duration": 7, "ModelDuration": 6, "ToolDuration": 1}[name]
		if got == nil || *got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// An app plugin that rewrites the model's reply or a tool's result ends
// ADK's plugin chain; the timer runs first, so it still sees both.
func TestTurnTimingSurvivesAppPluginsThatRewrite(t *testing.T) {
	clock := &manualClock{t: time.Unix(1000, 0)}
	work, err := functiontool.New(functiontool.Config{Name: "work", Description: "Works."},
		func(agent.Context, struct{}) (map[string]any, error) {
			clock.advance(time.Second)
			return map[string]any{"ok": true}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New: %v", err)
	}
	rewriter, err := plugin.New(plugin.Config{
		Name: "rewriter",
		AfterModelCallback: func(_ agent.Context, resp *model.LLMResponse, _ error) (*model.LLMResponse, error) {
			return resp, nil
		},
		AfterToolCallback: func(_ agent.Context, _ tool.Tool, _, result map[string]any, _ error) (map[string]any, error) {
			return result, nil
		},
	})
	if err != nil {
		t.Fatalf("plugin.New: %v", err)
	}
	a, err := llmagent.New(llmagent.Config{Name: "agent", Model: slowModel{clock: clock}, Tools: []tool.Tool{work}})
	if err != nil {
		t.Fatalf("llmagent.New: %v", err)
	}
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService(),
		PluginConfig: runner.PluginConfig{Plugins: []*plugin.Plugin{rewriter}},
	}, "agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	sim := simulation.NewStaticUserSimulator([]models.Invocation{{UserContent: genai.NewContentFromText("go", genai.RoleUser)}})

	inv, err := (&Generator{Runtime: rt, Now: clock.Now}).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if got := inv[0]; got.ModelDuration == nil || *got.ModelDuration != 6 || got.ToolDuration == nil || *got.ToolDuration != 1 {
		t.Errorf("model, tool = %v, %v; want 6, 1", got.ModelDuration, got.ToolDuration)
	}
}
