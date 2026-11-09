package tooloauth

import "context"

// nonInteractiveKey marks a context whose run nobody can answer.
type nonInteractiveKey struct{}

// WithNonInteractive returns ctx marked as a run nobody can answer, such as
// a scheduled job or an eval. Pass it to the runner: a configured tool with
// no token then fails with "connect <Provider> in chat first" instead of
// pausing for a consent that would never come.
func WithNonInteractive(ctx context.Context) context.Context {
	return context.WithValue(ctx, nonInteractiveKey{}, true)
}

// IsNonInteractive reports whether ctx was marked by WithNonInteractive.
func IsNonInteractive(ctx context.Context) bool {
	marked, _ := ctx.Value(nonInteractiveKey{}).(bool)
	return marked
}
