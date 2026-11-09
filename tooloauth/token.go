package tooloauth

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/v2/auth"
)

// Token is the OAuth token the plugin stores for a user and tool. It is a
// plain value, unlike ADK's auth.OAuth2Credential, which wraps a live token
// source and cannot be written to a persistent store.
//
// Its JSON encoding holds the secrets in clear text: a store must encrypt it.
// Printing a Token, or a pointer to one, with fmt or slog shows them as
// <redacted>, with three exceptions. fmt prints %p of a value and %w of a
// non-error without calling Format, and cannot redact a Token held in an
// unexported field. slog's JSON handler marshals a Token nested in another
// struct, slice or map, secrets included. Log the Token itself, never a
// value that contains it.
type Token struct {
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	// Expiry is when the access token expires; zero when the token endpoint
	// reported no lifetime.
	Expiry time.Time `json:"expiry,omitzero"`
}

var _ auth.Credential = (*Token)(nil)

const redactedText = "<redacted>"

// Apply implements [auth.Credential]: it sets the Authorization header to the
// token type and the access token. An empty type, or "bearer" in any case,
// is sent as "Bearer", the normalisation golang.org/x/oauth2 applies, since
// some providers reject the lowercase form. It fails, leaving h untouched, when
// there is no access token.
func (t *Token) Apply(h http.Header) error {
	if t.AccessToken == "" {
		return errors.New("tooloauth: token has no access token")
	}
	typ := t.TokenType
	if typ == "" || strings.EqualFold(typ, "bearer") {
		typ = "Bearer"
	}
	h.Set("Authorization", typ+" "+t.AccessToken)
	return nil
}

// Format implements [fmt.Formatter] for every verb, printing AccessToken and
// RefreshToken as <redacted> when set.
func (t Token) Format(f fmt.State, verb rune) {
	// plain has Token's fields without its Format method, so printing it does
	// not recurse.
	type plain Token
	p := plain(t)
	for _, s := range []*string{&p.AccessToken, &p.RefreshToken} {
		if *s != "" {
			*s = redactedText
		}
	}
	out := fmt.Sprintf(fmt.FormatString(f, verb), p)
	if rest, ok := strings.CutPrefix(out, "tooloauth.plain"); ok {
		out = "tooloauth.Token" + rest
	}
	// fmt.State writes into fmt's own buffer, so there is no error to handle.
	_, _ = io.WriteString(f, out)
}

// LogValue implements [slog.LogValuer] with the text Format prints, so a
// logged token never shows its secrets.
func (t Token) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("%+v", t))
}
