package simulation

import (
	"context"
	"errors"
	"fmt"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

var errLlmSimulatorRequired = errors.New("llm-backed user simulator factory not configured")

// Status is the result of GetNextUserMessage.
type Status string

const (
	StatusSuccess            Status = "success"
	StatusTurnLimitReached   Status = "turn_limit_reached"
	StatusStopSignalDetected Status = "stop_signal_detected"
	StatusNoMessageGenerated Status = "no_message_generated"
)

// NextUserMessage is the user simulator output.
type NextUserMessage struct {
	Status      Status         `json:"status"`
	UserMessage *genai.Content `json:"userMessage,omitempty"`
}

// UserSimulator generates the next user turn during eval inference.
type UserSimulator interface {
	GetNextUserMessage(ctx context.Context, events []*session.Event) (NextUserMessage, error)
}

// BaseUserSimulatorConfig is shared simulator configuration from EvalConfig.
type BaseUserSimulatorConfig struct {
	MaxInvocations *int `json:"maxInvocations,omitempty"`
}

// StaticUserSimulator replays user messages from a static conversation.
type StaticUserSimulator struct {
	conversation []models.Invocation
	index        int
}

// NewStaticUserSimulator replays user turns from a fixed golden conversation.
func NewStaticUserSimulator(conversation []models.Invocation) *StaticUserSimulator {
	return &StaticUserSimulator{conversation: conversation}
}

func (s *StaticUserSimulator) GetNextUserMessage(_ context.Context, _ []*session.Event) (NextUserMessage, error) {
	if s.index >= len(s.conversation) {
		return NextUserMessage{Status: StatusStopSignalDetected}, nil
	}
	msg := s.conversation[s.index].UserContent
	s.index++
	return NextUserMessage{Status: StatusSuccess, UserMessage: msg}, nil
}

// UserSimulatorProvider selects a simulator for an eval case.
type UserSimulatorProvider struct {
	Config BaseUserSimulatorConfig
	LLM    LlmBackedFactory
	// SimulatorConfig is the parsed user_simulator_config. An
	// [LlmAudioUserSimulatorConfig] speaks every case's turns through an
	// [AudioUserSimulator]; nil or any other config keeps text turns.
	SimulatorConfig UserSimulatorConfig
	// ResolveModel resolves the audio model by name; nil uses ADK's model
	// registry (model.NewLLM).
	ResolveModel func(ctx context.Context, name string) (model.LLM, error)
}

// LlmBackedFactory constructs LLM-backed simulators (injected for tests and runtime).
type LlmBackedFactory interface {
	NewLlmBackedSimulator(cfg BaseUserSimulatorConfig, scenario *models.ConversationScenario) (UserSimulator, error)
}

// LlmBackedConfigFactory is an [LlmBackedFactory] that can also build a
// simulator from a whole text-simulator config, so a run's
// user_simulator_config (its model, turn limit, instructions) takes effect.
// [DefaultLlmBackedFactory] implements it; a factory that does not only gets
// the turn limit, through NewLlmBackedSimulator.
type LlmBackedConfigFactory interface {
	NewLlmBackedSimulatorWithConfig(cfg LlmBackedUserSimulatorConfig, scenario *models.ConversationScenario) (UserSimulator, error)
}

// Provide selects a static or LLM-backed simulator based on eval case shape,
// wrapped in an [AudioUserSimulator] when SimulatorConfig asks for audio
// (adk-python's UserSimulatorProvider.provide).
func (p *UserSimulatorProvider) Provide(ctx context.Context, evalCase models.EvalCase) (UserSimulator, error) {
	if err := evalCase.Validate(); err != nil {
		return nil, err
	}
	audioCfg, audio := p.SimulatorConfig.(LlmAudioUserSimulatorConfig)
	// The text simulator of a scenario case takes the run's text settings,
	// from either config type, as adk-python builds it.
	var textCfg *LlmBackedUserSimulatorConfig
	switch c := p.SimulatorConfig.(type) {
	case LlmBackedUserSimulatorConfig:
		textCfg = &c
	case LlmAudioUserSimulatorConfig:
		textCfg = &c.LlmBackedUserSimulatorConfig
	}
	var text UserSimulator
	switch {
	case evalCase.Conversation != nil:
		text = NewStaticUserSimulator(evalCase.Conversation)
	case p.LLM == nil:
		return nil, errLlmSimulatorRequired
	default:
		var sim UserSimulator
		var err error
		if f, ok := p.LLM.(LlmBackedConfigFactory); ok && textCfg != nil {
			sim, err = f.NewLlmBackedSimulatorWithConfig(*textCfg, evalCase.ConversationScenario)
		} else {
			cfg := p.Config
			if textCfg != nil {
				maxInvocations := textCfg.MaxAllowedInvocations
				cfg = BaseUserSimulatorConfig{MaxInvocations: &maxInvocations}
			}
			sim, err = p.LLM.NewLlmBackedSimulator(cfg, evalCase.ConversationScenario)
		}
		if err != nil {
			return nil, err
		}
		text = sim
	}
	if !audio {
		return text, nil
	}
	resolve := p.ResolveModel
	if resolve == nil {
		resolve = model.NewLLM
	}
	audioModel, err := resolve(ctx, audioCfg.AudioModel)
	if err != nil {
		return nil, fmt.Errorf("user_simulator_config.audio_model %q: %w", audioCfg.AudioModel, err)
	}
	return NewAudioUserSimulator(audioCfg, text, audioModel), nil
}
