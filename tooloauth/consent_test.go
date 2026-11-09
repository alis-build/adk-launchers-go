package tooloauth

import (
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// authRequestCall is one adk_request_confirmation call, decoded.
type authRequestCall struct {
	OriginalFunctionCall struct {
		ID string `json:"id"`
	} `json:"originalFunctionCall"`
	ToolConfirmation struct {
		Hint    string          `json:"hint"`
		Payload json.RawMessage `json:"payload"`
	} `json:"toolConfirmation"`
}

// authRequests returns every adk_request_confirmation call in events,
// decoded by a JSON round trip of its arguments, as a client would see them.
func authRequests(t *testing.T, events []*session.Event) []authRequestCall {
	t.Helper()
	var out []authRequestCall
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p == nil || p.FunctionCall == nil || p.FunctionCall.Name != "adk_request_confirmation" {
				continue
			}
			raw, err := json.Marshal(p.FunctionCall.Args)
			if err != nil {
				t.Fatalf("marshal confirmation args: %v", err)
			}
			var c authRequestCall
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatalf("decode confirmation args %s: %v", raw, err)
			}
			out = append(out, c)
		}
	}
	return out
}

// payloadConfig decodes a confirmation payload as a toolauth.Config.
func payloadConfig(t *testing.T, c authRequestCall) toolauth.Config {
	t.Helper()
	var cfg toolauth.Config
	if err := json.Unmarshal(c.ToolConfirmation.Payload, &cfg); err != nil {
		t.Fatalf("decode payload %s: %v", c.ToolConfirmation.Payload, err)
	}
	return cfg
}

// consentConfig is calendarConfig asking for the scope the adk-python
// fixture was captured with.
func consentConfig(store auth.CredentialStore) Config {
	cfg := calendarConfig(store)
	tl := cfg.Tools["calendar_list"]
	tl.Scopes = []string{"https://www.example.com/auth/calendar.readonly"}
	cfg.Tools = map[string]Tool{"calendar_list": tl}
	return cfg
}

