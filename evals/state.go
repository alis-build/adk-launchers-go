package evals

import "google.golang.org/adk/v2/agent"

// createEmptyState returns default session state for eval cases created from sessions.
// Go agents do not expose instruction templates on the public Agent interface, so this
// returns an empty map matching the common Python initial_session.state default.
func createEmptyState(_ agent.Agent) map[string]any {
	return map[string]any{}
}
