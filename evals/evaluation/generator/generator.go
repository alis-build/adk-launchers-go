package generator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/plugin"
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
	// UseStoredOAuthTokens lets tools use the caller's stored tooloauth
	// tokens. Off by default: inference runs as the caller, so an eval of an
	// OAuth tool would otherwise act on their real account.
	UseStoredOAuthTokens bool
	// Now is the clock turn durations are measured with; nil uses
	// time.Now. Tests inject a manual clock.
	Now func() time.Time
}

// clock returns g.Now, or time.Now when it is unset.
func (g *Generator) clock() func() time.Time {
	if g.Now != nil {
		return g.Now
	}
	return time.Now
}

// NewEvalSessionID returns a new eval-scoped session id.
func NewEvalSessionID() string {
	return EvalSessionIDPrefix + uuid.NewString()
}

// IsEvalSessionID reports whether id was created for an eval run, under
// either prefix. Embedders that list sessions can use it to hide eval runs.
func IsEvalSessionID(id string) bool {
	return strings.HasPrefix(id, EvalSessionIDPrefix) || strings.HasPrefix(id, LegacyEvalSessionIDPrefix)
}

// evalSessionID returns opts.SessionID when set, otherwise a new eval session id.
func evalSessionID(opts InferenceOptions) string {
	if strings.TrimSpace(opts.SessionID) != "" {
		return opts.SessionID
	}
	return NewEvalSessionID()
}

// GenerateInferences runs the agent against the user simulator and returns
// invocations. The run is marked non-interactive: nobody can answer a
// tooloauth sign-in pause during an eval, so such tools fail instead. Unless
// UseStoredOAuthTokens is set, it is also marked to use no stored token.
func (g *Generator) GenerateInferences(ctx context.Context, opts InferenceOptions) ([]models.Invocation, error) {
	ctx = tooloauth.WithNonInteractive(ctx)
	if g == nil || g.Runtime == nil {
		return nil, fmt.Errorf("generator: runtime is required")
	}
	if opts.UserSimulator == nil {
		return nil, fmt.Errorf("generator: user simulator is required")
	}
	if !g.UseStoredOAuthTokens {
		ctx = tooloauth.WithoutStoredTokens(ctx)
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

	now := g.clock()
	timer, err := newTurnTimer(now)
	if err != nil {
		return nil, err
	}

	userID, appName, state := sessionBootstrap(opts.SessionInput, g.Runtime.AppName())
	sessionID := evalSessionID(opts)
	// The timer runs before every other plugin, the app's included, so none
	// can end ADK's plugin chain before it closes an interval.
	base := g.Runtime.MergeExtraPlugins(interceptor.Plugin())
	pluginConfig := runner.PluginConfig{
		Plugins:      append([]*plugin.Plugin{timer.plugin}, base.Plugins...),
		CloseTimeout: base.CloseTimeout,
	}
	turnSeconds := map[string]float64{}

	var events []*session.Event
	for {
		next, err := opts.UserSimulator.GetNextUserMessage(ctx, cloneEvents(events))
		if err != nil {
			return nil, err
		}
		if next.Status != simulation.StatusSuccess || next.UserMessage == nil {
			break
		}

		// Timed around the agent's turn only, so the simulator's time stays
		// out, as in adk-python.
		start := now()
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
		if len(turnEvents) > 0 {
			turnSeconds[turnEvents[0].InvocationID] = now().Sub(start).Seconds()
		}
		state = nil
		events = append(events, turnEvents...)
	}

	appDetails := GetAppDetailsByInvocationID(events, interceptor)
	invocations := ConvertEventsToEvalInvocations(events, appDetails)
	for i := range invocations {
		inv := &invocations[i]
		total, ok := turnSeconds[inv.InvocationID]
		if !ok {
			continue
		}
		busy := timer.busy(inv.InvocationID)
		d, m, t := round3(total), round3(busy.modelSeconds()), round3(busy.toolSeconds())
		inv.Duration, inv.ModelDuration, inv.ToolDuration = &d, &m, &t
	}
	return invocations, nil
}

// round3 rounds to 3 decimal places as adk-python's round(x, 3) does, by
// the exact stored value.
func round3(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
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
	// A turn the agent answered with no events is still a turn: record the
	// user's message under a fresh invocation ID so the case keeps one
	// invocation per turn, as adk-python does.
	if invocationID == "" {
		userEv := session.NewEvent(ctx, uuid.NewString())
		userEv.Author = userAuthor
		userEv.Content = p.userContent
		out = append(out, userEv)
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
