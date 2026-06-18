package stream

import (
	"context"
)

// ConfigureRun initializes per-run metadata on State before processing events.
// The mappings map is assigned directly (not copied) because the caller in
// executor.go builds a fresh map for each run — no shared ownership.
func (s *State) ConfigureRun(runID, threadID, userID, rootAppName string, runCtx context.Context, reqState map[string]any, mappings map[string][]PredictStateMapping) {
	s.RunID = runID
	s.ThreadID = threadID
	s.UserID = userID
	s.RootAppName = rootAppName
	s.RunCtx = runCtx
	s.ReqState = reqState
	s.PredictStateMappings = mappings
}
