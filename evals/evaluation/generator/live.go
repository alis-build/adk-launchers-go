package generator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/internal/adkrun"
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
	sessionID := evalSessionID(opts)

	r, err := g.newEvalRunner(appName, interceptor)
	if err != nil {
		return nil, err
	}

	var runOpts []runner.RunOption
	if len(state) > 0 {
		runOpts = append(runOpts, runner.WithStateDelta(state))
	}

	liveSess, eventIter, err := r.RunLive(ctx, userID, sessionID, liveRunConfig(), runOpts...)
	if err != nil {
		return nil, fmt.Errorf("generator: start live session: %w", err)
	}
	defer liveSess.Close()

	eventCh := make(chan *session.Event, 64)
	errCh := make(chan error, 1)
	// This runner is built here rather than obtained from Runtime.RunSSE, so the
	// compaction-error handling RunSSE applies has to be applied explicitly.
	// Without it a failed summary aborts eval inference for a turn the agent
	// answered.
	liveEvents := adkrun.WithoutCompactionErrors(ctx, fmt.Sprintf("eval session %q", sessionID), eventIter)
	// done stops the reader when this function returns, for example after a
	// turn times out while the model is still streaming: without it the
	// reader blocks on a full eventCh forever and holds the live stream open.
	done := make(chan struct{})
	defer close(done)
	go func() {
		for ev, err := range liveEvents {
			if err != nil {
				select {
				case errCh <- err:
				case <-done:
				}
				return
			}
			select {
			case eventCh <- ev:
			case <-done:
				return
			}
		}
		close(eventCh)
	}()

	var events []*session.Event
	for {
		// The simulator reads the agent's spoken replies as text.
		next, err := opts.UserSimulator.GetNextUserMessage(ctx, normalizeLiveTranscriptions(cloneEvents(events)))
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

		// The logged user event keeps the whole turn; the session gets audio
		// alone when the turn has any, so the agent answers the speech.
		if hasInlineData(next.UserMessage) {
			err = sendAudioToLive(liveSess, next.UserMessage)
		} else {
			err = liveSess.Send(agent.LiveRequest{Content: next.UserMessage})
		}
		if err != nil {
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
	return ConvertEventsToEvalInvocations(normalizeLiveTranscriptions(events), appDetails), nil
}

// audioChunkBytes is the size of each audio blob streamed to a live session.
const audioChunkBytes = 16000

// liveRunConfig is the config of every live eval session. Voice activity
// detection is off, so each turn's boundaries are the activity markers
// sendAudioToLive puts around its audio.
func liveRunConfig() agent.LiveRunConfig {
	return agent.LiveRunConfig{
		ResponseModalities:       []genai.Modality{genai.ModalityAudio},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
		RealtimeInputConfig: &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true},
		},
	}
}

// hasInlineData reports whether any part of c carries inline data.
func hasInlineData(c *genai.Content) bool {
	for _, p := range c.Parts {
		if p != nil && p.InlineData != nil {
			return true
		}
	}
	return false
}

// sendAudioToLive streams the audio of a user turn as realtime input,
// between an activity start and end, in audioChunkBytes chunks. Text parts
// are not sent.
func sendAudioToLive(sess agent.LiveSession, content *genai.Content) error {
	if err := sess.Send(agent.LiveRequest{RealtimeInput: &genai.ActivityStart{}}); err != nil {
		return err
	}
	for _, p := range content.Parts {
		if p == nil || p.InlineData == nil {
			continue
		}
		data := p.InlineData.Data
		for start := 0; start < len(data); start += audioChunkBytes {
			chunk := data[start:min(start+audioChunkBytes, len(data))]
			if err := sess.Send(agent.LiveRequest{RealtimeInput: &genai.Blob{Data: chunk, MIMEType: p.InlineData.MIMEType}}); err != nil {
				return err
			}
		}
	}
	return sess.Send(agent.LiveRequest{RealtimeInput: &genai.ActivityEnd{}})
}

// normalizeLiveTranscriptions returns events with each final transcription
// event rewritten as text content: what the user said as a user message,
// what the agent said as a model message. Partial events, events that have
// content, and events with no transcription text pass through; the input
// events are not modified.
func normalizeLiveTranscriptions(events []*session.Event) []*session.Event {
	out := make([]*session.Event, 0, len(events))
	for _, ev := range events {
		if ev == nil || ev.Content != nil || ev.Partial {
			out = append(out, ev)
			continue
		}
		var text string
		var role genai.Role
		switch {
		case ev.InputTranscription != nil && ev.InputTranscription.Text != "":
			text, role = ev.InputTranscription.Text, genai.RoleUser
		case ev.OutputTranscription != nil && ev.OutputTranscription.Text != "":
			text, role = ev.OutputTranscription.Text, genai.RoleModel
		default:
			out = append(out, ev)
			continue
		}
		rewritten := *ev
		rewritten.InputTranscription = nil
		rewritten.OutputTranscription = nil
		rewritten.Content = genai.NewContentFromText(text, role)
		out = append(out, &rewritten)
	}
	return out
}

// collectLiveTurn reads events until the turn completes, times out, or the stream errors.
func collectLiveTurn(ctx context.Context, invocationID string, eventCh <-chan *session.Event, errCh <-chan error) ([]*session.Event, error) {
	var out []*session.Event
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("generator: timed out waiting for live turn completion")
			}
			return nil, fmt.Errorf("generator: live turn: %w", ctx.Err())
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
		Compaction:        cfg.Compaction,
	})
}
