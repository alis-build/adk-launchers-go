package agui

import (
	"context"
	"fmt"
	"log"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
)

// pendingInterruptsStateKey is the ADK session state key used to persist open
// AG-UI interrupts between runs on the same thread.
const pendingInterruptsStateKey = "_agui_pending_interrupts"

// invocationIDFromClientState reads state.adk.invocationId from RunAgentInput.state.
func invocationIDFromClientState(state any) string {
	stateMap, ok := state.(map[string]any)
	if !ok || stateMap == nil {
		return ""
	}
	adkMeta, ok := stateMap["adk"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := adkMeta["invocationId"].(string)
	return id
}

// confirmationInvocationIndex builds a confirmationCallID → invocationID map
// from session events in a single pass.
func confirmationInvocationIndex(sess session.Session) map[string]string {
	if sess == nil {
		return nil
	}
	out := make(map[string]string)
	for ev := range sess.Events().All() {
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part == nil || part.FunctionCall == nil {
				continue
			}
			if part.FunctionCall.Name != toolconfirmation.FunctionCallName {
				continue
			}
			if part.FunctionCall.ID != "" && ev.InvocationID != "" {
				out[part.FunctionCall.ID] = ev.InvocationID
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveInvocationIDForResume picks an ADK invocation id for resume runs.
// Priority: pending record → client state → session event index.
func resolveInvocationIDForResume(pending []interrupt.Record, entries []types.ResumeEntry, clientState any, sess session.Session) string {
	if id := interrupt.InvocationIDFromPending(pending, entries); id != "" {
		return id
	}
	if id := invocationIDFromClientState(clientState); id != "" {
		return id
	}
	if sess == nil {
		return ""
	}
	idx := confirmationInvocationIndex(sess)
	if idx == nil {
		return ""
	}
	for _, entry := range entries {
		if entry.InterruptID == "" {
			continue
		}
		if id, ok := idx[entry.InterruptID]; ok && id != "" {
			return id
		}
	}
	return ""
}

// getSession loads an ADK session by app/user/session id.
func (l *aguiLauncher) getSession(ctx context.Context, appName, userID, sessionID string) (session.Session, error) {
	if l.sessionService == nil {
		return nil, nil
	}
	getResp, err := l.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}
	return getResp.Session, nil
}

// loadPendingInterrupts reads the persisted interrupt records from ADK session state.
// Returns nil (not an error) when no session exists or the state key is absent, because
// the absence of pending interrupts is the normal case for non-resume runs.
func (l *aguiLauncher) loadPendingInterrupts(ctx context.Context, appName, userID, sessionID string) ([]interrupt.Record, error) {
	sess, err := l.getSession(ctx, appName, userID, sessionID)
	if err != nil {
		log.Printf("agui: loadPendingInterrupts: session.Get failed (treating as no pending): %v", err)
		return nil, nil
	}
	if sess == nil {
		return nil, nil
	}
	raw, err := sess.State().Get(pendingInterruptsStateKey)
	if err != nil {
		return nil, nil
	}
	return interrupt.DecodeRecords(raw)
}

// writePendingInterruptsState persists or clears interrupt records by appending a synthetic
// event to the ADK session with a StateDelta targeting the pendingInterruptsStateKey.
// Passing nil records clears the key (used after clean success runs).
func (l *aguiLauncher) writePendingInterruptsState(ctx context.Context, appName, userID, sessionID string, records []interrupt.Record) error {
	sess, err := l.getSession(ctx, appName, userID, sessionID)
	if err != nil {
		return fmt.Errorf("load session for pending interrupts: %w", err)
	}
	if sess == nil {
		return nil
	}
	ev := session.NewEvent("")
	ev.Author = "agui"
	ev.Actions.StateDelta = map[string]any{
		pendingInterruptsStateKey: records,
	}
	return l.sessionService.AppendEvent(ctx, sess, ev)
}

// persistPendingInterrupts converts emitted AG-UI interrupts to Records and writes
// them to session state so the next resume run can validate against them.
func (l *aguiLauncher) persistPendingInterrupts(ctx context.Context, appName, userID, sessionID string, interrupts []types.Interrupt) error {
	return l.writePendingInterruptsState(ctx, appName, userID, sessionID, interrupt.RecordsFromInterrupts(interrupts))
}

// clearPendingInterrupts removes stale interrupt records from session state after a
// clean success run, so they don't block future runs on the same thread.
func (l *aguiLauncher) clearPendingInterrupts(ctx context.Context, appName, userID, sessionID string) error {
	return l.writePendingInterruptsState(ctx, appName, userID, sessionID, nil)
}
