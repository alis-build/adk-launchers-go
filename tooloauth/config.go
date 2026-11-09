package tooloauth

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool/toolauth"
)

// DefaultRefreshTokenTTL is how long a refreshable token is kept after it was
// stored or last refreshed when Config.RefreshTokenTTL is zero.
const DefaultRefreshTokenTTL = 90 * 24 * time.Hour

// Config says which tools need the user's OAuth token and where tokens are
// kept. NewPlugin, Config.Provider and Config.CredentialKey all validate it
// and fail on the same errors.
type Config struct {
	// Tools maps a tool name to how it signs in. It must not be empty.
	Tools map[string]Tool
	// Store keeps users' tokens. Nil means one in-memory store shared by the
	// whole process, which loses every token on restart; a warning is logged
	// the first time it is used.
	Store auth.CredentialStore
	// RefreshTokenTTL is how long a refreshable token is kept after it was
	// stored or last refreshed, so a user who keeps using a tool stays signed
	// in. Zero means DefaultRefreshTokenTTL; negative, or no longer than
	// auth.ExpirySkew, is rejected.
	RefreshTokenTTL time.Duration
	// HTTPClient calls the token endpoint. Nil means http.DefaultClient.
	HTTPClient *http.Client
}

// Tool says how one tool signs in to its provider.
type Tool struct {
	// Provider is the name users see, such as "Google Calendar". Required.
	Provider string
	// Scheme is an oauth2 scheme with an authorization code flow, or an
	// openIdConnect scheme with inline authorization and token endpoints.
	// Discovery through openIdConnectUrl is not supported. Scopes belong in
	// Tool.Scopes, not here.
	Scheme toolauth.Scheme
	// ClientID and ClientSecret identify the OAuth client. Both are required:
	// only confidential clients are supported.
	ClientID     string
	ClientSecret string
	// Scopes are the scopes the tool asks for. They are listed only here, so
	// the authorization URI, the store key and the config sent to the client
	// cannot disagree.
	Scopes []string
	// RedirectURI is the client's registered redirect URI. A client that
	// reports its own redirect URI on resume overrides it for the exchange.
	RedirectURI string
}

// validate returns the first problem with c, checking tools in sorted name
// order so the error does not depend on map iteration.
func (c Config) validate() error {
	if len(c.Tools) == 0 {
		return errors.New("tooloauth: Config.Tools is empty")
	}
	if c.RefreshTokenTTL < 0 {
		return errors.New("tooloauth: RefreshTokenTTL must not be negative")
	}
	// A store declines to keep anything within auth.ExpirySkew of expiry, so
	// a TTL that short would store no token and loop every sign-in.
	if c.RefreshTokenTTL > 0 && c.RefreshTokenTTL <= auth.ExpirySkew {
		return fmt.Errorf("tooloauth: RefreshTokenTTL must be longer than %s", auth.ExpirySkew)
	}
	names := make([]string, 0, len(c.Tools))
	for name := range c.Tools {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := c.Tools[name].validate(); err != nil {
			return fmt.Errorf("tooloauth: tool %q: %w", name, err)
		}
	}
	return nil
}

// validate returns the first problem with t, without the tool name.
func (t Tool) validate() error {
	if t.Provider == "" {
		return errors.New("Provider is required")
	}
	if t.ClientID == "" || t.ClientSecret == "" {
		return errors.New("ClientID and ClientSecret are required")
	}
	switch t.Scheme.Type {
	case toolauth.SchemeTypeOAuth2:
		if _, _, ok := t.endpoints(); !ok {
			return errors.New("oauth2 scheme needs an authorizationCode flow with authorizationUrl and tokenUrl")
		}
	case toolauth.SchemeTypeOpenIDConnect:
		if _, _, ok := t.endpoints(); !ok {
			return errors.New("openIdConnect scheme needs authorization_endpoint and token_endpoint")
		}
	default:
		return fmt.Errorf("unsupported scheme type %q", t.Scheme.Type)
	}
	if len(t.Scheme.Scopes) > 0 || (t.authorizationCodeFlow() != nil && len(t.authorizationCodeFlow().Scopes) > 0) {
		return errors.New("list scopes in Tool.Scopes, not in the scheme")
	}
	return nil
}

// authorizationCodeFlow returns the scheme's authorization code flow, or nil.
func (t Tool) authorizationCodeFlow() *toolauth.OAuthFlow {
	if t.Scheme.Flows == nil {
		return nil
	}
	return t.Scheme.Flows.AuthorizationCode
}

// endpoints returns the authorization and token endpoints, taken from where
// adk-python looks for them: the authorization code flow of an oauth2
// scheme, or the inline endpoints of an openIdConnect one. ok is false when
// either is missing.
func (t Tool) endpoints() (authURL, tokenURL string, ok bool) {
	switch t.Scheme.Type {
	case toolauth.SchemeTypeOAuth2:
		if f := t.authorizationCodeFlow(); f != nil {
			authURL, tokenURL = f.AuthorizationURL, f.TokenURL
		}
	case toolauth.SchemeTypeOpenIDConnect:
		authURL, tokenURL = t.Scheme.AuthorizationEndpoint, t.Scheme.TokenEndpoint
	}
	return authURL, tokenURL, authURL != "" && tokenURL != ""
}

// defaultStore is the store every Config without one shares, created on
// first use so a process that always sets Store never logs the warning.
var defaultStore = sync.OnceValue(func() auth.CredentialStore {
	slog.Warn("tooloauth: no Config.Store set; tokens are kept in memory and lost on restart")
	return auth.NewInMemoryCredentialStore()
})

// store returns c.Store, or the process-wide in-memory store when it is nil.
func (c Config) store() auth.CredentialStore {
	if c.Store != nil {
		return c.Store
	}
	return defaultStore()
}

// cloneTools copies tools deeply enough that the scopes and endpoints a
// running plugin uses are its own: a caller changing its Config after
// NewPlugin or Provider changes nothing the plugin reads. Fields the plugin
// never reads, such as the schemes' Extra maps, stay shared.
func cloneTools(tools map[string]Tool) map[string]Tool {
	out := make(map[string]Tool, len(tools))
	for name, t := range tools {
		t.Scopes = slices.Clone(t.Scopes)
		t.Scheme = cloneScheme(t.Scheme)
		out[name] = t
	}
	return out
}

// cloneScheme deep-copies the parts of s the plugin reads or writes: its
// flows, their scopes, and its own scopes.
func cloneScheme(s toolauth.Scheme) toolauth.Scheme {
	s.Scopes = slices.Clone(s.Scopes)
	if s.Flows != nil {
		flows := *s.Flows
		for _, f := range []**toolauth.OAuthFlow{&flows.Implicit, &flows.Password, &flows.ClientCredentials, &flows.AuthorizationCode} {
			if *f != nil {
				flow := **f
				flow.Scopes = maps.Clone(flow.Scopes)
				*f = &flow
			}
		}
		s.Flows = &flows
	}
	return s
}
