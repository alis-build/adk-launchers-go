package scheduler

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.alis.build/adk/launchers/internal/adkrun"
	historyservice "go.alis.build/agui/history/service"
	"go.alis.build/alog"
	pb "go.alis.build/common/alis/agui/scheduler"
	"go.alis.build/iam/v3"
	adklauncher "google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/protobuf/types/known/structpb"
)

// cronConfig holds runtime options for the Cloud Tasks cron handler and executeCron.
// Populated via [Option] functions on [NewLauncher].
type cronConfig struct {
	// systemIdentity is the IAM principal for SchedulerService GetCron and UpdateCron.
	// When nil, [defaultSystemIdentity] is used at handler construction time.
	systemIdentity *iam.Identity
	// syncExecution when true blocks the HTTP response until executeCron completes.
	syncExecution bool
	// observer receives lifecycle callbacks; nil disables observation.
	observer CronObserver
	// runInterceptors are invoked around each ADK run inside executeCron.
	runInterceptors []CronRunInterceptor
	// threadService when set enables thread metadata upserts on cron runs.
	threadService *historyservice.ThreadService
	// launcherCfg supplies AgentLoader for thread display names and agent validation.
	launcherCfg *adklauncher.Config
	// defaultAppName is the ADK app used when cron.agent_id is empty.
	defaultAppName string
}

// resolveSystemIdentity returns the configured system identity or the environment default.
func (cfg *cronConfig) resolveSystemIdentity() *iam.Identity {
	if cfg.systemIdentity != nil {
		return cfg.systemIdentity
	}
	return defaultSystemIdentity()
}

// defaultSystemIdentity returns alis-build@$ALIS_OS_PROJECT.iam.gserviceaccount.com
// when ALIS_OS_PROJECT is set; otherwise nil and the handler will reject requests.
func defaultSystemIdentity() *iam.Identity {
	projectID := os.Getenv("ALIS_OS_PROJECT")
	if projectID == "" {
		return nil
	}
	email := fmt.Sprintf("alis-build@%s.iam.gserviceaccount.com", projectID)
	return &iam.Identity{ID: email, Email: email, Type: iam.ServiceAccount}
}

// cronRunner executes ADK user prompts during cron ticks.
// [*adkrun.Runtime] satisfies this interface in production via runtimeAdapter; tests use fakes.
type cronRunner interface {
	RunCron(ctx context.Context, req adkrun.RunRequest) (sessionID string, err error)
}

// cronResponse is the JSON body returned to Cloud Tasks for both success and failure.
type cronResponse struct {
	// Status is "OK" or "FAILED".
	Status string `json:"status"`
	// Error is set when Status is "FAILED".
	Error string `json:"error,omitempty"`
}

// CronObserver receives lifecycle notifications for in-process cron execution.
// Use [WithCronObserver] to register an implementation for metrics, tracing, or logging.
//
// Implementations must be safe for concurrent use: multiple cron goroutines may call
// these methods when [WithSynchronousExecution] is false (the default).
type CronObserver interface {
	// OnCronStarted is called at the beginning of executeCron, before any ADK run.
	OnCronStarted(ctx context.Context, cron *pb.Cron)
	// OnCronFinished is called when executeCron returns. err is nil if both the
	// agent run and cron persist succeeded; non-nil for agent or persist failures.
	OnCronFinished(ctx context.Context, cron *pb.Cron, err error)
}

// CronRunKind identifies which prompt is being executed inside a cron tick.
// A recurring cron with a non-empty initial_prompt fires both kinds on its first
// tick (initial, then scheduled); subsequent ticks fire only [CronRunScheduled].
// One-shot TYPE_AT crons only ever fire [CronRunScheduled].
type CronRunKind int

const (
	// CronRunInitial is the one-time initial_prompt seed run for TYPE_CRON jobs.
	// It only fires on the first tick of a recurring cron and only when the cron
	// has no persisted thread yet — subsequent ticks skip it because the ADK
	// session already carries the seed messages.
	CronRunInitial CronRunKind = iota + 1
	// CronRunScheduled is the main recurring or one-shot prompt run. It fires on
	// every tick after the optional initial run and is the ADK invocation whose
	// returned session id is persisted back onto cron.thread.
	CronRunScheduled
)

