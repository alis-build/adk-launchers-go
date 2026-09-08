// Package adkrun runs ADK agents in-process from launchers (scheduler cron ticks,
// eval inference, AG-UI runs, etc.).
//
// Construct a [Runtime] with [NewRuntime] and [launcher.Config], then call
// [Runtime.RunSSE] and range over the returned event iterator. Use
// [Runtime.RunSSEWithExtraPlugins] when a single run needs additional ADK plugins
// (for example eval request interceptors) without changing the launcher-wide
// plugin list. Multi-turn callers should use [Runtime.MergeExtraPlugins] once and
// pass the result to [Runtime.RunSSEWithPluginConfig] on each turn.
//
// Context compaction is inherited from [launcher.Config.Compaction], matching
// how ADK's own launchers pass it to every runner they build; there is no
// per-launcher override, because ADK defines the setting as process-wide. It is
// off unless the application sets it. When on, the summaries it produces shrink
// the model's prompt but never appear in the event stream or in converted
// message history, so clients keep showing the full conversation. See
// [WithoutCompactionErrors] for how compaction failures are reported.
package adkrun

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/google/uuid"
	"go.alis.build/alog"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

// Content and Part are the genai message types used in [RunRequest.NewMessage].
// Part supports text, inlineData, fileData, functionCall, functionResponse,
// executableCode, codeExecutionResult, toolCall, toolResponse, and related fields
// documented in the ADK REST API.
type (
	Content = genai.Content
	Part    = genai.Part
	// Event is a single ADK session event emitted while an agent runs.
	Event = session.Event
)

// RunRequest describes an agent run. Field names align with the ADK REST run API
// (https://adk.dev/api-reference/rest/#/default/run_agent_sse_run_sse_post) for parity
// with HTTP clients and tooling.
type RunRequest struct {
	// AppName is the ADK application name to run.
	AppName string `json:"appName,omitempty"`
	// UserID is the user ID to run the agent for.
	UserID string `json:"userId"`
	// SessionID identifies the ADK session to continue. When empty a new UUID is generated and returned.
	SessionID string `json:"sessionId"`
	// NewMessage is the user or model turn to append. Use genai helpers such as
	// [genai.NewContentFromText] or [genai.NewPartFromFunctionResponse] to build parts.
	NewMessage Content `json:"newMessage"`
	// Streaming enables partial SSE-style events from the model when true.
	Streaming bool `json:"streaming,omitempty"`
	// SaveInputBlobsAsArtifacts saves blob parts in NewMessage (images, files) as artifacts.
	SaveInputBlobsAsArtifacts bool `json:"saveInputBlobsAsArtifacts,omitempty"`
	// StateDelta merges into the session state before the run (ADK runner.WithStateDelta).
	StateDelta map[string]any `json:"stateDelta,omitempty"`
	// FunctionCallEventID identifies the event whose function call this request answers.
	// Accepted for ADK REST API parity; the in-process runner ignores this field and
	// resolves resume context from session history plus FunctionResponse ids in NewMessage.
	FunctionCallEventID string `json:"functionCallEventId,omitempty"`
	// InvocationID correlates the run with a prior invocation for API/logging parity.
	// The in-process runner does not read this field; ADK v2 reuses the paused
	// invocation id automatically when NewMessage carries FunctionResponse parts whose
	// ids match FunctionCall entries already stored on the session.
	InvocationID string `json:"invocationId,omitempty"`
}

// UserTextMessage returns a user [Content] with a single text part.
func UserTextMessage(text string) Content {
	return *genai.NewContentFromText(text, genai.RoleUser)
}

// Runtime runs agents in-process using the same services wired into [launcher.Config].
type Runtime struct {
	launcherCfg *launcher.Config
	appName     string
}

