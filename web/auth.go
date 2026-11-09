package web

import (
	"fmt"
	"net/http"

	iam "go.alis.build/iam/v3"
	"google.golang.org/adk/v2/server/authn"
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
		return &authn.Caller{
			UserID: id.ID,
			Claims: map[string]any{"email": id.Email, "type": string(id.Type)},
		}, nil
	})
}
