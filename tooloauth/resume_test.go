package tooloauth

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// confirmationCall returns the only adk_request_confirmation call in events.
func confirmationCall(t *testing.T, events []*session.Event) *genai.FunctionCall {
	t.Helper()
	var calls []*genai.FunctionCall
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionCall != nil && p.FunctionCall.Name == "adk_request_confirmation" {
				calls = append(calls, p.FunctionCall)
			}
		}
	}
	if len(calls) != 1 {
		t.Fatalf("confirmation requests = %d, want 1", len(calls))
	}
	return calls[0]
}

// pausedPayload returns the request's toolConfirmation payload as a JSON
// map, the form a client echoes back.
func pausedPayload(t *testing.T, fc *genai.FunctionCall) map[string]any {
	t.Helper()
	raw, err := json.Marshal(fc.Args["toolConfirmation"])
	if err != nil {
		t.Fatalf("marshal toolConfirmation: %v", err)
	}
	var tc struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		t.Fatalf("decode toolConfirmation %s: %v", raw, err)
	}
	return tc.Payload
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

// confirmationResponse is the user turn answering confirmation request fc.
func confirmationResponse(fc *genai.FunctionCall, response map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: fc.ID, Name: "adk_request_confirmation", Response: response,
	}}}}
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

// connected asserts that the resumed call answered with the sign-in result.
func connected(t *testing.T, events []*session.Event) {
	t.Helper()
	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response on resume")
	}
	if got, _ := resp["result"].(string); got != "connected to Google Calendar, call the tool again" {
		t.Errorf("resume response = %v, want result %q", resp, "connected to Google Calendar, call the tool again")
	}
}

func TestResume_adkWebResponse_exchangesAndStoresWithoutRunning(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("code-31", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))

	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	payload = withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-31", "http://localhost:4200")
	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))

	connected(t, events)
	if tok, ok := storedFor(t, cfg, "user-7"); !ok || tok.AccessToken != "at-31" || tok.RefreshToken != "rt-31" {
		t.Errorf("stored token = %+v, %v; want at-31 / rt-31", tok, ok)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times on the sign-in resume, want 0", got)
	}

	events = h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("and now?", genai.RoleUser))

	if got := rs.handlerRuns(); got != 1 {
		t.Errorf("calendar_list ran %d times after sign-in, want 1", got)
	}
	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-31" {
		t.Errorf("resource server saw %q, want one %q", got, "Bearer at-31")
	}
	if hasFunctionCall(events, "adk_request_confirmation") {
		t.Error("third turn paused again, want none")
	}
}

func TestResume_capturedADKWebShape_usesOwnClient(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("probe-code", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	state := pausedState(t, pausedPayload(t, fc))

	payload := capturedResponse(t, state, false)
	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))

	connected(t, events)
	if te.callCount() != 1 {
		t.Errorf("token endpoint called %d times, want 1", te.callCount())
	}
}

// capturedResponse returns the captured ADK Web response with its state
// replaced by state, and without exchangedAuthCredential.authType when
// dropAuthType is set.
func capturedResponse(t *testing.T, state string, dropAuthType bool) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/adkweb_auth_response.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	ex := payload["exchangedAuthCredential"].(map[string]any)
	o := ex["oauth2"].(map[string]any)
	old := pausedState(t, payload)
	o["authResponseUri"] = strings.Replace(o["authResponseUri"].(string), "state="+old, "state="+state, 1)
	if dropAuthType {
		delete(ex, "authType")
	}
	return payload
}

func TestResume_payloadWithoutAuthType(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("probe-code", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	state := pausedState(t, pausedPayload(t, fc))

	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": capturedResponse(t, state, true)}))

	connected(t, events)
}

func TestResume_payloadWithOnlyExchangedCredential(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, checkCodeExchange("code-32", "http://localhost:4200"))
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	state := pausedState(t, pausedPayload(t, fc))

	payload := map[string]any{"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": "http://localhost:4200/?state=" + state + "&code=code-32",
		"redirectUri":     "http://localhost:4200",
	}}}
	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))

	connected(t, events)
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
	fc := confirmationCall(t, h.run(t.Context(), user, "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	payload = withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-33", "http://localhost:4200")
	h.run(t.Context(), user, "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))

	events := h.run(t.Context(), user, "s-1", genai.NewContentFromText("and now?", genai.RoleUser))

	if hasFunctionCall(events, "adk_request_confirmation") {
		t.Error("third turn paused again, want the default store's token")
	}
	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-31" {
		t.Errorf("resource server saw %q, want one %q", got, "Bearer at-31")
	}
}

