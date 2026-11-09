package agui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// signInConfig is an auth config as a tool would pause with it, client
// secret included, to prove the interrupt strips it.
func signInConfig() *toolauth.Config {
	return &toolauth.Config{
		AuthScheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
			AuthorizationURL: "https://accounts.example.com/o/oauth2/auth",
			TokenURL:         "https://oauth2.example.com/token",
			Scopes:           map[string]string{"calendar.read": ""},
		}}},
		RawAuthCredential: &toolauth.Credential{AuthType: toolauth.CredentialTypeOAuth2, OAuth2: &toolauth.OAuth2Auth{
			ClientID: "cid-7", ClientSecret: "secret-7",
		}},
		ExchangedAuthCredential: &toolauth.Credential{AuthType: toolauth.CredentialTypeOAuth2, OAuth2: &toolauth.OAuth2Auth{
			ClientID: "cid-7", ClientSecret: "secret-7",
			AuthURI: "https://accounts.example.com/o/oauth2/auth?state=s-1",
			State:   "s-1",
		}},
	}
}

// confirmationInterrupt runs one adk_request_confirmation event carrying
// payload through processEvent and returns the RUN_FINISHED interrupt and
// the SSE body.
func confirmationInterrupt(t *testing.T, payload any) (map[string]any, string) {
	t.Helper()
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "e-auth"
	ev.Content = &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID:   "confirm-cal-1",
		Name: toolconfirmation.FunctionCallName,
		Args: map[string]any{
			"toolConfirmation":     map[string]any{"hint": "Sign in to Google Calendar to continue.", "payload": payload},
			"originalFunctionCall": map[string]any{"ID": "call-cal-1", "Name": "calendar_list"},
		},
	}}}}
	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	body := rec.Body.String()
	for _, sev := range parseSSEEvents(body) {
		if sev.Raw["type"] != "RUN_FINISHED" {
			continue
		}
		outcome, _ := sev.Raw["outcome"].(map[string]any)
		intrs, _ := outcome["interrupts"].([]any)
		if len(intrs) != 1 {
			t.Fatalf("interrupts = %v, want one", intrs)
		}
		return intrs[0].(map[string]any), body
	}
	t.Fatalf("no RUN_FINISHED in %s", body)
	return nil, ""
}

// at returns the value at path in v, a decoded JSON object.
func at(v map[string]any, path ...string) any {
	var cur any = v
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func TestAuthInterrupt_emitsAuthRequired(t *testing.T) {
	intr, body := confirmationInterrupt(t, signInConfig())

	if got := intr["reason"]; got != "auth_required" {
		t.Errorf("reason = %v, want auth_required", got)
	}
	if got := intr["toolCallId"]; got != "call-cal-1" {
		t.Errorf("toolCallId = %v, want call-cal-1", got)
	}
	if got := intr["id"]; got != "confirm-cal-1" {
		t.Errorf("id = %v, want confirm-cal-1", got)
	}
	if got := at(intr, "metadata", "adk", "authConfig", "exchangedAuthCredential", "oauth2", "authUri"); got != "https://accounts.example.com/o/oauth2/auth?state=s-1" {
		t.Errorf("authConfig authUri = %v", got)
	}
	if got := at(intr, "metadata", "adk", "confirmationPayload"); got != nil {
		t.Errorf("confirmationPayload = %v, want absent", got)
	}
	if diff := cmp.Diff([]any{"exchangedAuthCredential"}, at(intr, "responseSchema", "required")); diff != "" {
		t.Errorf("responseSchema.required (-want +got):\n%s", diff)
	}
	if strings.Contains(body, "secret-7") {
		t.Error("SSE body carries the client secret")
	}
}

// A stored session hands the payload back as a JSON map.
func TestAuthInterrupt_mapPayloadFromStoredSession(t *testing.T) {
	raw, err := json.Marshal(signInConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	intr, body := confirmationInterrupt(t, payload)

	if got := intr["reason"]; got != "auth_required" {
		t.Errorf("reason = %v, want auth_required", got)
	}
	if got := at(intr, "metadata", "adk", "authConfig", "exchangedAuthCredential", "oauth2", "authUri"); got != "https://accounts.example.com/o/oauth2/auth?state=s-1" {
		t.Errorf("authConfig authUri = %v", got)
	}
	if strings.Contains(body, "secret-7") {
		t.Error("SSE body carries the client secret")
	}
}

func TestAuthInterrupt_otherPayloadStaysToolCall(t *testing.T) {
	intr, _ := confirmationInterrupt(t, map[string]any{"note": "x"})

	if got := intr["reason"]; got != "tool_call" {
		t.Errorf("reason = %v, want tool_call", got)
	}
	if diff := cmp.Diff(map[string]any{"note": "x"}, at(intr, "metadata", "adk", "confirmationPayload")); diff != "" {
		t.Errorf("confirmationPayload (-want +got):\n%s", diff)
	}
}

// A live run carries a ToolConfirmation value whose payload is a
// *toolauth.Config, not a decoded map.
func TestAuthInterrupt_liveConfigPayload(t *testing.T) {
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "e-auth"
	ev.Content = &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID:   "confirm-cal-1",
		Name: toolconfirmation.FunctionCallName,
		Args: map[string]any{
			"toolConfirmation":     toolconfirmation.ToolConfirmation{Hint: "Sign in to Google Calendar to continue.", Payload: signInConfig()},
			"originalFunctionCall": &genai.FunctionCall{ID: "call-cal-1", Name: "calendar_list"},
		},
	}}}}
	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	body := rec.Body.String()
	var intr map[string]any
	for _, sev := range parseSSEEvents(body) {
		if sev.Raw["type"] == "RUN_FINISHED" {
			outcome, _ := sev.Raw["outcome"].(map[string]any)
			intrs, _ := outcome["interrupts"].([]any)
			if len(intrs) == 1 {
				intr, _ = intrs[0].(map[string]any)
			}
		}
	}
	if intr == nil {
		t.Fatalf("no interrupt in %s", body)
	}
	if got := intr["reason"]; got != "auth_required" {
		t.Errorf("reason = %v, want auth_required", got)
	}
	if strings.Contains(body, "secret-7") {
		t.Error("SSE body carries the client secret")
	}
}
