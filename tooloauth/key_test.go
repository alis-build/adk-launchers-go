package tooloauth

import (
	"testing"

	"google.golang.org/adk/v2/tool/toolauth"
)

// keyTestConfig returns a Config with one calendar_list tool asking for scopes.
func keyTestConfig(scopes ...string) Config {
	return Config{Tools: map[string]Tool{"calendar_list": {
		Provider:     "Google Calendar",
		ClientID:     "cid-7",
		ClientSecret: "secret-7",
		RedirectURI:  "https://agent.example.com/oauth/callback",
		Scopes:       scopes,
		Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
			AuthorizationURL: "https://accounts.example.com/o/oauth2/auth",
			TokenURL:         "https://oauth2.example.com/token",
		}}},
	}}}
}

func TestCredentialKey_matchesDocumentedEncoding(t *testing.T) {
	key, err := keyTestConfig("b.read", "a.write").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	// printf '%s' '12:tooloauth/v16:oauth242:https://accounts.example.com/o/oauth2/auth32:https://oauth2.example.com/token5:cid-740:https://agent.example.com/oauth/callback1:27:a.write6:b.read' | shasum -a 256
	const wantKey = "25f529e14749891a9d774b320137dff3a81789f7a2dcc9af421aab53dbd332ff"
	if key.Key != wantKey {
		t.Errorf("Key = %q, want %q", key.Key, wantKey)
	}
	if key.AppName != "app-1" || key.UserID != "user-7" {
		t.Errorf("AppName, UserID = %q, %q; want app-1, user-7", key.AppName, key.UserID)
	}
}

func TestCredentialKey_ignoresScopeOrder(t *testing.T) {
	a, err := keyTestConfig("a.write", "b.read").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	b, err := keyTestConfig("b.read", "a.write").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if a != b {
		t.Errorf("keys differ by scope order: %+v vs %+v", a, b)
	}
}

func TestCredentialKey_differsByScope(t *testing.T) {
	narrow, err := keyTestConfig("a.write").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	broad, err := keyTestConfig("a.write", "b.read").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if narrow.Key == broad.Key {
		t.Errorf("narrow and broad scopes share key %q", narrow.Key)
	}
}

func TestCredentialKey_unknownTool(t *testing.T) {
	_, err := keyTestConfig("a.write").CredentialKey("nope", "app-1", "user-7")
	if err == nil || err.Error() != `tooloauth: tool "nope" is not configured` {
		t.Errorf("CredentialKey() error = %v, want %q", err, `tooloauth: tool "nope" is not configured`)
	}
}

func TestCredentialKey_rejectsInvalidConfig(t *testing.T) {
	_, err := Config{}.CredentialKey("calendar_list", "app-1", "user-7")
	if err == nil || err.Error() != "tooloauth: Config.Tools is empty" {
		t.Errorf("CredentialKey() error = %v, want %q", err, "tooloauth: Config.Tools is empty")
	}
}

// Scopes are sorted but not deduplicated: a repeated scope names a
// different request, as auth/gcp keys it.
func TestCredentialKey_keepsDuplicateScopes(t *testing.T) {
	once, err := keyTestConfig("a.write").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	twice, err := keyTestConfig("a.write", "a.write").CredentialKey("calendar_list", "app-1", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if once.Key == twice.Key {
		t.Errorf("duplicate scope shares key %q", once.Key)
	}
}
