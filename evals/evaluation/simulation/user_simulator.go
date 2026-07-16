package simulation

import (
	"context"
	"errors"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/session"
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
}

// LlmBackedFactory constructs LLM-backed simulators (injected for tests and runtime).
type LlmBackedFactory interface {
	NewLlmBackedSimulator(cfg BaseUserSimulatorConfig, scenario *models.ConversationScenario) (UserSimulator, error)
}

// Provide selects a static or LLM-backed simulator based on eval case shape.
func (p *UserSimulatorProvider) Provide(evalCase models.EvalCase) (UserSimulator, error) {
	if err := evalCase.Validate(); err != nil {
		return nil, err
	}
	if evalCase.Conversation != nil {
		return NewStaticUserSimulator(evalCase.Conversation), nil
	}
	if p.LLM == nil {
		return nil, errLlmSimulatorRequired
	}
	return p.LLM.NewLlmBackedSimulator(p.Config, evalCase.ConversationScenario)
}
