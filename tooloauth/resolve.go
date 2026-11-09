package tooloauth

import (
	"context"
	"errors"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/adk/v2/auth"
)

var (
	// errMiss reports that the user has no usable token for the tool.
	errMiss = errors.New("tooloauth: no usable token")
	// errStore reports that the store failed. It wraps nothing, because a
	// store's error text may name the key, and so the user.
	errStore = errors.New("tooloauth: credential store failed")
	// errEndpoint reports that a refresh failed for a reason other than the
	// refresh token being bad: the endpoint was unreachable, answered with a
	// server or client-configuration error, or sent no usable token. The
	// stored token is kept: the user is still signed in.
	errEndpoint = errors.New("tooloauth: token endpoint unreachable")
)

// resolver finds the user's token for a configured tool, refreshing an
// expired one. The plugin and every provider built from one Config read the
// same store.
type resolver struct {
	cfg   Config
	store auth.CredentialStore
	ttl   time.Duration
}

// newResolver returns a resolver for cfg, which must be valid.
func newResolver(cfg Config) *resolver {
	ttl := cfg.RefreshTokenTTL
	if ttl == 0 {
		ttl = DefaultRefreshTokenTTL
	}
	return &resolver{cfg: cfg, store: cfg.store(), ttl: ttl}
}

// credential returns userID's usable token for toolName in appName,
// refreshing an expired access token that has a refresh token. It returns
// errMiss when there is no usable token, and errStore or errEndpoint when the
// store or the token endpoint fails. Only a refresh the endpoint refuses with
// invalid_grant deletes the entry, since only that says the refresh token is
// bad; an outage or a misconfigured client must not sign users out.
func (r *resolver) credential(ctx context.Context, toolName, appName, userID string) (*Token, error) {
	t := r.cfg.Tools[toolName]
	key := auth.CredentialKey{AppName: appName, UserID: userID, Key: t.slot()}
	cred, ok, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, errStore
	}
	tok, isToken := cred.(*Token)
	if !ok || !isToken || tok == nil {
		return nil, errMiss
	}
	if tok.usable(time.Now()) {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, errMiss
	}
	fresh, err := r.refresh(ctx, t, tok)
	if re, ok := errors.AsType[*oauth2.RetrieveError](err); ok && re.ErrorCode == "invalid_grant" {
		if err := r.store.Delete(ctx, key); err != nil {
			return nil, errStore
		}
		return nil, errMiss
	}
	if err != nil {
		return nil, errEndpoint
	}
	if err := r.save(ctx, key, fresh, time.Now()); err != nil {
		return nil, err
	}
	return fresh, nil
}

// save stores tok under key until r.expiresAt(tok, now), or not at all when
// that leaves no more than auth.ExpirySkew: declining to cache a spent token
// is the producer's job under the CredentialStore contract.
func (r *resolver) save(ctx context.Context, key auth.CredentialKey, tok *Token, now time.Time) error {
	expiresAt := r.expiresAt(tok, now)
	if !expiresAt.After(now.Add(auth.ExpirySkew)) {
		return nil
	}
	if err := r.store.Set(ctx, key, tok, expiresAt); err != nil {
		return errStore
	}
	return nil
}

// expiresAt is how long tok is worth keeping. A refreshable token, or one
// that never expires, is kept for the refresh-token TTL from now, so each
// refresh renews it; any other token only until its access token expires.
func (r *resolver) expiresAt(tok *Token, now time.Time) time.Time {
	if tok.RefreshToken != "" || tok.Expiry.IsZero() {
		return now.Add(r.ttl)
	}
	return tok.Expiry
}

// refresh trades old's refresh token for a new token. The new token keeps
// the old refresh token when the endpoint sends none. A non-2xx answer from
// the endpoint is an *oauth2.RetrieveError, whose text includes the response
// body: never print or wrap it.
func (r *resolver) refresh(ctx context.Context, t Tool, old *Token) (*Token, error) {
	src := r.oauth2Config(t, t.RedirectURI).TokenSource(r.tokenContext(ctx), &oauth2.Token{
		RefreshToken: old.RefreshToken,
		// Expired, so the source refreshes instead of returning it.
		Expiry: time.Unix(1, 0),
	})
	ot, err := src.Token()
	if err != nil {
		return nil, err
	}
	return &Token{AccessToken: ot.AccessToken, TokenType: ot.TokenType, RefreshToken: ot.RefreshToken, Expiry: ot.Expiry}, nil
}

// oauth2Config returns the OAuth client for t, sending the client secret
// with HTTP Basic (client_secret_basic, adk-python's default) so oauth2 does
// not probe both styles.
func (r *resolver) oauth2Config(t Tool, redirectURI string) *oauth2.Config {
	authURL, tokenURL, _ := t.endpoints()
	return &oauth2.Config{
		ClientID:     t.ClientID,
		ClientSecret: t.ClientSecret,
		Scopes:       t.Scopes,
		RedirectURL:  redirectURI,
		Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInHeader},
	}
}

// tokenContext returns ctx carrying Config.HTTPClient for oauth2's
// token-endpoint calls, when one is set.
func (r *resolver) tokenContext(ctx context.Context) context.Context {
	if r.cfg.HTTPClient == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, r.cfg.HTTPClient)
}

// usable reports whether t has an access token that is still good at now,
// allowing auth.ExpirySkew, the margin stores and producers share.
func (t *Token) usable(now time.Time) bool {
	return t.AccessToken != "" && (t.Expiry.IsZero() || t.Expiry.After(now.Add(auth.ExpirySkew)))
}
