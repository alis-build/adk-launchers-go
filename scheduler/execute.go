package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/internal/threadmeta"
	"go.alis.build/alog"
	pb "go.alis.build/common/alis/agui/scheduler"
	"go.alis.build/iam/v3"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// cronHandler returns the HTTP handler mounted at schedulerext.HandlerPath.
//
// svc provides cron storage; rt runs the agent; cfg controls identity and sync behavior.
// The returned handler uses the system identity for SchedulerService RPCs and impersonates
// the cron owner inside executeCron.
func cronHandler(
	svc pb.SchedulerServiceServer,
	rt cronRunner,
	systemIdentity *iam.Identity,
	cfg *cronConfig,
) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := incomingContext(r)
		// SchedulerService reads/writes use the system service account.
		ctx = systemIdentity.Context(ctx)

		var body struct {
			// ID is the cron resource id (without the "crons/" prefix).
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return writeCronFailed(w, "decode request body")
		}
		if strings.TrimSpace(body.ID) == "" {
			return writeCronFailed(w, "cron id is required")
		}

		cron, err := svc.GetCron(ctx, &pb.GetCronRequest{Name: "crons/" + body.ID})
		if err != nil {
			return writeCronFailed(w, err.Error())
		}
		// Archived crons are acknowledged without re-running the agent.
		if cron.GetState() == pb.Cron_STATE_ARCHIVED {
			return writeCronOK(w)
		}

		ownerID, err := ownerFromCron(cron)
		if err != nil {
			return writeCronFailed(w, err.Error())
		}
		if err := validateCronForRun(cron); err != nil {
			return writeCronFailed(w, err.Error())
		}

		alog.Infof(ctx, "scheduler: executing cron %s", body.ID)

		// Detach from request cancellation so async runs and logging
		// complete after the HTTP response is sent.
		detachedCtx := context.WithoutCancel(ctx)

		if cfg.syncExecution {
			if err := executeCron(detachedCtx, svc, rt, cfg, cron, ownerID); err != nil {
				return writeCronFailed(w, err.Error())
			}
			return writeCronOK(w)
		}

		go func() {
			if err := executeCron(detachedCtx, svc, rt, cfg, cron, ownerID); err != nil {
				alog.Errorf(detachedCtx, "scheduler: cron %s: %v", cron.GetName(), err)
			}
		}()
		return writeCronOK(w)
	}
}

// executeCron runs the agent for one cron tick and updates cron metadata in Spanner.
//
// ctx must carry the system identity (for UpdateCron). ADK runs use userRunContext.
// Behavior matches the stock extension handler: initial_prompt seeding for TYPE_CRON,
// session reuse via thread, and TYPE_AT archival after a successful run.
//
// On failure, last_failure_time and last_failure_message are persisted. TYPE_AT jobs
// are also archived so they do not remain STATE_ACTIVE without a backing Cloud Task.
// TYPE_CRON jobs stay active so the next schedule tick can retry. Successful runs
// include last_failure_time and last_failure_message in the update mask so
// SchedulerService Prune clears any prior failure metadata.
//
// Returns a non-nil error only for agent failures. UpdateCron failures are logged
// and reported to the [CronObserver] but do not cause a returned error, preventing
// Cloud Tasks from retrying an already-completed agent run.
func executeCron(
	ctx context.Context,
	svc pb.SchedulerServiceServer,
	rt cronRunner,
	cfg *cronConfig,
	cron *pb.Cron,
	ownerID string,
) error {
	if cfg.observer != nil {
		cfg.observer.OnCronStarted(ctx, cron)
	}
	var runErr error
	defer func() {
		if cfg.observer != nil {
			cfg.observer.OnCronFinished(ctx, cron, runErr)
		}
	}()

	appName := threadmeta.ResolveCronAppName(cron.GetAgentId(), cfg.defaultAppName)
	if err := threadmeta.ValidateCronAppName(cfg.launcherCfg, appName); err != nil {
		// Configuration error: do not archive TYPE_AT. The agent is misconfigured,
		// not the schedule, and archival would make the failure invisible to any
		// operator who redeploys with the fix.
		runErr = errors.Join(err, persistCronFailure(ctx, svc, cron, err, false))
		return runErr
	}

	sessionID := threadmeta.ThreadIDFromResource(cron.GetThread())
	owner := ownerIdentity(ownerID, cron.GetEmail())

	promptCall := cronPromptCall{
		rt:             rt,
		cfg:            cfg,
		cron:           cron,
		owner:          owner,
		defaultAppName: appName,
	}

	// Recurring crons: run initial_prompt once before the first regular prompt.
	if cron.GetType() == pb.Cron_TYPE_CRON && sessionID == "" && strings.TrimSpace(cron.GetInitialPrompt()) != "" {
		id, err := runCronPrompt(ctx, promptCall.with(sessionID, cron.GetInitialPrompt(), CronRunInitial))
		if err != nil {
			runErr = fmt.Errorf("initial run: %w", err)
			runErr = errors.Join(runErr, persistCronFailure(ctx, svc, cron, runErr, true))
			return runErr
		}
		sessionID = mergeSessionID(sessionID, id)
	}

	id, err := runCronPrompt(ctx, promptCall.with(sessionID, cron.GetPrompt(), CronRunScheduled))
	if err != nil {
		runErr = fmt.Errorf("run: %w", err)
		runErr = errors.Join(runErr, persistCronFailure(ctx, svc, cron, runErr, true))
		return runErr
	}
	sessionID = mergeSessionID(sessionID, id)

	now := timestamppb.Now()
	threadResource := threadmeta.ThreadResource(sessionID)
	update := &pb.Cron{
		Name:        cron.GetName(),
		Thread:      threadResource,
		LastRunTime: now,
	}
	// Include last_failure_time and last_failure_message in the mask (with unset
	// values on the update) so SchedulerService clears any prior failure metadata
	// on a successful run.
	paths := []string{"last_run_time", "last_failure_time", "last_failure_message"}
	if cron.GetThread() != threadResource && threadResource != "" {
		paths = append(paths, "thread")
	}
	if cron.GetType() == pb.Cron_TYPE_AT {
		update.State = pb.Cron_STATE_ARCHIVED
		update.ArchiveTime = now
		paths = append(paths, "state", "archive_time")
	}

	// Log UpdateCron failures instead of returning them. The agent run already
	// succeeded, so returning an error here would cause Cloud Tasks to retry
	// and duplicate the agent execution. The observer still sees the persist
	// error via runErr so operators can track persist failures separately.
	//
	// Trade-off: TYPE_AT crons will not be archived on persist failure, so
	// Cloud Tasks may re-invoke them. This is preferred over the alternative
	// (returning error → guaranteed retry → guaranteed duplicate execution).
	if _, err := svc.UpdateCron(ctx, &pb.UpdateCronRequest{
		Cron:       update,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
	}); err != nil {
		runErr = fmt.Errorf("persist cron: %w", err)
		alog.Errorf(ctx, "scheduler: update cron %s after successful run: %v", cron.GetName(), err)
	}
	return nil
}