// NewRuntime builds an in-process runner for appName using the ADK launcher config.
func NewRuntime(launcherCfg *launcher.Config, appName string) (*Runtime, error) {
	if launcherCfg == nil {
		return nil, fmt.Errorf("adkrun: launcher config is required")
	}
	if launcherCfg.AgentLoader == nil {
		return nil, fmt.Errorf("adkrun: AgentLoader is required")
	}
	if launcherCfg.SessionService == nil {
		return nil, fmt.Errorf("adkrun: SessionService is required")
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("adkrun: app name is required")
	}
	// Compaction is validated inside runner.New, and a runner is built per
	// request, so without a check here an unusable setting produces a runtime
	// that constructs cleanly and then fails every run.
	//
	// A nil Compaction is valid and means compaction is disabled, which is the
	// common case. Validate takes a nil receiver and reports no error for it, so
	// this needs no nil guard: adding one that rejects nil would break every
	// application that does not ask for compaction.
	if err := launcherCfg.Compaction.Validate(); err != nil {
		return nil, fmt.Errorf("adkrun: invalid Compaction: %w", err)
	}
	return &Runtime{launcherCfg: launcherCfg, appName: appName}, nil
}

// AppName returns the ADK application name this runtime executes.
func (rt *Runtime) AppName() string {
	return rt.appName
}

// LauncherConfig returns the launcher configuration backing this runtime.
// Callers must treat the returned value as read-only; mutating shared services
// or plugin lists affects all concurrent runs on this runtime.
func (rt *Runtime) LauncherConfig() *launcher.Config {
	return rt.launcherCfg
}

// RunSSE executes an agent turn and returns the session id plus an iterator of ADK
// session events. Callers range over events until the iterator completes or returns
// an error; set [RunRequest.Streaming] to receive partial model tokens.
func (rt *Runtime) RunSSE(ctx context.Context, req RunRequest) (string, iter.Seq2[*Event, error], error) {
	return rt.runSSE(ctx, req, rt.launcherCfg.PluginConfig)
}

// MergeExtraPlugins returns a plugin config with extraPlugins appended to the
// launcher plugin list. Multi-turn eval inference should call this once per run
// and reuse the result with [Runtime.RunSSEWithPluginConfig].
func (rt *Runtime) MergeExtraPlugins(extraPlugins ...*plugin.Plugin) runner.PluginConfig {
	base := rt.launcherCfg.PluginConfig
	if len(extraPlugins) == 0 {
		return base
	}
	merged := make([]*plugin.Plugin, 0, len(base.Plugins)+len(extraPlugins))
	merged = append(merged, base.Plugins...)
	merged = append(merged, extraPlugins...)
	return runner.PluginConfig{
		Plugins:      merged,
		CloseTimeout: base.CloseTimeout,
	}
}

// RunSSEWithPluginConfig is like [Runtime.RunSSE] but uses the supplied plugin
// config instead of the launcher default. Prefer this over repeated calls to
// [Runtime.RunSSEWithExtraPlugins] when the same extra plugins apply to every turn.
func (rt *Runtime) RunSSEWithPluginConfig(ctx context.Context, req RunRequest, pluginConfig runner.PluginConfig) (string, iter.Seq2[*Event, error], error) {
	return rt.runSSE(ctx, req, pluginConfig)
}

// RunSSEWithExtraPlugins is like [Runtime.RunSSE] but merges extraPlugins onto
// the launcher plugin list for this run only. Plugin order is launcher plugins
// first, then extraPlugins. Returns the same errors as [Runtime.RunSSE].
func (rt *Runtime) RunSSEWithExtraPlugins(ctx context.Context, req RunRequest, extraPlugins ...*plugin.Plugin) (string, iter.Seq2[*Event, error], error) {
	return rt.runSSE(ctx, req, rt.MergeExtraPlugins(extraPlugins...))
}

