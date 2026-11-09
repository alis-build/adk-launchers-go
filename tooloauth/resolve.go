package tooloauth

import (
	"context"
	"errors"
	"time"

	"google.golang.org/adk/v2/auth"
)

var (
	// errMiss reports that the user has no usable token for the tool.
	errMiss = errors.New("tooloauth: no usable token")
	// errStore reports that the store failed. It wraps nothing, because a
	// store's error text may name the key, and so the user.
	errStore = errors.New("tooloauth: credential store failed")
)

// resolver finds the user's token for a configured tool. The plugin and
// every provider built from one Config read the same store.
type resolver struct {
	cfg   Config
	store auth.CredentialStore
}

// newResolver returns a resolver for cfg, which must be valid.
func newResolver(cfg Config) *resolver {
	return &resolver{cfg: cfg, store: cfg.store()}
}

// credential returns userID's usable token for toolName in appName. It
// returns errMiss when there is none, and errStore when the store fails.
func (r *resolver) credential(ctx context.Context, toolName, appName, userID string) (*Token, error) {
	key := auth.CredentialKey{AppName: appName, UserID: userID, Key: r.cfg.Tools[toolName].slot()}
	cred, ok, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, errStore
	}
	tok, isToken := cred.(*Token)
	if !ok || !isToken || tok == nil || !tok.usable(time.Now()) {
		return nil, errMiss
	}
	return tok, nil
}

// usable reports whether t has an access token that is still good at now,
// allowing auth.ExpirySkew, the margin stores and producers share.
func (t *Token) usable(now time.Time) bool {
	return t.AccessToken != "" && (t.Expiry.IsZero() || t.Expiry.After(now.Add(auth.ExpirySkew)))
}