const maxCronFailureMessageLen = 2048

// persistCronFailure records the last failure on the cron resource and returns
// any error from the underlying UpdateCron so callers can surface it (e.g. via
// [errors.Join] with the run error) to observers.
//
// When archiveTerminal is true and the cron is TYPE_AT, the cron is also archived
// because a Cloud Task has already fired against a one-shot schedule that will
// not fire again. Pass false for pre-run configuration errors, where archival
// would silently hide the misconfiguration from operators after a redeploy.
//
// A nil runErr is a no-op.
func persistCronFailure(ctx context.Context, svc pb.SchedulerServiceServer, cron *pb.Cron, runErr error, archiveTerminal bool) error {
	if runErr == nil {
		return nil
	}

	now := timestamppb.Now()
	update := &pb.Cron{
		Name:            cron.GetName(),
		LastFailureTime: now,
	}
	paths := []string{"last_failure_time", "last_failure_message"}
	if msg, ok := cronFailureMessage(runErr); ok {
		update.LastFailureMessage = &msg
	}
	if archiveTerminal && cron.GetType() == pb.Cron_TYPE_AT {
		update.State = pb.Cron_STATE_ARCHIVED
		update.ArchiveTime = now
		paths = append(paths, "state", "archive_time")
	}

	if _, err := svc.UpdateCron(ctx, &pb.UpdateCronRequest{
		Cron:       update,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
	}); err != nil {
		alog.Errorf(ctx, "scheduler: update cron %s after failed run: %v", cron.GetName(), err)
		return fmt.Errorf("persist cron failure: %w", err)
	}
	return nil
}

// cronFailureMessage returns the truncated, UTF-8-safe error string suitable for
// persistence in Cron.last_failure_message. The second return is false when the
// message is empty after trimming, so callers can leave the field unset rather
// than storing a present-but-empty string.
func cronFailureMessage(runErr error) (string, bool) {
	msg := truncateCronFailureMessage(runErr.Error())
	if msg == "" {
		return "", false
	}
	return msg, true
}

// truncateCronFailureMessage trims whitespace and caps the message at
// maxCronFailureMessageLen bytes, backing up to the previous rune boundary so
// the returned string is always valid UTF-8. Otherwise a multi-byte rune split
// mid-sequence would produce an invalid UTF-8 payload that the proto/RPC layer
// would reject when writing the update.
func truncateCronFailureMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxCronFailureMessageLen {
		return msg
	}
	end := maxCronFailureMessageLen
	for end > 0 && !utf8.RuneStart(msg[end]) {
		end--
	}
	return msg[:end]
}

