package tooloauth

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// credentialCall returns the only adk_request_credential call in events.
func credentialCall(t *testing.T, events []*session.Event) *genai.FunctionCall {
	t.Helper()
	calls := namedCalls(events, toolauth.RequestCredentialFunctionCallName)
	if len(calls) != 1 {
		t.Fatalf("credential requests = %d, want 1", len(calls))
	}
	return calls[0]
}

// confirmationCall returns the only adk_request_confirmation call in events:
// a tool's own approval request.
func confirmationCall(t *testing.T, events []*session.Event) *genai.FunctionCall {
	t.Helper()
	calls := namedCalls(events, "adk_request_confirmation")
	if len(calls) != 1 {
		t.Fatalf("confirmation requests = %d, want 1", len(calls))
	}
	return calls[0]
}

func namedCalls(events []*session.Event, name string) []*genai.FunctionCall {
	var calls []*genai.FunctionCall
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionCall != nil && p.FunctionCall.Name == name {
				calls = append(calls, p.FunctionCall)
			}
		}
	}
	return calls
}

// pausedPayload returns a credential request's auth config as a JSON map,
// the form a client echoes back.
func pausedPayload(t *testing.T, fc *genai.FunctionCall) map[string]any {
	t.Helper()
	raw, err := json.Marshal(fc.Args)
	if err != nil {
		t.Fatalf("marshal credential request args: %v", err)
	}
	var args struct {
		AuthConfig map[string]any `json:"authConfig"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.AuthConfig == nil {
		t.Fatalf("decode credential request args %s: %v", raw, err)
	}
	return args.AuthConfig
}

// pausedState returns the OAuth state in a paused payload.
func pausedState(t *testing.T, payload map[string]any) string {
	t.Helper()
	ex, _ := payload["exchangedAuthCredential"].(map[string]any)
	o, _ := ex["oauth2"].(map[string]any)
	state, _ := o["state"].(string)
	if state == "" {
		t.Fatalf("paused payload has no state: %v", payload)
	}
	return state
}

// withADKWebResponse applies ADK Web's sendOAuthResponse to payload.
func withADKWebResponse(payload map[string]any, authResponseURI, redirectURI string) map[string]any {
	ex := payload["exchangedAuthCredential"].(map[string]any)
	o := ex["oauth2"].(map[string]any)
	o["authResponseUri"] = authResponseURI
	o["redirectUri"] = redirectURI
	return payload
}

// credentialAnswer is the user turn answering credential request fc.
func credentialAnswer(fc *genai.FunctionCall, response map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: fc.ID, Name: toolauth.RequestCredentialFunctionCallName, Response: response,
	}}}}
}

// confirmationResponse is the user turn answering confirmation request fc.
func confirmationResponse(fc *genai.FunctionCall, response map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: fc.ID, Name: "adk_request_confirmation", Response: response,
	}}}}
}

// signedIn answers the credential request in events with a redirect
// carrying code and the request's state, as ADK Web sends it.
func signedIn(t *testing.T, events []*session.Event, code string) *genai.Content {
	t.Helper()
	fc := credentialCall(t, events)
	payload := pausedPayload(t, fc)
	return credentialAnswer(fc, withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code="+code, "http://localhost:4200"))
}

// checkCodeExchange asserts an authorization_code grant for code and
// redirectURI from client cid-7 authenticated with HTTP Basic.
func checkCodeExchange(code, redirectURI string) func(*testing.T, *http.Request) {
	return func(t *testing.T, r *http.Request) {
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		if got := r.PostForm.Get("code"); got != code {
			t.Errorf("code = %q, want %q", got, code)
		}
		if got := r.PostForm.Get("redirect_uri"); got != redirectURI {
			t.Errorf("redirect_uri = %q, want %q", got, redirectURI)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "cid-7" || pass != "secret-7" {
			t.Errorf("Basic auth = %q, %q, %v; want cid-7, secret-7", user, pass, ok)
		}
	}
}

const exchangeReply = `{"access_token":"at-31","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-31"}`

// storedFor returns the token cfg's store holds for userID in app-resume.
func storedFor(t *testing.T, cfg Config, userID string) (*Token, bool) {
	t.Helper()
	key, err := cfg.CredentialKey("calendar_list", "app-resume", userID)
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	cred, ok, err := cfg.store().Get(t.Context(), key)
	if err != nil {
		t.Fatalf("Store.Get() error = %v", err)
	}
	tok, _ := cred.(*Token)
	return tok, ok
}

// ranWith asserts that calendar_list ran want times in total, every time
// with "Bearer at-31".
func ranWith(t *testing.T, rs *resourceServer, want int) {
	t.Helper()
	if got := rs.handlerRuns(); got != want {
		t.Errorf("calendar_list ran %d times, want %d", got, want)
	}
	for _, a := range rs.authorizations() {
		if a != "Bearer at-31" {
			t.Errorf("resource server saw %q, want %q", a, "Bearer at-31")
		}
	}
}

// keepsStateClean fails when any event writes the sign-in answer, an
// authorization code, a token or the client secret into state: ADK keeps
// the answer out of events, and the plugin stores tokens only in its store.
func keepsStateClean(t *testing.T, events []*session.Event) {
	t.Helper()
	for _, ev := range events {
		for k, v := range ev.Actions.StateDelta {
			raw, _ := json.Marshal(v)
			s := string(raw)
			if strings.HasPrefix(k, "temp:") || strings.Contains(s, "code-") || strings.Contains(s, "at-3") || strings.Contains(s, "rt-31") || strings.Contains(s, "secret-7") {
				t.Errorf("event by %s writes state %q = %s", ev.Author, k, raw)
			}
		}
	}
}

func TestResume_adkWebResponse_exchangesAndRuns(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("code-31", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	paused := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	events := h.run(t.Context(), "user-7", "s-1", signedIn(t, paused, "code-31"))

	ranWith(t, rs, 1)
	if resp, ok := functionResponse(events, "calendar_list"); !ok || resp["error"] != nil || resp["status"] == nil {
		t.Errorf("calendar_list response on the sign-in run = %v, want the tool's result", resp)
	}
	if tok, ok := storedFor(t, cfg, "user-7"); !ok || tok.AccessToken != "at-31" || tok.RefreshToken != "rt-31" {
		t.Errorf("stored token = %+v, %v; want at-31 / rt-31", tok, ok)
	}
	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	keepsStateClean(t, events)

	events = h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("and now?", genai.RoleUser))

	ranWith(t, rs, 2)
	if hasFunctionCall(events, toolauth.RequestCredentialFunctionCallName) {
		t.Error("third turn asked to sign in again, want the stored token")
	}
}

func TestResume_capturedADKWebShape_usesOwnClient(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("probe-code", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := credentialCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	state := pausedState(t, pausedPayload(t, fc))

	events := h.run(t.Context(), "user-7", "s-1", credentialAnswer(fc, capturedResponse(t, state)))

	ranWith(t, rs, 1)
	if te.callCount() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.callCount())
	}
	keepsStateClean(t, events)
}

// capturedResponse returns the captured ADK Web response with its state
// replaced by state.
func capturedResponse(t *testing.T, state string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/adkweb_auth_response.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	o := payload["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)
	old := pausedState(t, payload)
	o["authResponseUri"] = strings.Replace(o["authResponseUri"].(string), "state="+old, "state="+state, 1)
	return payload
}

// A bare credential, the shape agui answers with, completes the sign-in:
// ADK fills the rest in from its own request.
func TestResume_bareCredentialAnswer(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("code-32", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := credentialCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	state := pausedState(t, pausedPayload(t, fc))

	h.run(t.Context(), "user-7", "s-1", credentialAnswer(fc, map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": "http://localhost:4200/?state=" + state + "&code=code-32",
		"redirectUri":     "http://localhost:4200",
	}}))

	ranWith(t, rs, 1)
	if tok, ok := storedFor(t, cfg, "user-7"); !ok || tok.AccessToken != "at-31" {
		t.Errorf("stored token = %+v, %v; want at-31", tok, ok)
	}
}

func TestResume_nilStore_keepsTokenForLaterRuns(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(nil, te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	const user = "user-default-store-1"
	paused := h.run(t.Context(), user, "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	h.run(t.Context(), user, "s-1", signedIn(t, paused, "code-33"))

	events := h.run(t.Context(), user, "s-1", genai.NewContentFromText("and now?", genai.RoleUser))

	if hasFunctionCall(events, toolauth.RequestCredentialFunctionCallName) {
		t.Error("third turn asked to sign in again, want the default store's token")
	}
	ranWith(t, rs, 2)
}

// The sign-in is never taken as the tool's own approval: ADK's rerun of a
// tool that requires confirmation answers that it needs one, and the
// model's next call asks for it.
func TestResume_retryAsksToolsOwnApproval(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarToolWith(t, cfg, rs, true))
	paused := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	events := h.run(t.Context(), "user-7", "s-1", signedIn(t, paused, "code-34"))

	resp, ok := functionResponse(events, "calendar_list")
	if want := `error tool "calendar_list" requires confirmation, please approve or reject`; !ok || resp["error"] != want {
		t.Errorf("rerun response = %v, want error %q", resp, want)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Fatalf("calendar_list ran %d times on the sign-in rerun, want 0", got)
	}

	approval := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("and now?", genai.RoleUser)))
	raw, err := json.Marshal(approval.Args)
	if err != nil {
		t.Fatalf("marshal approval args: %v", err)
	}
	var args struct {
		OriginalFunctionCall struct {
			ID string `json:"id"`
		} `json:"originalFunctionCall"`
		ToolConfirmation struct {
			Hint    string          `json:"hint"`
			Payload json.RawMessage `json:"payload"`
		} `json:"toolConfirmation"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatalf("decode approval args %s: %v", raw, err)
	}
	if args.OriginalFunctionCall.ID != "call-2" {
		t.Errorf("approval is for %q, want call-2", args.OriginalFunctionCall.ID)
	}
	if p := string(args.ToolConfirmation.Payload); p != "null" && p != "" {
		t.Errorf("approval payload = %s, want null", p)
	}
	if !strings.HasPrefix(args.ToolConfirmation.Hint, "Please approve or reject the tool call calendar_list()") {
		t.Errorf("approval hint = %q, want the tool's own approval request", args.ToolConfirmation.Hint)
	}

	h.run(t.Context(), "user-7", "s-1", confirmationResponse(approval, map[string]any{"confirmed": true}))

	ranWith(t, rs, 1)
	if te.callCount() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.callCount())
	}
}

