package tooloauth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"google.golang.org/adk/v2/auth"
)

// keyVersion prefixes every slot, so another producer sharing the store
// cannot collide with this one, and a future change to the slot's inputs can
// move to a new version without reading old entries as new ones.
const keyVersion = "tooloauth/v1"

// CredentialKey returns the store key under which the plugin keeps userID's
// token for toolName in appName. Use it to revoke a token with
// Config.Store.Delete when Store is set; the in-memory store a nil Store
// falls back to cannot be reached from outside.
//
// The slot is a SHA-256 digest of everything that changes which token comes
// back: the scheme type, both endpoints, the client ID, the redirect URI and
// the scopes, in any order. The tool name and the client secret are not part
// of it, so two tools with the same client and scopes share one consent, and
// rotating the secret does not sign everyone out. It fails when c is invalid
// or toolName is not configured.
func (c Config) CredentialKey(toolName, appName, userID string) (auth.CredentialKey, error) {
	if err := c.validate(); err != nil {
		return auth.CredentialKey{}, err
	}
	t, ok := c.Tools[toolName]
	if !ok {
		return auth.CredentialKey{}, fmt.Errorf("tooloauth: tool %q is not configured", toolName)
	}
	return auth.CredentialKey{AppName: appName, UserID: userID, Key: t.slot()}, nil
}

// slot digests t's sign-in inputs. Each field is length-prefixed, as
// auth/gcp's joinFields does, so no character inside a field can make two
// different inputs encode alike. Scopes are sorted but not deduplicated.
func (t Tool) slot() string {
	authURL, tokenURL, _ := t.endpoints()
	scopes := slices.Sorted(slices.Values(t.Scopes))
	fields := append([]string{
		keyVersion, string(t.Scheme.Type), authURL, tokenURL, t.ClientID, t.RedirectURI,
		strconv.Itoa(len(scopes)),
	}, scopes...)
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte(':')
		b.WriteString(f)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