// cronPromptCall bundles the tick-scoped inputs to runCronPrompt so the two
// call sites in executeCron don't have to repeat 5+ positional arguments per
// invocation. Use [cronPromptCall.with] to attach the per-invocation fields
// (sessionID, prompt, kind).
type cronPromptCall struct {
	rt             cronRunner
	cfg            *cronConfig
	cron           *pb.Cron
	owner          *iam.Identity
	defaultAppName string

	sessionID string
	prompt    string
	kind      CronRunKind
}

// with returns a copy of the call bound to a specific invocation.
func (c cronPromptCall) with(sessionID, prompt string, kind CronRunKind) cronPromptCall {
	c.sessionID = sessionID
	c.prompt = prompt
	c.kind = kind
	return c
}

func runCronPrompt(ctx context.Context, c cronPromptCall) (string, error) {
	run := &CronRunContext{
		Cron:      c.cron,
		OwnerID:   c.owner.ID,
		SessionID: c.sessionID,
		Prompt:    c.prompt,
		AppName:   c.defaultAppName,
		Kind:      c.kind,
		Metadata:  c.cron.GetMetadata(),
	}

	runCtx, err := c.cfg.chainBeforeRun(ctx, run)
	if err != nil {
		return c.sessionID, err
	}

	userCtx := userRunContext(runCtx, c.owner)

	threadID := threadmeta.ThreadIDFromResource(c.cron.GetThread())
	if threadID == "" {
		threadID = c.sessionID
	}
	if c.cfg.threadService != nil && threadID != "" {
		threadmeta.Upsert(userCtx, c.cfg.threadService, c.owner, c.cfg.launcherCfg, c.cfg.defaultAppName, threadID, run.AppName, run.Prompt)
	}

	newSessionID, runErr := c.rt.RunCron(userCtx, adkrun.RunRequest{
		AppName:    run.AppName,
		UserID:     c.owner.ID,
		SessionID:  c.sessionID,
		NewMessage: adkrun.UserTextMessage(run.Prompt),
		StateDelta: run.StateDelta,
	})
	c.cfg.chainAfterRun(runCtx, run, newSessionID, runErr)

	// Post-run upsert only when ADK returned a new session id that we haven't
	// already upserted above. This avoids a redundant CreateOrUpdateThread when
	// a continuing cron reuses the same thread id (idempotent, but wasteful),
	// and matches the AGUI interactive path which upserts once per run.
	if runErr == nil && c.cfg.threadService != nil && newSessionID != "" && newSessionID != threadID {
		threadmeta.Upsert(userCtx, c.cfg.threadService, c.owner, c.cfg.launcherCfg, c.cfg.defaultAppName, newSessionID, run.AppName, run.Prompt)
	}
	return newSessionID, runErr
}

func ownerIdentity(ownerID, email string) *iam.Identity {
	if email == "" {
		email = ownerID
	}
	user := &iam.Identity{ID: ownerID, Email: email, Type: iam.User}
	if strings.HasSuffix(email, ".iam.gserviceaccount.com") {
		user.Type = iam.ServiceAccount
	}
	return user
}

// runtimeAdapter adapts [adkrun.Runtime] to [cronRunner].
type runtimeAdapter struct {
	rt *adkrun.Runtime
}

func (a *runtimeAdapter) RunCron(ctx context.Context, req adkrun.RunRequest) (string, error) {
	sessionID, events, err := a.rt.RunSSE(ctx, req)
	if err != nil {
		return "", err
	}
	for _, eventErr := range events {
		if eventErr != nil {
			return "", eventErr
		}
	}
	return sessionID, nil
}

// userRunContext returns a context that runs ADK as the cron owner.
func userRunContext(parent context.Context, owner *iam.Identity) context.Context {
	ctx := owner.OutgoingMetadata(parent)
	return owner.Context(ctx)
}

// incomingContext copies HTTP headers into gRPC incoming metadata for downstream RPCs.
func incomingContext(r *http.Request) context.Context {
	md := metadata.MD{}
	for k, vs := range r.Header {
		md[strings.ToLower(k)] = append([]string(nil), vs...)
	}
	return metadata.NewIncomingContext(r.Context(), md)
}

// writeCronOK writes a 200 JSON response acknowledging the cron invocation.
func writeCronOK(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(cronResponse{Status: "OK"})
	return nil
}

// writeCronFailed writes a 500 JSON response; Cloud Tasks may retry depending on queue config.
func writeCronFailed(w http.ResponseWriter, msg string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(cronResponse{Status: "FAILED", Error: msg})
	return nil
}
