package agui

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"google.golang.org/adk/v2/session"
)

// isInternalStateKey reports session state keys managed by the launcher and omitted
// from client-visible StateSnapshot payloads.
//
// The _adk namespace carries launcher-owned graph data (see
// [stream.NodeOutputsStateKey]). Reserving it keeps node outputs, which travel
// on the same state channel as host application state, from being read back as
// host state and written into the ADK session on the next turn.
//
// The match is exact or followed by a separator, so a host key that merely
// starts with the same letters, such as "_adkish", stays visible.
func isInternalStateKey(key string) bool {
	if key == pendingInterruptsStateKey || strings.HasPrefix(key, "_agui_") {
		return true
	}
	return key == stream.NodeOutputsStateKey ||
		strings.HasPrefix(key, stream.NodeOutputsStateKey+".") ||
		strings.HasPrefix(key, stream.NodeOutputsStateKey+"_")
}

// loadSessionForSnapshot loads an existing ADK session for snapshot emission.
// Returns (nil, false, nil) when the session does not exist or session service
// is unset. Get errors are logged but treated as "session missing" because ADK
// has no sentinel not-found error (see loadPendingInterrupts for rationale).
func (l *aguiLauncher) loadSessionForSnapshot(ctx context.Context, appName, userID, sessionID string) (session.Session, bool, error) {
	sess, err := l.getSession(ctx, appName, userID, sessionID)
	if err != nil {
		log.Printf("agui: loadSessionForSnapshot: session.Get failed (treating as missing): %v", err)
		return nil, false, nil
	}
	if sess == nil {
		return nil, false, nil
	}
	return sess, true, nil
}

// ensureSessionForSnapshot returns an existing session or creates one so a run-start
// StateSnapshot can be emitted before adkrun.RunSSE (AutoCreateSession otherwise runs
// only inside the runner).
func (l *aguiLauncher) ensureSessionForSnapshot(ctx context.Context, appName, userID, sessionID string, initialState map[string]any) (session.Session, error) {
	sess, ok, _ := l.loadSessionForSnapshot(ctx, appName, userID, sessionID)
	if ok {
		return sess, nil
	}
	if l.sessionService == nil {
		return nil, nil
	}
	createResp, err := l.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     initialState,
	})
	if err != nil {
		return nil, fmt.Errorf("create session for snapshot: %w", err)
	}
	return createResp.Session, nil
}

// buildStateSnapshot merges persisted session state with optional request state from
// RunAgentInput, omitting launcher-internal keys.
func buildStateSnapshot(sess session.Session, reqState map[string]any) map[string]any {
	return stream.BuildStateSnapshot(sess, reqState, isInternalStateKey)
}

// buildMessagesSnapshot converts ADK session history to AG-UI messages for
// MESSAGES_SNAPSHOT events. Emitted at interrupt boundaries (always) and
// optionally at run end when WithMessagesSnapshotOnRunEnd is configured.
func (l *aguiLauncher) buildMessagesSnapshot(ctx context.Context, sess session.Session) ([]types.Message, error) {
	if sess == nil {
		return nil, nil
	}
	var opts []ConvertOption
	if l.config.genAIPartConverter != nil {
		opts = append(opts, WithPartConverter(l.config.genAIPartConverter))
	}
	if appName := strings.TrimSpace(sess.AppName()); appName != "" {
		opts = append(opts, WithRootAppName(appName))
	}
	return ConvertSessionToMessages(ctx, sess, opts...)
}

// emitStateSnapshotIfNonEmpty and emitMessagesSnapshotIfNonEmpty are defined in stream.go.

// withoutInternalKeys returns state with launcher-owned keys removed, for use
// wherever client-supplied state would otherwise become agent-visible.
//
// Client state is untrusted. Snapshots publish the _adk namespace to clients, so
// a client echoing it back is the normal case rather than an attack, and on a
// thread's first request that state becomes the ADK session's initial state,
// which is permanent and visible to the agent.
//
// The input map is returned unchanged when it holds nothing internal, which is
// the common case, and is never mutated: the caller still owns it.
func withoutInternalKeys(state map[string]any) map[string]any {
	var internal int
	for key := range state {
		if isInternalStateKey(key) {
			internal++
		}
	}
	if internal == 0 {
		return state
	}

	out := make(map[string]any, len(state)-internal)
	for key, val := range state {
		if !isInternalStateKey(key) {
			out[key] = val
		}
	}
	return out
}
