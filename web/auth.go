package web

import (
	"fmt"
	"net/http"

	iam "go.alis.build/iam/v3"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/server/authn"
	"google.golang.org/adk/v2/server/authz"
)

// IdentityAuthenticator authenticates ADK REST API requests from the alis
// identity that the auth gateway put on the request context.
//
// Like [DefaultAuthGateway], it trusts that identity: authentication happened
// upstream, and the gateway only carries its result. A request with no
// identity is unauthenticated, which ADK's REST server answers with 401.
//
// The caller's UserID is the identity ID, the same user ID the AG-UI launcher
// runs sessions under, so a strict authorizer lets a caller reach exactly the
// sessions their AG-UI threads use.
func IdentityAuthenticator() authn.Authenticator {
	return authn.NewCustom(func(r *http.Request) (*authn.Caller, error) {
		id, err := iam.FromContext(r.Context())
		if err != nil {
			return nil, fmt.Errorf("%w: no alis identity on the request", authn.ErrUnauthenticated)
		}
		// An identity without an ID would act as user "", which a strict
		// authorizer lets through for any request naming that user.
		if id.ID == "" {
			return nil, fmt.Errorf("%w: alis identity has no ID", authn.ErrUnauthenticated)
		}
		return &authn.Caller{
			UserID: id.ID,
			Claims: map[string]any{"email": id.Email, "type": string(id.Type)},
		}, nil
	})
}

// applyAuthDefaults fills ADK's REST API authentication and authorization
// hooks the embedder left nil: callers are authenticated from the forwarded
// identity, and authz.NewStrict lets a caller act only as their own user.
//
// A value the embedder set is never replaced, and there is no flag, so an
// operator cannot loosen it by accident. In code, Config.Authorizer =
// authz.NewNoop() lets any signed-in caller act as any user; a fully open API
// also needs Config.Authenticator = authn.NewNoop().
//
// It must run before the sublaunchers are set up: ADK's api sublauncher hands
// both hooks to adkrest.NewServer while it builds its routes.
func applyAuthDefaults(config *launcher.Config) {
	if config.Authenticator == nil {
		config.Authenticator = IdentityAuthenticator()
	}
	if config.Authorizer == nil {
		config.Authorizer = authz.NewStrict()
	}
}