// CronRunContext describes a single ADK invocation inside executeCron. A new
// CronRunContext is constructed per ADK invocation and handed to every
// registered [CronRunInterceptor] in order; the same instance flows from
// BeforeRun through the ADK call to AfterRun.
//
// BeforeRun interceptors may mutate Prompt, AppName, and StateDelta before the
// executor uses them. Cron and Metadata are effectively read-only: they are
// provided as context for routing/observability decisions and are not written
// back to storage. SessionID reflects the id in play before this specific ADK
// call — after the initial run, the scheduled run sees the session id returned
// by ADK.
type CronRunContext struct {
	// Cron is the full cron proto for this tick. Read-only.
	Cron *pb.Cron
	// OwnerID is the "users/{id}" tail from cron.owner. Read-only.
	OwnerID string
	// SessionID is the ADK session id in play for this invocation, or empty for
	// the first run of a cron that has no persisted thread yet. Read-only:
	// interceptors that mutate SessionID have no effect on the ADK dispatch —
	// the executor uses its own local copy taken before BeforeRun is called.
	SessionID string
	// Prompt is the user text passed to ADK. BeforeRun may mutate.
	Prompt string
	// AppName is the ADK app to run. Defaults to cron.agent_id or the launcher
	// default (see [threadmeta.ResolveCronAppName]). BeforeRun may mutate; the
	// resolved value is re-validated against the AgentLoader before dispatch.
	AppName string
	// Kind identifies which prompt this invocation is running.
	Kind CronRunKind
	// Metadata is cron.metadata (google.protobuf.Struct), exposed for
	// interceptors that route or annotate ADK runs based on cron-scoped config.
	// Read-only: the executor does not forward Metadata to ADK on its own.
	Metadata *structpb.Struct
	// StateDelta is passed to ADK as the initial session state delta for this
	// invocation. BeforeRun may set or extend it; the default is nil.
	StateDelta map[string]any
}

// CronRunInterceptor hooks each ADK run inside a cron tick (initial_prompt and
// main prompt are separate invocations and see separate BeforeRun/AfterRun
// pairs). It runs at a finer granularity than [CronObserver], which wraps the
// whole tick.
//
// Register with [WithCronRunInterceptor] or [WithCronRunInterceptors].
// Implementations must be safe for concurrent use: multiple cron ticks may
// invoke these methods concurrently when [WithSynchronousExecution] is false.
//
// Ordering. When multiple interceptors are registered, BeforeRun is invoked in
// registration order and AfterRun is invoked in the same order (not reversed).
// If BeforeRun returns a non-nil error, the failing interceptor short-circuits
// the chain: subsequent BeforeRun and AfterRun calls are not made, the ADK run
// is skipped, and the cron tick fails (the observer sees the error and Cloud
// Tasks may retry per queue policy). If AfterRun returns a non-nil error, the
// error is logged and the remaining AfterRun calls still run; the tick is not
// failed by AfterRun errors. This mirrors the AGUI launcher's After semantics.
//
// Mutation. BeforeRun may mutate CronRunContext.Prompt, AppName, and StateDelta.
// The returned context replaces the context passed to ADK and to subsequent
// interceptors' AfterRun calls. Cron and Metadata are effectively read-only.
type CronRunInterceptor interface {
	// BeforeRun fires immediately before the ADK invocation for run. Return
	// the (possibly wrapped) context that the ADK call and later AfterRun
	// calls should use. A non-nil error aborts the tick.
	BeforeRun(ctx context.Context, run *CronRunContext) (context.Context, error)
	// AfterRun fires after the ADK invocation returns. sessionID is the id
	// returned by ADK (possibly empty on failure) and err is the ADK error
	// (nil on success). Returned errors are logged and do not fail the tick.
	AfterRun(ctx context.Context, run *CronRunContext, sessionID string, err error) error
}

func (cfg *cronConfig) chainBeforeRun(ctx context.Context, run *CronRunContext) (context.Context, error) {
	for _, ic := range cfg.runInterceptors {
		if ic == nil {
			continue
		}
		next, err := ic.BeforeRun(ctx, run)
		if err != nil {
			return ctx, err
		}
		// Defensive: an interceptor returning a nil context is treated as
		// "keep the incoming context" instead of overwriting it and panicking
		// downstream in metadata.NewOutgoingContext.
		if next != nil {
			ctx = next
		}
	}
	return ctx, nil
}

func (cfg *cronConfig) chainAfterRun(ctx context.Context, run *CronRunContext, sessionID string, err error) {
	for i, ic := range cfg.runInterceptors {
		if ic == nil {
			continue
		}
		if afterErr := ic.AfterRun(ctx, run, sessionID, err); afterErr != nil {
			alog.Errorf(ctx, "scheduler: CronRunInterceptor[%d] (%T) AfterRun: %v", i, ic, afterErr)
		}
	}
}

// ownerFromCron extracts the user id from cron.owner, which must be users/{id}.
func ownerFromCron(cron *pb.Cron) (string, error) {
	owner := strings.TrimSpace(cron.GetOwner())
	id, ok := strings.CutPrefix(owner, "users/")
	if !ok || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("cron %s: invalid owner %q (expected users/{id})", cron.GetName(), owner)
	}
	return id, nil
}

// mergeSessionID returns the ADK session id to persist on the cron.
// A non-empty returned id from RunCron wins; otherwise the existing id is kept.
func mergeSessionID(existing, returned string) string {
	if returned != "" {
		return returned
	}
	return existing
}

// validateCronForRun rejects crons that cannot produce a user message for the agent.
func validateCronForRun(cron *pb.Cron) error {
	if strings.TrimSpace(cron.GetPrompt()) == "" {
		return fmt.Errorf("cron %s: prompt is required", cron.GetName())
	}
	return nil
}
