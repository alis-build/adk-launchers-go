package simulation

import (
	"context"
	"fmt"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const stopSignal = "</finished>"

// LlmBackedUserSimulatorConfig configures LLM-backed user simulation.
//
// MaxAllowedInvocations controls how many user turns the simulator generates
// before terminating with [StatusTurnLimitReached]:
//   - 0 (zero-value) is treated as unset and replaced with the default (20).
//     Set an explicit positive limit to override.
//   - Any negative value (e.g. -1) disables the limit; the simulator runs
//     until the LLM emits the stop signal or the model errors out.
//   - Positive values cap the loop after that many invocations.
type LlmBackedUserSimulatorConfig struct {
	Model                 string `json:"model,omitempty"`
	MaxAllowedInvocations int    `json:"maxAllowedInvocations,omitempty"`
	CustomInstructions    string `json:"customInstructions,omitempty"`
	IncludeFunctionCalls  bool   `json:"includeFunctionCalls,omitempty"`
}

// defaultLlmBackedConfig returns simulator defaults when config fields are unset.
func defaultLlmBackedConfig() LlmBackedUserSimulatorConfig {
	return LlmBackedUserSimulatorConfig{
		Model:                 "gemini-2.5-flash",
		MaxAllowedInvocations: 20,
	}
}

// LlmContentGenerator generates simulator responses from a prompt.
type LlmContentGenerator interface {
	GenerateContent(ctx context.Context, model, prompt string) (string, error)
}

// LlmBackedUserSimulator generates user turns with an LLM.
type LlmBackedUserSimulator struct {
	config               LlmBackedUserSimulatorConfig
	scenario             *models.ConversationScenario
	generator            LlmContentGenerator
	invocationCount      int
	persona              string
}

// NewLlmBackedUserSimulator constructs an LLM-backed user simulator for scenario eval cases.
func NewLlmBackedUserSimulator(cfg LlmBackedUserSimulatorConfig, scenario *models.ConversationScenario, generator LlmContentGenerator) (*LlmBackedUserSimulator, error) {
	if scenario == nil {
		return nil, fmt.Errorf("conversation scenario is required")
	}
	if generator == nil {
		return nil, fmt.Errorf("content generator is required")
	}
	if cfg.Model == "" {
		cfg.Model = defaultLlmBackedConfig().Model
	}
	if cfg.MaxAllowedInvocations == 0 {
		cfg.MaxAllowedInvocations = defaultLlmBackedConfig().MaxAllowedInvocations
	}
	persona := ""
	if len(scenario.UserPersona) > 0 && string(scenario.UserPersona) != "null" {
		persona = string(scenario.UserPersona)
	}
	return &LlmBackedUserSimulator{
		config:    cfg,
		scenario:  scenario,
		generator: generator,
		persona:   persona,
	}, nil
}

func (s *LlmBackedUserSimulator) GetNextUserMessage(ctx context.Context, events []*session.Event) (NextUserMessage, error) {
	limit := s.config.MaxAllowedInvocations
	if limit >= 0 && s.invocationCount >= limit {
		return NextUserMessage{Status: StatusTurnLimitReached}, nil
	}

	dialogue := summarizeConversation(events, s.config.IncludeFunctionCalls)
	response, err := s.nextResponse(ctx, dialogue)
	if err != nil {
		return NextUserMessage{}, err
	}
	s.invocationCount++

	if response != "" && strings.Contains(strings.ToLower(response), strings.ToLower(stopSignal)) {
		return NextUserMessage{Status: StatusStopSignalDetected}, nil
	}
	if response == "" {
		return NextUserMessage{}, fmt.Errorf("failed to generate a user message")
	}
	return NextUserMessage{
		Status:      StatusSuccess,
		UserMessage: genai.NewContentFromText(response, genai.RoleUser),
	}, nil
}

// nextResponse returns the starting prompt on the first turn, otherwise asks the LLM.
func (s *LlmBackedUserSimulator) nextResponse(ctx context.Context, dialogue string) (string, error) {
	if s.invocationCount == 0 {
		return s.scenario.StartingPrompt, nil
	}
	prompt, err := GetLlmBackedUserSimulatorPrompt(
		s.scenario.ConversationPlan,
		dialogue,
		stopSignal,
		s.config.CustomInstructions,
		s.persona,
	)
	if err != nil {
		return "", err
	}
	return s.generator.GenerateContent(ctx, s.config.Model, prompt)
}

// summarizeConversation formats prior session events as dialogue for the simulator prompt.
func summarizeConversation(events []*session.Event, includeFunctionCalls bool) string {
	var lines []string
	for _, e := range events {
		if e == nil || e.Content == nil {
			continue
		}
		for _, part := range e.Content.Parts {
			if part == nil {
				continue
			}
			switch {
			case part.Text != "" && !part.Thought:
				lines = append(lines, fmt.Sprintf("%s: %s", e.Author, part.Text))
			case includeFunctionCalls && part.FunctionCall != nil:
				lines = append(lines, fmt.Sprintf("%s called tool '%s' with args: %v", e.Author, part.FunctionCall.Name, part.FunctionCall.Args))
			case includeFunctionCalls && part.FunctionResponse != nil:
				lines = append(lines, fmt.Sprintf("Tool '%s' returned: %v", part.FunctionResponse.Name, part.FunctionResponse.Response))
			}
		}
	}
	return strings.Join(lines, "\n\n")
}

// DefaultLlmBackedFactory builds LlmBackedUserSimulator instances.
type DefaultLlmBackedFactory struct {
	Config    LlmBackedUserSimulatorConfig
	Generator LlmContentGenerator
}

func (f *DefaultLlmBackedFactory) NewLlmBackedSimulator(_ BaseUserSimulatorConfig, scenario *models.ConversationScenario) (UserSimulator, error) {
	cfg := f.Config
	if cfg.Model == "" {
		cfg = defaultLlmBackedConfig()
		cfg.CustomInstructions = f.Config.CustomInstructions
		cfg.IncludeFunctionCalls = f.Config.IncludeFunctionCalls
		if f.Config.MaxAllowedInvocations != 0 {
			cfg.MaxAllowedInvocations = f.Config.MaxAllowedInvocations
		}
	}
	return NewLlmBackedUserSimulator(cfg, scenario, f.Generator)
}