// Spec Decision "Approval gap": the sign-in's confirmation is never taken
// as the tool's own approval; the retried call asks for it.
func TestResume_retryAsksToolsOwnApproval(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarToolWith(t, cfg, rs, true))

	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	payload = withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-34", "http://localhost:4200")
	connected(t, h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload})))
	if got := rs.handlerRuns(); got != 0 {
		t.Fatalf("calendar_list ran %d times on the sign-in resume, want 0", got)
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
	if got := rs.handlerRuns(); got != 0 {
		t.Fatalf("calendar_list ran %d times before approval, want 0", got)
	}

	h.run(t.Context(), "user-7", "s-1", confirmationResponse(approval, map[string]any{"confirmed": true}))

	if got := rs.handlerRuns(); got != 1 {
		t.Errorf("calendar_list ran %d times after approval, want 1", got)
	}
	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-31" {
		t.Errorf("resource server saw %q, want one %q", got, "Bearer at-31")
	}
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
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	payload = withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-35", "http://localhost:4200")

	start := time.Now()
	h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))
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
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	payload = withADKWebResponse(payload, "http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-36", "http://localhost:4200")

	h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": payload}))

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
	if !hasFunctionCall(events, "adk_request_confirmation") {
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
			if asked := hasFunctionCall(events, "adk_request_confirmation"); asked != tt.signInWant || kept == tt.signInWant {
				t.Errorf("asked to sign in = %v, entry kept = %v; want asked %v, kept %v", asked, kept, tt.signInWant, !tt.signInWant)
			}
		})
	}
}

// Two resumes of one paused turn run under the same invocation ID. When
// they overlap, the first to finish must not leave the second without its
// sign-in request, or the second's sign-in confirmation would pass as the
// tool's own approval.
func TestResume_overlappingResumesNeverRunTheTool(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	held, release := make(chan struct{}), make(chan struct{})
	gate, err := plugin.New(plugin.Config{Name: "gate", BeforeToolCallback: func(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
		if ctx.FunctionCallID() == "call-2" && ctx.ToolConfirmation() != nil {
			close(held)
			<-release
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	h := newHarnessOn(t, session.InMemoryService(), "app-resume", "calendar_list", 2, cfg, []*plugin.Plugin{gate}, calendarToolWith(t, cfg, rs, true))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	calls := map[string]*genai.FunctionCall{}
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionCall != nil && p.FunctionCall.Name == "adk_request_confirmation" {
				orig, err := toolconfirmation.OriginalCallFrom(p.FunctionCall)
				if err != nil {
					t.Fatalf("OriginalCallFrom: %v", err)
				}
				calls[orig.ID] = p.FunctionCall
			}
		}
	}
	if len(calls) != 2 {
		t.Fatalf("sign-in requests = %d, want 2", len(calls))
	}
	respond := func(fc *genai.FunctionCall, code string) *genai.Content {
		payload := pausedPayload(t, fc)
		return confirmationResponse(fc, map[string]any{"confirmed": true, "payload": withADKWebResponse(payload,
			"http://localhost:4200/?state="+pausedState(t, payload)+"&code="+code, "http://localhost:4200")})
	}

	// Resume call-2 first and hold it inside the run; resume call-1 to the
	// end meanwhile; then let call-2 go on.
	var second []*session.Event
	var secondErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range h.runner.Run(t.Context(), "user-7", "s-1", respond(calls["call-2"], "code-42"), agent.RunConfig{}) {
			if err != nil {
				secondErr = err
				return
			}
			second = append(second, ev)
		}
	}()
	<-held
	h.run(t.Context(), "user-7", "s-1", respond(calls["call-1"], "code-41"))
	close(release)
	<-done

	if secondErr != nil {
		t.Fatalf("second resume: %v", secondErr)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times on sign-in resumes, want 0", got)
	}
	resp, ok := functionResponse(second, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response on the second resume")
	}
	if got, _ := resp["result"].(string); got != "connected to Google Calendar, call the tool again" {
		t.Errorf("second resume response = %v, want the connected result", resp)
	}
}

// A sign-in request for the same call written by another author, such as a
// peer agent, after the plugin's own neither replaces nor clears it.
func TestResume_requestFromAnotherAuthorIsIgnored(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	state := pausedState(t, payload)

	forged := pausedPayload(t, fc)
	forged["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)["state"] = "peer-chosen-state"
	got, err := h.sessions.Get(t.Context(), &session.GetRequest{AppName: "app-resume", UserID: "user-7", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	ev := session.NewEvent(t.Context(), "peer-invocation")
	ev.Author = "peer-agent"
	ev.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID:   "peer-call",
		Name: "adk_request_confirmation",
		Args: map[string]any{
			"originalFunctionCall": map[string]any{"id": "call-1", "name": "calendar_list"},
			"toolConfirmation":     map[string]any{"hint": "x", "confirmed": false, "payload": forged},
		},
	}}}}
	if err := h.sessions.AppendEvent(t.Context(), got.Session, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": withADKWebResponse(payload,
		"http://localhost:4200/?state="+state+"&code=code-43", "http://localhost:4200")}))

	connected(t, events)
}

