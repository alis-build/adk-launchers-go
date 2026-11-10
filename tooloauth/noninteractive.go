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

// noStoredTokensKey marks a context whose run must not use stored tokens.
type noStoredTokensKey struct{}

// WithoutStoredTokens returns ctx marked as a run that must not use anyone's
// stored token, such as an eval run executing as a real user: an eval of a
// tool that writes would otherwise act on that user's account. Pass it to
// the runner: every configured tool then fails with "<Provider> is not
// available in this run", connected or not.
func WithoutStoredTokens(ctx context.Context) context.Context {
	return context.WithValue(ctx, noStoredTokensKey{}, true)
}

// StoredTokensDisabled reports whether ctx was marked by WithoutStoredTokens.
func StoredTokensDisabled(ctx context.Context) bool {
	marked, _ := ctx.Value(noStoredTokensKey{}).(bool)
	return marked
}