func TestConsent_noToken_pausesWithAuthRequest(t *testing.T) {
	cfg := consentConfig(auth.NewInMemoryCredentialStore())
	rs := newResourceServer(t)
	h := newHarness(t, "app-consent", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list handler ran %d times, want 0", got)
	}
	reqs := authRequests(t, events)
	if len(reqs) != 1 {
		t.Fatalf("auth requests = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.OriginalFunctionCall.ID != "call-1" {
		t.Errorf("original call ID = %q, want call-1", req.OriginalFunctionCall.ID)
	}
	if req.ToolConfirmation.Hint != "Sign in to Google Calendar to continue." {
		t.Errorf("hint = %q, want %q", req.ToolConfirmation.Hint, "Sign in to Google Calendar to continue.")
	}
	payload := payloadConfig(t, req)
	if payload.ExchangedAuthCredential == nil || payload.ExchangedAuthCredential.OAuth2 == nil {
		t.Fatalf("payload has no exchanged OAuth2 credential: %s", req.ToolConfirmation.Payload)
	}
	o := payload.ExchangedAuthCredential.OAuth2
	u, err := url.Parse(o.AuthURI)
	if err != nil {
		t.Fatalf("parse auth URI %q: %v", o.AuthURI, err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != "https://accounts.example.com/o/oauth2/auth" {
		t.Errorf("auth URI endpoint = %q, want https://accounts.example.com/o/oauth2/auth", got)
	}
	wantQuery := url.Values{
		"response_type": {"code"},
		"client_id":     {"cid-7"},
		"redirect_uri":  {"https://agent.example.com/oauth/callback"},
		"scope":         {"https://www.example.com/auth/calendar.readonly"},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {o.State},
	}
	if diff := cmp.Diff(wantQuery, u.Query()); diff != "" {
		t.Errorf("auth URI query (-want +got):\n%s", diff)
	}
	if len(o.State) != 43 {
		t.Errorf("state %q has %d characters, want 43", o.State, len(o.State))
	}
	key, err := cfg.CredentialKey("calendar_list", "app-consent", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if payload.CredentialKey != key.Key {
		t.Errorf("payload credentialKey = %q, want %q", payload.CredentialKey, key.Key)
	}
	for _, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if strings.Contains(string(raw), "secret-7") {
			t.Errorf("event carries the client secret: %s", raw)
		}
	}
}

// jsonKeys returns the sorted keys of the JSON object at path in v.
func jsonKeys(t *testing.T, v map[string]any, path ...string) []string {
	t.Helper()
	cur := v
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("no object at %v", path)
		}
		cur = next
	}
	return slices.Sorted(func(yield func(string) bool) {
		for k := range cur {
			if !yield(k) {
				return
			}
		}
	})
}

// Our auth request has the keys adk-python's has, at the levels a client
// reads, so an adk-python client can complete it.
func TestConsent_payloadHasADKPythonShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/adkpython_auth_request.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	cfg := consentConfig(auth.NewInMemoryCredentialStore())
	rs := newResourceServer(t)
	events := newHarness(t, "app-consent", "calendar_list", cfg, calendarTool(t, cfg, rs)).
		run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	reqs := authRequests(t, events)
	if len(reqs) != 1 {
		t.Fatalf("auth requests = %d, want 1", len(reqs))
	}
	var got map[string]any
	if err := json.Unmarshal(reqs[0].ToolConfirmation.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, path := range [][]string{
		nil,
		{"authScheme"},
		{"authScheme", "flows"},
		{"authScheme", "flows", "authorizationCode"},
		{"exchangedAuthCredential"},
		{"rawAuthCredential"},
		{"exchangedAuthCredential", "oauth2"},
		{"rawAuthCredential", "oauth2"},
	} {
		if diff := cmp.Diff(jsonKeys(t, want, path...), jsonKeys(t, got, path...)); diff != "" {
			t.Errorf("keys at %v (-adk-python +ours):\n%s", path, diff)
		}
	}
}

// AC6: a token stored for one user is not served to another.
func TestConsent_otherUserIsNotServed(t *testing.T) {
	cfg := consentConfig(auth.NewInMemoryCredentialStore())
	seed(t, cfg, "app-consent", "user-7", &Token{AccessToken: "at-user7", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-consent", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(t.Context(), "user-8", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if len(authRequests(t, events)) != 1 {
		t.Error("user-8 was not asked to sign in")
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times for user-8, want 0", got)
	}
}

// countingTool returns a tool named name that counts its runs into *ran.
func countingTool(t *testing.T, name string, ran *int) tool.Tool {
	t.Helper()
	tl, err := functiontool.New(functiontool.Config{Name: name, Description: "Counts."},
		func(agent.Context, struct{}) (map[string]any, error) {
			*ran++
			return map[string]any{}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	return tl
}

// AC6: a token granted for narrower scopes is not served to a tool asking
// for others, even with the same client.
func TestConsent_otherScopesAreNotServed(t *testing.T) {
	base := consentConfig(auth.NewInMemoryCredentialStore()).Tools["calendar_list"]
	read, write := base, base
	read.Scopes = []string{"https://www.example.com/auth/calendar.readonly"}
	write.Scopes = []string{"https://www.example.com/auth/calendar"}
	cfg := Config{Store: auth.NewInMemoryCredentialStore(), Tools: map[string]Tool{"cal_read": read, "cal_write": write}}
	key, err := cfg.CredentialKey("cal_read", "app-consent", "user-7")
	if err != nil {
		t.Fatalf("CredentialKey() error = %v", err)
	}
	if err := cfg.Store.Set(t.Context(), key, &Token{AccessToken: "at-read", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Store.Set() error = %v", err)
	}
	ran := 0
	h := newHarness(t, "app-consent", "cal_write", cfg, countingTool(t, "cal_write", &ran))

	events := h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("add an event", genai.RoleUser))

	if len(authRequests(t, events)) != 1 {
		t.Error("cal_write was served cal_read's token instead of asking to sign in")
	}
	if ran != 0 {
		t.Errorf("cal_write ran %d times, want 0", ran)
	}
}
