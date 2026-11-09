package tooloauth

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
)

// ErrNotConnected is the error a provider returns when the user has no usable
// token for the tool: they have not signed in, or their sign-in has lapsed.
// It is wrapped with the provider's name; match it with errors.Is.
var ErrNotConnected = errors.New("tooloauth: not connected")

// Provider returns the credential provider toolName sends its requests
// through, typically as &http.Client{Transport: &auth.Transport{Provider: p}}
// with the tool's context as each request's context. It reads the acting user
// from that context and serves the token the plugin keeps for them. It fails
// when c is invalid or toolName is not configured.
//
// The provider's errors are meant for the tool to return, and so reach the
// model and the session: they never name the user or carry a store's text.
func (c Config) Provider(toolName string) (auth.CredentialProvider, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	t, ok := c.Tools[toolName]
	if !ok {
		return nil, fmt.Errorf("tooloauth: tool %q is not configured", toolName)
	}
	r := newResolver(c)
	return auth.ProviderFunc(func(ctx context.Context) (auth.Credential, error) {
		id, ok := agent.IdentityFromContext(ctx)
		if !ok || id.UserID == "" {
			return nil, errors.New("tooloauth: no ADK invocation identity on the context")
		}
		tok, err := r.credential(ctx, toolName, id.AppName, id.UserID)
		switch {
		case errors.Is(err, errMiss):
			return nil, fmt.Errorf("%w to %s", ErrNotConnected, t.Provider)
		case err != nil:
			return nil, fmt.Errorf("tooloauth: %s sign-in is unavailable right now", t.Provider)
		}
		return tok, nil
	}), nil
}
