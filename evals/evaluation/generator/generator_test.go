package generator

import (
	"context"
	"iter"
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestNewEvalSessionID(t *testing.T) {
	id := NewEvalSessionID()
	if !strings.HasPrefix(id, EvalSessionIDPrefix) {
		t.Fatalf("id = %q", id)
	}
}

func TestGetAppDetailsByInvocationID(t *testing.T) {
	interceptor, err := NewRequestInterceptor()
	if err != nil {
		t.Fatalf("NewRequestInterceptor: %v", err)
	}
	requestID := "req-1"
	interceptor.mu.Lock()
	interceptor.requests[requestID] = &model.LLMRequest{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("Be helpful", genai.RoleUser),
			Tools: []*genai.Tool{
				{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "lookup"}}},
			},
		},
	}
	interceptor.mu.Unlock()

	ev := session.NewEvent("inv1")
	ev.Author = "test-agent"
	ev.CustomMetadata = map[string]any{llmRequestIDKey: requestID}

	details := GetAppDetailsByInvocationID([]*session.Event{ev}, interceptor)
	appDetails := details["inv1"]
	if appDetails == nil {
		t.Fatalf("details missing for inv1")
	}
	agentDetails := appDetails.AgentDetails["test-agent"]
	if agentDetails.Instructions != "Be helpful" {
		t.Fatalf("instructions = %q", agentDetails.Instructions)
	}
}

func TestGenerateInferencesStaticConversation(t *testing.T) {
	ctx := context.Background()
	replyAgent, err := agent.New(agent.Config{
		Name: "reply-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				msg := ctx.UserContent()
				text := "ack"
				if msg != nil && len(msg.Parts) > 0 && msg.Parts[0].Text != "" {
					text = "echo: " + msg.Parts[0].Text
				}
				ev := session.NewEvent(ctx.InvocationID())
				ev.Author = "reply-agent"
				ev.Content = genai.NewContentFromText(text, genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(replyAgent),
		SessionService: session.InMemoryService(),
	}, "reply-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	gen := &Generator{Runtime: rt}
	evalCase := models.EvalCase{
		EvalID: "case-1",
		Conversation: []models.Invocation{
			{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)},
			{UserContent: genai.NewContentFromText("Bye", genai.RoleUser)},
		},
	}
	provider := simulation.UserSimulatorProvider{}
	sim, err := provider.Provide(evalCase)
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}

	inv, err := gen.GenerateInferences(ctx, InferenceOptions{UserSimulator: sim})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	if len(inv) != 2 {
		t.Fatalf("len = %d", len(inv))
	}
	if inv[0].FinalResponse == nil || inv[0].FinalResponse.Parts[0].Text != "echo: Hello" {
		t.Fatalf("turn1 = %+v", inv[0].FinalResponse)
	}
}

func TestGenerateInferencesLiveRequiresLiveAgent(t *testing.T) {
	ctx := context.Background()
	rt := testReplyRuntime(t)
	gen := &Generator{Runtime: rt}
	sim := simulation.NewStaticUserSimulator([]models.Invocation{
		{UserContent: genai.NewContentFromText("Hello", genai.RoleUser)},
	})
	_, err := gen.GenerateInferences(ctx, InferenceOptions{
		UserSimulator: sim,
		UseLive:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "live session") {
		t.Fatalf("err = %v", err)
	}
}

func testReplyRuntime(t *testing.T) *adkrun.Runtime {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: "reply-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx.InvocationID())
				ev.Author = "reply-agent"
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
		SessionService: session.InMemoryService(),
	}, "reply-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}