// An exchange without a refresh token is kept until the access token
// expires, not for RefreshTokenTTL.
func TestResume_tokenWithoutRefreshKeptUntilExpiry(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-35","token_type":"Bearer","expires_in":3600}`, nil)
	store := newRecordingStore()
	cfg := refreshConfig(store, te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-35")

	start := time.Now()
	h.run(t.Context(), "user-7", "s-1", answer)
	end := time.Now()

	expiresWithin(t, store.expiresAt(), start, end, time.Hour)
}

// An exchanged token with no refresh token and almost no life left is not
// stored: the store contract leaves declining a spent credential to the
// producer.
func TestResume_spentTokenIsNotStored(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, `{"access_token":"at-36","token_type":"Bearer","expires_in":5}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-36")

	h.run(t.Context(), "user-7", "s-1", answer)

	if tok, ok := storedFor(t, cfg, "user-7"); ok {
		t.Errorf("stored token = %+v, want none", tok)
	}
}

// invalid_scope means the user's grant no longer covers the tool's scopes:
// the entry is dropped and the user is asked to sign in again.
func TestRefresh_invalidScopeAsksToSignInAgain(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_scope"}`, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if tok, ok := storedToken(t, cfg); ok {
		t.Errorf("stored token = %+v, want the entry dropped", tok)
	}
	if !hasFunctionCall(events, toolauth.RequestCredentialFunctionCallName) {
		t.Error("user was not asked to sign in again")
	}
}

// A 400 with no error code (a bare page) is taken as a refused grant, so the
// user can sign in again rather than stay "unavailable" until the entry
// lapses; a client-setup code is not.
func TestRefresh_refusalsThatAskToSignInAgain(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		signInWant bool
	}{
		{"bare 400 page", http.StatusBadRequest, `<html>token revoked</html>`, true},
		{"401 without code is the client", http.StatusUnauthorized, `{}`, false},
		{"unauthorized_client", http.StatusBadRequest, `{"error":"unauthorized_client"}`, false},
		{"invalid_request", http.StatusBadRequest, `{"error":"invalid_request"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := newTokenEndpoint(t, tt.status, tt.body, nil)
			cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
			seed(t, cfg, "app-refresh", "user-7", &Token{AccessToken: "at-old-9", RefreshToken: "rt-old-9", Expiry: time.Now().Add(-time.Minute)}, time.Now().Add(24*time.Hour))
			rs := newResourceServer(t)
			h := newHarness(t, "app-refresh", "calendar_list", cfg, calendarTool(t, cfg, rs))

			events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

			_, kept := storedToken(t, cfg)
			if asked := hasFunctionCall(events, toolauth.RequestCredentialFunctionCallName); asked != tt.signInWant || kept == tt.signInWant {
				t.Errorf("asked to sign in = %v, entry kept = %v; want asked %v, kept %v", asked, kept, tt.signInWant, !tt.signInWant)
			}
		})
	}
}

// Two copies of one answer, as a double submit sends them, arrive while the
// first rerun is still in flight. ADK reruns the call for both; the code is
// exchanged once, the tool runs once, and the other rerun of the same call
// gets the out-of-date error.
func TestResume_overlappingAnswersExchangeOnce(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	held, release := make(chan struct{}), make(chan struct{})
	var holding atomic.Bool
	gate, err := plugin.New(plugin.Config{Name: "gate", BeforeToolCallback: func(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
		if v, err := ctx.State().Get("temp:" + cfg.Tools["calendar_list"].slot()); err == nil && v != nil && holding.CompareAndSwap(false, true) {
			close(held)
			<-release
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	h := newHarnessOn(t, session.InMemoryService(), "app-resume", "calendar_list", 1, cfg, []*plugin.Plugin{gate}, calendarTool(t, cfg, rs))
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-41")

	var first []*session.Event
	var firstErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range h.runner.Run(t.Context(), "user-7", "s-1", answer, agent.RunConfig{}) {
			if err != nil {
				firstErr = err
				return
			}
			first = append(first, ev)
		}
	}()
	<-held
	second := h.run(t.Context(), "user-7", "s-1", answer)
	close(release)
	<-done

	if firstErr != nil {
		t.Fatalf("held answer: %v", firstErr)
	}
	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	ranWith(t, rs, 1)
	if resp, ok := functionResponse(second, "calendar_list"); !ok || resp["error"] != nil || resp["status"] == nil {
		t.Errorf("second answer's rerun = %v, want the tool's result", resp)
	}
	resp, ok := functionResponse(first, "calendar_list")
	if want := "Google Calendar sign-in failed: the sign-in request is out of date"; !ok || resp["error"] != want {
		t.Errorf("held answer's rerun = %v, want error %q", resp, want)
	}
	keepsStateClean(t, append(first, second...))
}

// twoCallsHarness returns a harness whose model calls calendar_list twice a
// turn, and a token endpoint, answering status and body, that holds its
// reply until the rerun of call-2 has reached the plugins, so the two reruns
// of one sign-in overlap whichever of them exchanges the code.
func twoCallsHarness(t *testing.T, status int, body string) (*harness, Config, *resourceServer, *tokenEndpoint) {
	t.Helper()
	secondArrived := make(chan struct{})
	var once sync.Once
	te := newTokenEndpoint(t, status, body, func(*testing.T, *http.Request) {
		select {
		case <-secondArrived:
		case <-time.After(5 * time.Second):
		}
	})
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	slot := cfg.Tools["calendar_list"].slot()
	gate, err := plugin.New(plugin.Config{Name: "gate", BeforeToolCallback: func(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
		// Only the rerun carries the answer in temp: state; the pausing turn
		// must not open the gate.
		if v, err := ctx.State().Get("temp:" + slot); err == nil && v != nil && ctx.FunctionCallID() == "call-2" {
			once.Do(func() { close(secondArrived) })
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	return newHarnessOn(t, session.InMemoryService(), "app-resume", "calendar_list", 2, cfg, []*plugin.Plugin{gate}, calendarTool(t, cfg, rs)), cfg, rs, te
}

// ADK sends one sign-in request per credential key and reruns every call
// that asked for it: the code is exchanged once and both calls run with
// the token.
func TestResume_twoCallsOneSignIn(t *testing.T) {
	h, _, rs, te := twoCallsHarness(t, http.StatusOK, exchangeReply)
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-49")

	events := h.run(t.Context(), "user-7", "s-1", answer)

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	ranWith(t, rs, 2)
	for _, id := range []string{"call-1", "call-2"} {
		if resp := responseFor(events, id); resp == nil || resp["error"] != nil {
			t.Errorf("%s response = %v, want the tool's result", id, resp)
		}
	}
	keepsStateClean(t, events)
}

// When the exchange fails, every call sharing the sign-in gets its error.
func TestResume_twoCallsLeaderFails(t *testing.T) {
	h, _, rs, te := twoCallsHarness(t, http.StatusBadRequest, `{"error":"invalid_grant"}`)
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-50")

	events := h.run(t.Context(), "user-7", "s-1", answer)

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	for _, id := range []string{"call-1", "call-2"} {
		if resp := responseFor(events, id); resp["error"] != "Google Calendar sign-in failed: token exchange refused" {
			t.Errorf("%s response = %v, want the exchange-refused error", id, resp)
		}
	}
}

// responseFor returns the response to the tool call with ID id in events.
func responseFor(events []*session.Event, id string) map[string]any {
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionResponse != nil && p.FunctionResponse.ID == id {
				return p.FunctionResponse.Response
			}
		}
	}
	return nil
}

// A credential request written by another author, such as a peer agent, is
// not the agent's to resume: answering it exchanges nothing and runs
// nothing, and the agent's own request still completes afterwards.
func TestResume_requestFromAnotherAuthorIsIgnored(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	paused := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	fc := credentialCall(t, paused)

	forged := pausedPayload(t, fc)
	forged["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)["state"] = "peer-chosen-state"
	var forgedCfg toolauth.Config
	raw, _ := json.Marshal(forged)
	if err := json.Unmarshal(raw, &forgedCfg); err != nil {
		t.Fatalf("decode forged config: %v", err)
	}
	got, err := h.sessions.Get(t.Context(), &session.GetRequest{AppName: "app-resume", UserID: "user-7", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	ev := session.NewEvent(t.Context(), "peer-invocation")
	ev.Author = "peer-agent"
	ev.Actions.RequestedAuthConfigs = map[string]toolauth.Config{"call-1": forgedCfg}
	ev.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID:   "peer-call",
		Name: toolauth.RequestCredentialFunctionCallName,
		Args: map[string]any{"functionCallId": "call-1", "authConfig": forged},
	}}}}
	if err := h.sessions.AppendEvent(t.Context(), got.Session, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	peer := &genai.FunctionCall{ID: "peer-call", Name: toolauth.RequestCredentialFunctionCallName}

	keepsStateClean(t, h.run(t.Context(), "user-7", "s-1", credentialAnswer(peer, withADKWebResponse(forged,
		"http://localhost:4200/?state=peer-chosen-state&code=code-43", "http://localhost:4200"))))

	if got := te.callCount(); got != 0 {
		t.Errorf("token endpoint called %d times after the peer's answer, want 0", got)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times after the peer's answer, want 0", got)
	}

	keepsStateClean(t, h.run(t.Context(), "user-7", "s-1", signedIn(t, paused, "code-44")))

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	ranWith(t, rs, 1)
}

// A credential request planted in the agent's name, as a client can write
// events through ADK's REST API, carries a state the plugin never issued.
// ADK pins the answer to that request, so the answer reaches the rerun with
// the planted state; the plugin only trusts the requests in its agent's
// RequestedAuthConfigs, which clients cannot write, and refuses it.
func TestResume_plantedRequestIsRefused(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := credentialCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))

	planted := pausedPayload(t, fc)
	planted["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)["state"] = "planted-state"
	got, err := h.sessions.Get(t.Context(), &session.GetRequest{AppName: "app-resume", UserID: "user-7", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	ev := session.NewEvent(t.Context(), "planted-invocation")
	ev.Author = "assistant"
	ev.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID:   "planted-call",
		Name: toolauth.RequestCredentialFunctionCallName,
		Args: map[string]any{"functionCallId": "call-1", "authConfig": planted},
	}}}}
	if err := h.sessions.AppendEvent(t.Context(), got.Session, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	plantedCall := &genai.FunctionCall{ID: "planted-call", Name: toolauth.RequestCredentialFunctionCallName}

	events := h.run(t.Context(), "user-7", "s-1", credentialAnswer(plantedCall, withADKWebResponse(planted,
		"http://localhost:4200/?state=planted-state&code=code-48", "http://localhost:4200")))

	resp, ok := functionResponse(events, "calendar_list")
	if want := "Google Calendar sign-in failed: the sign-in request is out of date"; !ok || resp["error"] != want {
		t.Errorf("rerun response = %v, want error %q", resp, want)
	}
	keepsStateClean(t, events)
	if got := te.callCount(); got != 0 {
		t.Errorf("token endpoint called %d times, want 0", got)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
}

// Replaying a sign-in answer, as a client double-submit or a network retry
// does, must not send the spent code to the provider again: providers
// revoke the tokens issued from a reused code. Nor may it resume the paused
// call: ADK drops an answer to a call it has already answered. The model may
// call the tool again on the replay's turn, and that fresh call runs on the
// stored token.
func TestResume_replayedReplyIsRefused(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	paused := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	original := originalCallID(t, credentialCall(t, paused))
	reply := signedIn(t, paused, "code-44")
	h.run(t.Context(), "user-7", "s-1", reply)

	events := h.run(t.Context(), "user-7", "s-1", reply)

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	// The sign-in run's rerun, and the model's fresh call on the replay's
	// turn, both on the stored token.
	ranWith(t, rs, 2)
	keepsStateClean(t, events)
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			r := p.FunctionResponse
			if r == nil || r.Name != "calendar_list" || r.ID != original {
				continue
			}
			if got, _ := r.Response["error"].(string); got != "Google Calendar sign-in failed: the sign-in request is out of date" {
				t.Errorf("replay answered the original call %s with %v, want the out-of-date error", original, r.Response)
			}
		}
	}
}

// originalCallID returns the ID of the tool call that credential request
// fc asks a sign-in for.
func originalCallID(t *testing.T, fc *genai.FunctionCall) string {
	t.Helper()
	raw, err := json.Marshal(fc.Args)
	if err != nil {
		t.Fatalf("marshal credential request args: %v", err)
	}
	var args toolauth.ToolArguments
	if err := json.Unmarshal(raw, &args); err != nil || args.FunctionCallID == "" {
		t.Fatalf("credential request args %s name no call: %v", raw, err)
	}
	return args.FunctionCallID
}

// A sign-in answer sent as a confirmation, as clients of the plugin's
// earlier versions sent it, never completes a sign-in and never passes as
// the tool's own approval.
func TestResume_oldConfirmationRailIsRefused(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	seed(t, cfg, "app-resume", "user-7", &Token{AccessToken: "at-31", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarToolWith(t, cfg, rs, true))
	approval := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))

	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(approval, map[string]any{"confirmed": true, "payload": map[string]any{
		"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
			"state": "s-old", "authResponseUri": "http://localhost:4200/?state=s-old&code=code-46",
		}},
	}}))

	resp, ok := functionResponse(events, "calendar_list")
	if want := "Google Calendar sign-in failed: the sign-in request is out of date"; !ok || resp["error"] != want {
		t.Errorf("response = %v, want error %q", resp, want)
	}
	if got := te.callCount(); got != 0 {
		t.Errorf("token endpoint called %d times, want 0", got)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	keepsStateClean(t, events)
}

// A run that another plugin ends in its BeforeRun never reaches tooloauth's
// hooks. The request it answered stays open: answering again completes the
// sign-in once.
func TestResume_runEndedEarlyByAnotherPluginStillCompletes(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	var endEarly atomic.Bool
	guard, err := plugin.New(plugin.Config{
		Name: "guard",
		BeforeRunCallback: func(agent.InvocationContext) (*genai.Content, error) {
			if endEarly.Load() {
				return genai.NewContentFromText("quota exceeded", genai.RoleModel), nil
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	h := newHarnessOn(t, session.InMemoryService(), "app-resume", "calendar_list", 1, cfg, []*plugin.Plugin{guard}, calendarTool(t, cfg, rs))
	answer := signedIn(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)), "code-47")
	endEarly.Store(true)
	h.run(t.Context(), "user-7", "s-1", answer)
	endEarly.Store(false)

	keepsStateClean(t, h.run(t.Context(), "user-7", "s-1", answer))

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	ranWith(t, rs, 1)
}
