package generator

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// InferenceOptions configures a single eval inference run.
type InferenceOptions struct {
	SessionInput       *models.SessionInput
	UserSimulator      simulation.UserSimulator
	UseLive            bool
	LiveTimeoutSeconds int
	RequestInterceptor *RequestInterceptor
	// SessionID is the ADK session id for the inference run. When empty, the
	// generator allocates one via NewEvalSessionID.
	SessionID string
}

// Generator runs agents through the user simulator loop for eval inference.
type Generator struct {
	Runtime *adkrun.Runtime
}

// NewEvalSessionID returns a new eval-scoped session id.
func NewEvalSessionID() string {
	return EvalSessionIDPrefix + uuid.NewString()
}

// evalSessionID returns opts.SessionID when set, otherwise a new eval session id.
func evalSessionID(opts InferenceOptions) string {
	if strings.TrimSpace(opts.SessionID) != "" {
		return opts.SessionID
	}
	return NewEvalSessionID()
}

// GenerateInferences runs the agent against the user simulator and returns invocations.
func (g *Generator) GenerateInferences(ctx context.Context, opts InferenceOptions) ([]models.Invocation, error) {
	if g == nil || g.Runtime == nil {
		return nil, fmt.Errorf("generator: runtime is required")
	}
	if opts.UserSimulator == nil {
		return nil, fmt.Errorf("generator: user simulator is required")
	}
	if opts.UseLive {
		return g.generateInferencesLive(ctx, opts)
	}
	return g.generateInferencesStandard(ctx, opts)
}

// generateInferencesStandard runs the user-simulator loop with SSE inference.
func (g *Generator) generateInferencesStandard(ctx context.Context, opts InferenceOptions) ([]models.Invocation, error) {
	interceptor := opts.RequestInterceptor
	if interceptor == nil {
		var err error
		interceptor, err = NewRequestInterceptor()
		if err != nil {
			return nil, err
		}
	}

	userID, appName, state := sessionBootstrap(opts.SessionInput, g.Runtime.AppName())
	sessionID := evalSessionID(opts)
	pluginConfig := g.Runtime.MergeExtraPlugins(interceptor.Plugin())

	var events []*session.Event
	for {
		next, err := opts.UserSimulator.GetNextUserMessage(ctx, cloneEvents(events))
		if err != nil {
			return nil, err
		}
		if next.Status != simulation.StatusSuccess || next.UserMessage == nil {
			break
		}

		turnEvents, err := g.runSingleUserTurn(ctx, runTurnParams{
			userID:       userID,
			appName:      appName,
			sessionID:    sessionID,
			userContent:  next.UserMessage,
			stateDelta:   state,
			pluginConfig: pluginConfig,
		})
		if err != nil {
			return nil, err
		}
		state = nil
		events = append(events, turnEvents...)
	}

	appDetails := GetAppDetailsByInvocationID(events, interceptor)
	return ConvertEventsToEvalInvocations(events, appDetails), nil
}

// runTurnParams holds inputs for a single user turn during eval inference.
type runTurnParams struct {
	userID       string
	appName      string
	sessionID    string
	userContent  *genai.Content
	stateDelta   map[string]any
	pluginConfig runner.PluginConfig
}

// runSingleUserTurn executes one agent turn and returns session events including
// a synthetic user event prepended for eval conversion.
func (g *Generator) runSingleUserTurn(ctx context.Context, p runTurnParams) ([]*session.Event, error) {
	req := adkrun.RunRequest{
		AppName:    p.appName,
		UserID:     p.userID,
		SessionID:  p.sessionID,
		NewMessage: contentValue(p.userContent),
		StateDelta: p.stateDelta,
	}
	_, eventIter, err := g.Runtime.RunSSEWithPluginConfig(ctx, req, p.pluginConfig)
	if err != nil {
		return nil, err
	}

	var out []*session.Event
	var invocationID string
	for ev, err := range eventIter {
		if err != nil {
			return nil, err
		}
		if invocationID == "" {
			invocationID = ev.InvocationID
			userEv := session.NewEvent(ctx, invocationID)
			userEv.Author = userAuthor
			userEv.Content = p.userContent
			out = append(out, userEv)
		}
		out = append(out, ev)
	}
	return out, nil
}

// sessionBootstrap resolves user id, app name, and initial state from SessionInput.
func sessionBootstrap(input *models.SessionInput, defaultAppName string) (userID, appName string, state map[string]any) {
	userID = defaultEvalUserID
	appName = strings.TrimSpace(defaultAppName)
	if input != nil {
		if strings.TrimSpace(input.UserID) != "" {
			userID = input.UserID
		}
		if strings.TrimSpace(input.AppName) != "" {
			appName = input.AppName
		}
		state = input.State
	}
	return userID, appName, state
}

// contentValue converts optional genai Content to adkrun Content for RunRequest.
func contentValue(c *genai.Content) adkrun.Content {
	if c == nil {
		return adkrun.Content{Parts: []*genai.Part{}}
	}
	return *c
}

// cloneEvents returns a shallow copy of events for simulator input without mutating history.
func cloneEvents(events []*session.Event) []*session.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]*session.Event, len(events))
	copy(out, events)
	return out
}
