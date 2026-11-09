package tooloauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// calendarConfig returns a Config whose only tool is calendar_list, signing
// in to "Google Calendar" through store.
func calendarConfig(store auth.CredentialStore) Config {
	return Config{
		Store: store,
		Tools: map[string]Tool{"calendar_list": {
			Provider:     "Google Calendar",
			ClientID:     "cid-7",
			ClientSecret: "secret-7",
			RedirectURI:  "https://agent.example.com/oauth/callback",
			Scopes:       []string{"calendar.read"},
			Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
				AuthorizationURL: "https://accounts.example.com/o/oauth2/auth",
				TokenURL:         "https://oauth2.example.com/token",
			}}},
		}},
	}
}

// seed stores tok for userID in appName under calendar_list's key.
func seed(t *testing.T, cfg Config, appName, userID string, tok *Token, expiresAt time.Time) {
	t.Helper()
	key, err := cfg.CredentialKey("calendar_list", appName, userID)
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if err := cfg.Store.Set(context.Background(), key, tok, expiresAt); err != nil {
		t.Fatalf("Store.Set() error = %v", err)
	}
}

func TestPlugin_storedToken_toolRequestCarriesIt(t *testing.T) {
	cfg := calendarConfig(auth.NewInMemoryCredentialStore())
	seed(t, cfg, "app-hit", "user-7", &Token{AccessToken: "at-seeded-42", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-hit", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-seeded-42" {
		t.Errorf("resource server saw Authorization %q, want one %q", got, "Bearer at-seeded-42")
	}
	if hasFunctionCall(events, "adk_request_confirmation") {
		t.Error("run asked for confirmation, want none")
	}
	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response")
	}
	if _, isErr := resp["error"]; isErr {
		t.Errorf("calendar_list response = %v, want no error", resp)
	}
}

// The store fails, so a plugin that consulted it for ping would stop the
// call with a store error.
func TestPlugin_unconfiguredToolRunsUntouched(t *testing.T) {
	cfg := calendarConfig(failingStore{auth.NewInMemoryCredentialStore()})
	ran := 0
	ping, err := functiontool.New(functiontool.Config{Name: "ping", Description: "Pings."},
		func(agent.Context, struct{}) (map[string]any, error) {
			ran++
			return map[string]any{"pong": true}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	h := newHarness(t, "app-hit", "ping", cfg, ping)

	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("ping", genai.RoleUser))

	if ran != 1 {
		t.Errorf("ping ran %d times, want 1", ran)
	}
}

// failingStore is a CredentialStore whose Get always fails with a message
// naming the user, as a real backend's error might.
type failingStore struct{ auth.CredentialStore }

func (failingStore) Get(context.Context, auth.CredentialKey) (auth.Credential, bool, error) {
	return nil, false, errors.New("backend down for user-7")
}

func TestPlugin_storeErrorIsToolError(t *testing.T) {
	cfg := calendarConfig(failingStore{auth.NewInMemoryCredentialStore()})
	rs := newResourceServer(t)
	h := newHarness(t, "app-hit", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response")
	}
	msg, _ := resp["error"].(string)
	if msg != "Google Calendar sign-in is unavailable right now" {
		t.Errorf("error = %q, want %q", msg, "Google Calendar sign-in is unavailable right now")
	}
	if strings.Contains(msg, "user-7") {
		t.Errorf("error %q names the user", msg)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list handler ran %d times, want 0", got)
	}
}

// The provider is called from a tool the plugin does not intercept, so the
// miss reaches it rather than the plugin's consent pause.
func TestProvider_missIsErrNotConnected(t *testing.T) {
	cfg := calendarConfig(auth.NewInMemoryCredentialStore())
	p, err := cfg.Provider("calendar_list")
	if err != nil {
		t.Fatalf("Provider() error = %v", err)
	}
	var credErr error
	probe, err := functiontool.New(functiontool.Config{Name: "probe", Description: "Probes."},
		func(ctx agent.Context, _ struct{}) (map[string]any, error) {
			_, credErr = p.Credential(ctx)
			return map[string]any{}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	newHarness(t, "app-hit", "probe", cfg, probe).run(t.Context(), "user-7", "s-1", genai.NewContentFromText("probe", genai.RoleUser))

	if !errors.Is(credErr, ErrNotConnected) || credErr.Error() != "tooloauth: not connected to Google Calendar" {
		t.Errorf("Credential() error = %v, want ErrNotConnected %q", credErr, "tooloauth: not connected to Google Calendar")
	}
}

func TestProvider_withoutIdentityFails(t *testing.T) {
	p, err := calendarConfig(auth.NewInMemoryCredentialStore()).Provider("calendar_list")
	if err != nil {
		t.Fatalf("Provider() error = %v", err)
	}
	_, err = p.Credential(context.Background())
	if err == nil || err.Error() != "tooloauth: no ADK invocation identity on the context" {
		t.Errorf("Credential() error = %v, want %q", err, "tooloauth: no ADK invocation identity on the context")
	}
}

func TestProvider_unknownTool(t *testing.T) {
	_, err := calendarConfig(auth.NewInMemoryCredentialStore()).Provider("nope")
	if err == nil || err.Error() != `tooloauth: tool "nope" is not configured` {
		t.Errorf("Provider() error = %v, want %q", err, `tooloauth: tool "nope" is not configured`)
	}
}

// opaqueCredential is an auth.Credential the plugin did not store.
type opaqueCredential struct{}

func (opaqueCredential) Apply(http.Header) error { return nil }

// A credential that is not a usable *Token, whether another producer's or
// an expired one, is a miss.
func TestProvider_unusableStoredCredentialIsAMiss(t *testing.T) {
	for name, cred := range map[string]auth.Credential{
		"other type":  opaqueCredential{},
		"expired":     &Token{AccessToken: "at-old-5", Expiry: time.Now().Add(-time.Minute)},
		"empty":       &Token{},
		"within skew": &Token{AccessToken: "at-6", Expiry: time.Now().Add(5 * time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := calendarConfig(auth.NewInMemoryCredentialStore())
			key, err := cfg.CredentialKey("calendar_list", "app-hit", "user-7")
			if err != nil {
				t.Fatalf("CredentialKey() error = %v", err)
			}
			if err := cfg.Store.Set(context.Background(), key, cred, time.Now().Add(time.Hour)); err != nil {
				t.Fatalf("Store.Set() error = %v", err)
			}
			p, err := cfg.Provider("calendar_list")
			if err != nil {
				t.Fatalf("Provider() error = %v", err)
			}
			var credErr error
			probe, err := functiontool.New(functiontool.Config{Name: "probe", Description: "Probes."},
				func(ctx agent.Context, _ struct{}) (map[string]any, error) {
					_, credErr = p.Credential(ctx)
					return map[string]any{}, nil
				})
			if err != nil {
				t.Fatalf("functiontool.New() error = %v", err)
			}
			newHarness(t, "app-hit", "probe", cfg, probe).run(t.Context(), "user-7", "s-1", genai.NewContentFromText("probe", genai.RoleUser))

			if !errors.Is(credErr, ErrNotConnected) {
				t.Errorf("Credential() error = %v, want ErrNotConnected", credErr)
			}
		})
	}
}