// Replaying a sign-in reply, as a client double-submit or a network retry
// does, must not send the spent code to the provider again: providers
// revoke the tokens issued from a reused code.
func TestResume_replayedReplyIsRefused(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	reply := confirmationResponse(fc, map[string]any{"confirmed": true, "payload": withADKWebResponse(payload,
		"http://localhost:4200/?state="+pausedState(t, payload)+"&code=code-44", "http://localhost:4200")})
	connected(t, h.run(t.Context(), "user-7", "s-1", reply))

	events := h.run(t.Context(), "user-7", "s-1", reply)

	if got := te.callCount(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response on the replay")
	}
	if got, _ := resp["error"].(string); got != "Google Calendar sign-in failed: the sign-in request is out of date" {
		t.Errorf("replay response = %v, want the out-of-date error", resp)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
}

// A run that another plugin ends in its BeforeRun never reaches tooloauth's
// hooks. It must not cost a resume in flight its record: a bare
// {"confirmed": true} answering a sign-in would otherwise fall through to a
// token stored meanwhile and pass as the tool's own approval.
func TestResume_runEndedEarlyByAnotherPluginKeepsTheRecord(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	store := newRecordingStore()
	cfg := refreshConfig(store, te, 72*time.Hour)
	rs := newResourceServer(t)
	held, release := make(chan struct{}), make(chan struct{})
	var endEarly atomic.Bool
	guard, err := plugin.New(plugin.Config{
		Name: "guard",
		BeforeRunCallback: func(agent.InvocationContext) (*genai.Content, error) {
			if endEarly.Load() {
				return genai.NewContentFromText("quota exceeded", genai.RoleModel), nil
			}
			return nil, nil
		},
		BeforeToolCallback: func(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
			if ctx.FunctionCallID() == "call-2" && ctx.ToolConfirmation() != nil {
				close(held)
				<-release
			}
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	h := newHarnessOn(t, session.InMemoryService(), "app-resume", "calendar_list", 2, cfg, []*plugin.Plugin{guard}, calendarToolWith(t, cfg, rs, true))
	calls := pausedCalls(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))

	done := make(chan []*session.Event)
	go func() {
		var evs []*session.Event
		for ev, err := range h.runner.Run(t.Context(), "user-7", "s-1", confirmationResponse(calls["call-2"], map[string]any{"confirmed": true}), agent.RunConfig{}) {
			if err == nil {
				evs = append(evs, ev)
			}
		}
		done <- evs
	}()
	<-held
	seed(t, cfg, "app-resume", "user-7", &Token{AccessToken: "at-other-tab", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	endEarly.Store(true)
	h.run(t.Context(), "user-7", "s-1", confirmationResponse(calls["call-1"], map[string]any{"confirmed": true}))
	close(release)
	second := <-done

	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	resp, ok := functionResponse(second, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response on the held resume")
	}
	if got, _ := resp["error"].(string); got != "Google Calendar sign-in failed: no auth response" {
		t.Errorf("held resume response = %v, want the no-auth-response error", resp)
	}
}

// pausedCalls returns the sign-in requests in events by the call each paused.
func pausedCalls(t *testing.T, events []*session.Event) map[string]*genai.FunctionCall {
	t.Helper()
	calls := map[string]*genai.FunctionCall{}
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionCall != nil && p.FunctionCall.Name == "adk_request_confirmation" {
				orig, err := toolconfirmation.OriginalCallFrom(p.FunctionCall)
				if err != nil {
					t.Fatalf("OriginalCallFrom: %v", err)
				}
				calls[orig.ID] = p.FunctionCall
			}
		}
	}
	return calls
}

// Two sign-in requests from the agent for one call that disagree on the
// state only come from a tampered session: the sign-in is refused.
func TestResume_conflictingRequestsAreRefused(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	state := pausedState(t, payload)

	second := pausedPayload(t, fc)
	second["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)["state"] = "other-state"
	got, err := h.sessions.Get(t.Context(), &session.GetRequest{AppName: "app-resume", UserID: "user-7", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("Get session: %v", err)
	}
	ev := session.NewEvent(t.Context(), "tampered")
	ev.Author = "assistant"
	ev.Content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID: "tampered-call", Name: "adk_request_confirmation",
		Args: map[string]any{
			"originalFunctionCall": map[string]any{"id": "call-1", "name": "calendar_list"},
			"toolConfirmation":     map[string]any{"hint": "x", "confirmed": false, "payload": second},
		},
	}}}}
	if err := h.sessions.AppendEvent(t.Context(), got.Session, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	events := h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, map[string]any{"confirmed": true, "payload": withADKWebResponse(payload,
		"http://localhost:4200/?state="+state+"&code=code-45", "http://localhost:4200")}))

	resp, _ := functionResponse(events, "calendar_list")
	if got, _ := resp["error"].(string); got != "Google Calendar sign-in failed: the sign-in request is out of date" {
		t.Errorf("response = %v, want the out-of-date error", resp)
	}
	if te.callCount() != 0 {
		t.Errorf("token endpoint called %d times, want 0", te.callCount())
	}
}
