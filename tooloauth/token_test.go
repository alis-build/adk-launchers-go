package tooloauth

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestToken_Apply_writesBearerForEmptyType(t *testing.T) {
	h := http.Header{}
	if err := (&Token{AccessToken: "at-42"}).Apply(h); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer at-42" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer at-42")
	}
}

func TestToken_Apply_normalisesLowercaseBearer(t *testing.T) {
	h := http.Header{}
	if err := (&Token{AccessToken: "at-42", TokenType: "bearer"}).Apply(h); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer at-42" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer at-42")
	}
}

func TestToken_Apply_keepsOtherTypes(t *testing.T) {
	h := http.Header{}
	if err := (&Token{AccessToken: "at-43", TokenType: "MAC"}).Apply(h); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := h.Get("Authorization"); got != "MAC at-43" {
		t.Errorf("Authorization = %q, want %q", got, "MAC at-43")
	}
}

func TestToken_Apply_rejectsEmptyAccessToken(t *testing.T) {
	h := http.Header{}
	err := (&Token{RefreshToken: "rt-1"}).Apply(h)
	if err == nil || err.Error() != "tooloauth: token has no access token" {
		t.Errorf("Apply() error = %v, want %q", err, "tooloauth: token has no access token")
	}
	if len(h) != 0 {
		t.Errorf("header = %v, want untouched", h)
	}
}

func TestToken_Format_redactsSecrets(t *testing.T) {
	tok := Token{AccessToken: "at-42", RefreshToken: "rt-42", TokenType: "Bearer"}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		t.Run(verb, func(t *testing.T) {
			out := fmt.Sprintf(verb, tok)
			if strings.Contains(out, "at-42") || strings.Contains(out, "rt-42") {
				t.Errorf("Sprintf(%s) = %q, leaks a token", verb, out)
			}
			if !strings.Contains(out, "<redacted>") {
				t.Errorf("Sprintf(%s) = %q, want <redacted>", verb, out)
			}
		})
	}
}

func TestToken_Format_pointerRedactsSecrets(t *testing.T) {
	out := fmt.Sprintf("%+v", &Token{AccessToken: "at-42", RefreshToken: "rt-42"})
	if strings.Contains(out, "at-42") || strings.Contains(out, "rt-42") {
		t.Errorf("Sprintf(%%+v, &tok) = %q, leaks a token", out)
	}
}

func TestToken_LogValue_redactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("token", "tok", Token{AccessToken: "at-42", RefreshToken: "rt-42"})
	if out := buf.String(); strings.Contains(out, "at-42") || strings.Contains(out, "rt-42") {
		t.Errorf("log = %q, leaks a token", out)
	}
}
