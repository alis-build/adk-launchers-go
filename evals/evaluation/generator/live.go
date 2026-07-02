package generator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// generateInferencesLive runs eval inference through RunLive with per-turn timeouts.
func (g *Generator) generateInferencesLive(ctx context.Context, opts InferenceOptions) ([]models.Invocation, error) {
	timeout := opts.LiveTimeoutSeconds
	if timeout <= 0 {
		timeout = DefaultLiveTimeoutSeconds
	}

	interceptor := opts.RequestInterceptor
	if interceptor == nil {
		var err error
		interceptor, err = NewRequestInterceptor()
		if err != nil {
			return nil, err
		}
	}

	userID, appName, state := sessionBootstrap(opts.SessionInput, g.Runtime.AppName())
	sessionID := NewEvalSessionID()

	r, err := g.newEvalRunner(appName, interceptor)
	if err != nil {
		return nil, err
	}

	var runOpts []runner.RunOption
	if len(state) > 0 {
		runOpts = append(runOpts, runner.WithStateDelta(state))
	}

	liveSess, eventIter, err := r.RunLive(ctx, userID, sessionID, agent.LiveRunConfig{
		ResponseModalities:       []genai.Modality{genai.ModalityAudio},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
	}, runOpts...)
	if err != nil {
		return nil, fmt.Errorf("generator: start live session: %w", err)
	}
	defer liveSess.Close()

	eventCh := make(chan *session.Event, 64)
	errCh := make(chan error, 1)
	go func() {
		for ev, err := range eventIter {
			if err != nil {
				errCh <- err
				return
			}
			eventCh <- ev
		}
		close(eventCh)
	}()

	var events []*session.Event
	for {
		next, err := opts.UserSimulator.GetNextUserMessage(ctx, cloneEvents(events))
		if err != nil {
			return nil, err
		}
		if next.Status != simulation.StatusSuccess || next.UserMessage == nil {
			break
		}

		invocationID := uuid.NewString()
		userEv := session.NewEvent(ctx, invocationID)
		userEv.Author = userAuthor
		userEv.Content = next.UserMessage
		events = append(events, userEv)

		if err := liveSess.Send(agent.LiveRequest{Content: next.UserMessage}); err != nil {
			return nil, fmt.Errorf("generator: send live user message: %w", err)
		}

		turnCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		turnEvents, err := collectLiveTurn(turnCtx, invocationID, eventCh, errCh)
		cancel()
		if err != nil {
			return nil, err
		}
		events = append(events, turnEvents...)
	}

	appDetails := GetAppDetailsByInvocationID(events, interceptor)
	return ConvertEventsToEvalInvocations(events, appDetails), nil
}

// collectLiveTurn reads events until the turn completes, times out, or the stream errors.
func collectLiveTurn(ctx context.Context, invocationID string, eventCh <-chan *session.Event, errCh <-chan error) ([]*session.Event, error) {
	var out []*session.Event
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("generator: timed out waiting for live turn completion")
		case err := <-errCh:
			if err != nil {
				return nil, err
			}
		case ev, ok := <-eventCh:
			if !ok {
				return out, nil
			}
			if ev == nil {
				continue
			}
			if ev.InvocationID != invocationID {
				evCopy := *ev
				evCopy.InvocationID = invocationID
				ev = &evCopy
			}
			out = append(out, ev)
			if ev.TurnComplete && !ev.Partial {
				return out, nil
			}
		}
	}
}

// newEvalRunner builds a runner with the request interceptor plugin appended.
func (g *Generator) newEvalRunner(appName string, interceptor *RequestInterceptor) (*runner.Runner, error) {
	cfg := g.Runtime.LauncherConfig()
	if cfg == nil {
		return nil, fmt.Errorf("generator: launcher config is required")
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = g.Runtime.AppName()
	}
	curAgent, err := cfg.AgentLoader.LoadAgent(appName)
	if err != nil {
		return nil, fmt.Errorf("generator: load agent %q: %w", appName, err)
	}
	plugins := append([]*plugin.Plugin{}, cfg.PluginConfig.Plugins...)
	plugins = append(plugins, interceptor.Plugin())
	return runner.New(runner.Config{
		AppName:           appName,
		Agent:             curAgent,
		SessionService:    cfg.SessionService,
		MemoryService:     cfg.MemoryService,
		ArtifactService:   cfg.ArtifactService,
		PluginConfig:      runner.PluginConfig{Plugins: plugins, CloseTimeout: cfg.PluginConfig.CloseTimeout},
		AutoCreateSession: true,
	})
}
