package tooloauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// recordingStore is an in-memory CredentialStore that remembers the
// expiresAt of its last Set.
type recordingStore struct {
	auth.CredentialStore
	mu            sync.Mutex
	lastExpiresAt time.Time
}

func newRecordingStore() *recordingStore {
	return &recordingStore{CredentialStore: auth.NewInMemoryCredentialStore()}
}

func (s *recordingStore) Set(ctx context.Context, key auth.CredentialKey, cred auth.Credential, expiresAt time.Time) error {
	s.mu.Lock()
	s.lastExpiresAt = expiresAt
	s.mu.Unlock()
	return s.CredentialStore.Set(ctx, key, cred, expiresAt)
}

func (s *recordingStore) expiresAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastExpiresAt
}

// tokenEndpoint is a fake RFC 6749 token endpoint answering every request
// with status and body after checking it with check.
type tokenEndpoint struct {
	*httptest.Server
	mu    sync.Mutex
	calls int
}

func newTokenEndpoint(t *testing.T, status int, body string, check func(*testing.T, *http.Request)) *tokenEndpoint {
	t.Helper()
	te := &tokenEndpoint{}
	// TLS, so only Config.HTTPClient (the server's client) can reach it and a
	// refresh that ignored HTTPClient would fail.
	te.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		te.mu.Lock()
		te.calls++
		te.mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Errorf("token endpoint: ParseForm: %v", err)
		}
		if check != nil {
			check(t, r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(te.Close)
	return te
}

func (te *tokenEndpoint) callCount() int {
	te.mu.Lock()
	defer te.mu.Unlock()
	return te.calls
}

// refreshConfig returns calendarConfig's tool pointed at te's token
// endpoint, with store and ttl.
func refreshConfig(store auth.CredentialStore, te *tokenEndpoint, ttl time.Duration) Config {
	cfg := calendarConfig(store)
	tl := cfg.Tools["calendar_list"]
	tl.Scheme = toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
		AuthorizationURL: "https://accounts.example.com/o/oauth2/auth",
		TokenURL:         te.URL + "/token",
	}}}
	cfg.Tools = map[string]Tool{"calendar_list": tl}
	cfg.HTTPClient = te.Client()
	cfg.RefreshTokenTTL = ttl
	return cfg
}

// storedToken returns the token cfg's store holds for user-7 in app-refresh.
func storedToken(t *testing.T, cfg Config) (*Token, bool) {
	t.Helper()
	key, err := cfg.CredentialKey("calendar_list", "app-refresh", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	cred, ok, err := cfg.Store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Store.Get() error = %v", err)
	}
	if !ok {
		return nil, false
	}
	tok, isToken := cred.(*Token)
	if !isToken {
		t.Fatalf("stored credential is %T, want *Token", cred)
	}
	return tok, true
}

// checkRefreshForm asserts a refresh_token grant for refreshToken from
// client cid-7 authenticated with HTTP Basic.
func checkRefreshForm(refreshToken string) func(*testing.T, *http.Request) {
	return func(t *testing.T, r *http.Request) {
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.PostForm.Get("refresh_token"); got != refreshToken {
			t.Errorf("refresh_token = %q, want %q", got, refreshToken)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "cid-7" || pass != "secret-7" {
			t.Errorf("Basic auth = %q, %q, %v; want cid-7, secret-7", user, pass, ok)
		}
	}
}

// expiresWithin reports an error unless got lies in [start+ttl, end+ttl].
func expiresWithin(t *testing.T, got, start, end time.Time, ttl time.Duration) {
	t.Helper()
	if got.Before(start.Add(ttl)) || got.After(end.Add(ttl)) {
		t.Errorf("expiresAt = %v, want between %v and %v", got, start.Add(ttl), end.Add(ttl))
	}
}

func TestRefresh_expiredAccessToken_refreshesAndRuns(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-new-10","token_type":"Bearer","expires_in":3600}`, checkRefreshForm("rt-old-9"))
	store := newRecordingStore()
	cfg := refreshConfig(store, te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	start := time.Now()
	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	end := time.Now()

	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-new-10" {
		t.Errorf("resource server saw %q, want one %q", got, "Bearer at-new-10")
	}
	tok, ok := storedToken(t, cfg)
	if !ok || tok.AccessToken != "at-new-10" || tok.RefreshToken != "rt-old-9" {
		t.Errorf("stored token = %+v, %v; want access at-new-10, refresh rt-old-9", tok, ok)
	}
	expiresWithin(t, store.expiresAt(), start, end, 72*time.Hour)
}

func TestRefresh_rotatedRefreshTokenIsKept(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-new-10","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-new-11"}`, checkRefreshForm("rt-old-9"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if tok, ok := storedToken(t, cfg); !ok || tok.RefreshToken != "rt-new-11" {
		t.Errorf("stored token = %+v, %v; want refresh rt-new-11", tok, ok)
	}
}

func TestRefresh_refusedRefreshDropsTheEntry(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_grant"}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if tok, ok := storedToken(t, cfg); ok {
		t.Errorf("stored token = %+v, want the entry dropped", tok)
	}
	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
}

func TestRefresh_zeroTTLUsesNinetyDays(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-new-10","token_type":"Bearer","expires_in":3600}`, checkRefreshForm("rt-old-9"))
	store := newRecordingStore()
	cfg := refreshConfig(store, te, 0)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	start := time.Now()
	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	end := time.Now()

	expiresWithin(t, store.expiresAt(), start, end, 2160*time.Hour)
}

func TestRefresh_expiredWithoutRefreshTokenIsAMiss(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-new-10","token_type":"Bearer","expires_in":3600}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-12", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if got := te.callCount(); got != 0 {
		t.Errorf("token endpoint called %d times, want 0", got)
	}
}

func TestRefresh_unreachableEndpointKeepsTheEntry(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	te.Close()
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if tok, ok := storedToken(t, cfg); !ok || tok.RefreshToken != "rt-old-9" {
		t.Errorf("stored token = %+v, %v; want the entry kept", tok, ok)
	}
	resp, _ := functionResponse(events, "calendar_list")
	if msg, _ := resp["error"].(string); msg != "Google Calendar sign-in is unavailable right now" {
		t.Errorf("error = %q, want %q", msg, "Google Calendar sign-in is unavailable right now")
	}
}

// A provider outage is not a bad refresh token: the user stays signed in.
func TestRefresh_serverErrorKeepsTheEntry(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusServiceUnavailable, `<html>upstream unavailable</html>`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if tok, ok := storedToken(t, cfg); !ok || tok.RefreshToken != "rt-old-9" {
		t.Errorf("stored token = %+v, %v; want the entry kept", tok, ok)
	}
	resp, _ := functionResponse(events, "calendar_list")
	if msg, _ := resp["error"].(string); msg != "Google Calendar sign-in is unavailable right now" {
		t.Errorf("error = %q, want %q", msg, "Google Calendar sign-in is unavailable right now")
	}
}

// invalid_client means the client is misconfigured, not that this user's
// refresh token is bad; deleting on it would sign every user out in turn.
func TestRefresh_invalidClientKeepsTheEntry(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusUnauthorized, `{"error":"invalid_client"}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if _, ok := storedToken(t, cfg); !ok {
		t.Error("entry dropped on invalid_client, want it kept")
	}
}
