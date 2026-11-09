package tooloauth

import (
	"crypto/rand"
	"encoding/base64"
	"slices"

	"golang.org/x/oauth2"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool/toolauth"
)

// consentHint is the hint the plugin sends with a sign-in request.
const consentHint = "Sign in to %s to continue."

// authRequest builds the auth config a client needs to sign the user in to
// t, in the shape adk-python sends: the scheme with t's scopes written in,
// the client ID and redirect URI, and the authorization URI and its fresh
// state. The client secret is stripped, since the config is persisted in the
// session and sent to the client. key.Key becomes the config's credential
// key.
func (r *resolver) authRequest(t Tool, key auth.CredentialKey) (*toolauth.Config, error) {
	state, err := newState()
	if err != nil {
		return nil, err
	}
	// access_type=offline and prompt=consent are what adk-python sends, so
	// the provider returns a refresh token.
	authURI := r.oauth2Config(t, t.RedirectURI).AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	credType := toolauth.CredentialTypeOAuth2
	if t.Scheme.Type == toolauth.SchemeTypeOpenIDConnect {
		credType = toolauth.CredentialTypeOpenIDConnect
	}
	client := toolauth.OAuth2Auth{
		ClientID:                t.ClientID,
		ClientSecret:            t.ClientSecret,
		RedirectURI:             t.RedirectURI,
		TokenEndpointAuthMethod: "client_secret_basic",
	}
	exchanged := client
	exchanged.AuthURI = authURI
	exchanged.State = state
	cfg := &toolauth.Config{
		AuthScheme:              schemeWithScopes(t),
		RawAuthCredential:       &toolauth.Credential{AuthType: credType, OAuth2: &client},
		ExchangedAuthCredential: &toolauth.Credential{AuthType: credType, OAuth2: &exchanged},
		CredentialKey:           key.Key,
	}
	return cfg.WithoutSecrets()
}

// schemeWithScopes returns a fresh copy of t's scheme with t.Scopes written
// where the scheme type keeps them: the authorization code flow's scope map
// for oauth2, the scheme's scope list for openIdConnect. Its flows and
// scopes are fresh, so writing the scopes leaves the resolver's Config
// valid.
func schemeWithScopes(t Tool) toolauth.Scheme {
	s := cloneScheme(t.Scheme)
	if f := s.Flows; f != nil && f.AuthorizationCode != nil && s.Type == toolauth.SchemeTypeOAuth2 {
		scopes := make(map[string]string, len(t.Scopes))
		for _, scope := range t.Scopes {
			scopes[scope] = ""
		}
		f.AuthorizationCode.Scopes = scopes
	}
	if s.Type == toolauth.SchemeTypeOpenIDConnect {
		s.Scopes = slices.Clone(t.Scopes)
	}
	return s
}

// newState returns 32 random bytes, base64url-encoded without padding (43
// characters), for the OAuth state parameter.
func newState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