func (rt *Runtime) runSSE(ctx context.Context, req RunRequest, pluginConfig runner.PluginConfig) (string, iter.Seq2[*Event, error], error) {
	if strings.TrimSpace(req.UserID) == "" {
		return "", nil, fmt.Errorf("adkrun: user id is required")
	}
	if len(req.NewMessage.Parts) == 0 {
		return "", nil, fmt.Errorf("adkrun: newMessage.parts is required")
	}

	// Override appName from the request. Note: the same SessionService and
	// ArtifactService are used regardless of appName, so callers must ensure
	// these services are not app-scoped when using the override.
	appName := strings.TrimSpace(req.AppName)
	if appName == "" {
		appName = rt.appName
	}

	sessionService := rt.launcherCfg.SessionService
	memoryService := rt.launcherCfg.MemoryService
	artifactService := rt.launcherCfg.ArtifactService

	sessionID := req.SessionID
	if sessionID == "" {
		// Vertex AI memory bank does not currently support hyphens in session IDs.
		// Therefore we replace all hyphens with empty strings.
		// TODO: Remove this once Vertex AI memory bank supports hyphens.
		// See: https://github.com/googleapis/google-cloud-go/issues/14656
		sessionID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	curAgent, err := rt.launcherCfg.AgentLoader.LoadAgent(appName)
	if err != nil {
		return "", nil, fmt.Errorf("adkrun: load agent %q: %w", appName, err)
	}

	// Per-request runner matches the stock ADK REST server pattern. The runner
	// walks the agent tree (parentmap.New) and rebuilds the plugin manager on
	// each call — intentional for isolation between concurrent requests.
	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             curAgent,
		SessionService:    sessionService,
		MemoryService:     memoryService,
		ArtifactService:   artifactService,
		PluginConfig:      pluginConfig,
		AutoCreateSession: true,
		Compaction:        rt.launcherCfg.Compaction,
	})
	if err != nil {
		return "", nil, fmt.Errorf("adkrun: create runner: %w", err)
	}

	streamingMode := agent.StreamingModeNone
	if req.Streaming {
		streamingMode = agent.StreamingModeSSE
	}
	runCfg := agent.RunConfig{
		StreamingMode:             streamingMode,
		SaveInputBlobsAsArtifacts: req.SaveInputBlobsAsArtifacts,
	}

	var opts []runner.RunOption
	if req.StateDelta != nil {
		opts = append(opts, runner.WithStateDelta(req.StateDelta))
	}

	// HITL resume: ADK v2 runner resolves the invocation id from session history
	// and FunctionResponse ids in NewMessage (see https://adk.dev/tools-custom/confirmation/).
	// RunRequest.InvocationID and FunctionCallEventID are accepted for API parity only
	// and are not passed through to the runner.

	msg := req.NewMessage
	msg.Parts = slices.Clone(req.NewMessage.Parts)
	events := r.Run(ctx, req.UserID, sessionID, &msg, runCfg, opts...)
	label := fmt.Sprintf("app %q session %q", appName, sessionID)
	return sessionID, WithoutCompactionErrors(ctx, label, events), nil
}

// WithoutCompactionErrors logs compaction failures and drops them from the
// event stream, passing everything else through untouched. label identifies the
// run in the log line.
//
// Compaction is post-invocation bookkeeping: the turn's events are persisted
// before it runs, so a failure costs a smaller prompt on the next turn, not the
// answer the caller already received. Consumers treat a streamed error as
// terminal — the AG-UI executor emits RunError in place of RunFinished, and the
// scheduler records a failed cron tick — so passing the failure on would report
// a delivered answer as a failed run. Errors that are not
// [compaction.ErrCompaction] still reach the caller, because those mean the turn
// itself failed.
//
// [Runtime.RunSSE] applies this already. It is exported for callers that build
// their own runner from [Runtime.LauncherConfig] and so bypass RunSSE.
func WithoutCompactionErrors(ctx context.Context, label string, events iter.Seq2[*Event, error]) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		for ev, err := range events {
			if err != nil && errors.Is(err, compaction.ErrCompaction) {
				// Logged rather than silent: a summarizer that never succeeds
				// leaves sessions growing unbounded, and the first visible
				// symptom would otherwise be prompts failing against the
				// model's context limit, far from the cause.
				alog.Warnf(ctx, "adkrun: compaction failed for %s: %v", label, err)
				continue
			}
			if !yield(ev, err) {
				return
			}
		}
	}
}
