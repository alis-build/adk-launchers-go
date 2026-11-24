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

// credentialInterrupt runs one adk_request_credential event carrying args
// through processEvent and returns the RUN_FINISHED interrupt and the SSE
// body.
func credentialInterrupt(t *testing.T, args map[string]any) (map[string]any, string) {
	t.Helper()
	return interruptFor(t, &genai.FunctionCall{ID: "cred-cal-1", Name: toolauth.RequestCredentialFunctionCallName, Args: args})
}

// confirmationInterrupt runs one adk_request_confirmation event carrying
// payload through processEvent and returns the RUN_FINISHED interrupt and
// the SSE body.
func confirmationInterrupt(t *testing.T, payload any) (map[string]any, string) {
	t.Helper()
	return interruptFor(t, &genai.FunctionCall{
		ID:   "confirm-cal-1",
		Name: toolconfirmation.FunctionCallName,
		Args: map[string]any{
			"toolConfirmation":     map[string]any{"hint": "Approve?", "payload": payload},
			"originalFunctionCall": map[string]any{"ID": "call-cal-1", "Name": "calendar_list"},
		},
	})
}

func interruptFor(t *testing.T, fc *genai.FunctionCall) (map[string]any, string) {
	t.Helper()
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
	ev := session.NewEvent(t.Context(), "inv1")
	ev.InvocationID = "e-auth"
	ev.LongRunningToolIDs = []string{fc.ID}
	// ADK writes its credential request as a user-role event.
	role := genai.RoleModel
	if fc.Name == toolauth.RequestCredentialFunctionCallName {
		role = genai.RoleUser
	}
	ev.Content = &genai.Content{Role: string(role), Parts: []*genai.Part{{FunctionCall: fc}}}
	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	// A sign-in interrupt waits for the end of the stream, as in the executor.
	if _, err := l.finishHeldInterrupts(e, state); err != nil {
		t.Fatalf("finishHeldInterrupts() error = %v", err)
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

// toolResultsFor runs ev through processEvent and returns the toolCallIds
// of the TOOL_CALL_RESULT events it emitted.
func toolResultsFor(t *testing.T, ev *session.Event) []any {
	t.Helper()
	l := newTestLauncher("test-app")
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: "t1", RootAppName: "test-app"}
	if _, err := l.processEvent(e, ev, state, nil); err != nil {
		t.Fatalf("processEvent() error = %v", err)
	}
	var ids []any
	for _, sev := range parseSSEEvents(rec.Body.String()) {
		if sev.Raw["type"] == "TOOL_CALL_RESULT" {
			ids = append(ids, sev.Raw["toolCallId"])
		}
	}
	return ids
}

// A call waiting for the user's sign-in answers with a placeholder that ADK
// replaces when it reruns the call; clients see only the real result.
func TestAuthInterrupt_placeholderResultNotStreamed(t *testing.T) {
	waiting := session.NewEvent(t.Context(), "inv1")
	waiting.Actions.RequestedAuthConfigs = map[string]toolauth.Config{"call-cal-1": *signInConfig()}
	waiting.Content = &genai.Content{Role: string(genai.RoleUser), Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: "call-cal-1", Name: "calendar_list", Response: map[string]any{"status": "waiting for the user to sign in to Google Calendar"},
	}}}}
	if got := toolResultsFor(t, waiting); len(got) != 0 {
		t.Errorf("placeholder emitted TOOL_CALL_RESULT for %v, want none", got)
	}

	ran := session.NewEvent(t.Context(), "inv1")
	ran.Content = &genai.Content{Role: string(genai.RoleUser), Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: "call-cal-1", Name: "calendar_list", Response: map[string]any{"status": 200},
	}}}}
	if diff := cmp.Diff([]any{"call-cal-1"}, toolResultsFor(t, ran)); diff != "" {
		t.Errorf("rerun TOOL_CALL_RESULT ids (-want +got):\n%s", diff)
	}
}

// In a live run ADK's credential call carries the config as a Go value.
func TestAuthInterrupt_emitsAuthRequired(t *testing.T) {
	intr, body := credentialInterrupt(t, map[string]any{"functionCallId": "call-cal-1", "authConfig": signInConfig()})

	if got := intr["reason"]; got != "auth_required" {
		t.Errorf("reason = %v, want auth_required", got)
	}
	if got := intr["toolCallId"]; got != "call-cal-1" {
		t.Errorf("toolCallId = %v, want call-cal-1", got)
	}
	if got := intr["id"]; got != "cred-cal-1" {
		t.Errorf("id = %v, want cred-cal-1", got)
	}
	if got := intr["message"]; got != "Sign in to continue." {
		t.Errorf("message = %v, want %q", got, "Sign in to continue.")
	}
	if got := at(intr, "metadata", "adk", "callName"); got != toolauth.RequestCredentialFunctionCallName {
		t.Errorf("metadata.adk.callName = %v, want %s", got, toolauth.RequestCredentialFunctionCallName)
	}
	if got := at(intr, "metadata", "adk", "invocationId"); got != "e-auth" {
		t.Errorf("metadata.adk.invocationId = %v, want e-auth", got)
	}
	if got := at(intr, "metadata", "adk", "authConfig", "exchangedAuthCredential", "oauth2", "authUri"); got != "https://accounts.example.com/o/oauth2/auth?state=s-1" {
		t.Errorf("authConfig authUri = %v", got)
	}
	if diff := cmp.Diff([]any{"exchangedAuthCredential"}, at(intr, "responseSchema", "required")); diff != "" {
		t.Errorf("responseSchema.required (-want +got):\n%s", diff)
	}
	if strings.Contains(body, "secret-7") {
		t.Error("SSE body carries the client secret")
	}
	for _, sev := range parseSSEEvents(body) {
		if sev.Raw["type"] == "TOOL_CALL_START" {
			t.Errorf("credential request emitted a tool call: %v", sev.Raw)
		}
	}
}

// A stored session hands the arguments back as JSON maps.
func TestAuthInterrupt_mapArgsFromStoredSession(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"functionCallId": "call-cal-1", "authConfig": signInConfig()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	intr, body := credentialInterrupt(t, args)

	if got := intr["reason"]; got != "auth_required" {
		t.Errorf("reason = %v, want auth_required", got)
	}
	if got := intr["toolCallId"]; got != "call-cal-1" {
		t.Errorf("toolCallId = %v, want call-cal-1", got)
	}
	if got := at(intr, "metadata", "adk", "authConfig", "exchangedAuthCredential", "oauth2", "authUri"); got != "https://accounts.example.com/o/oauth2/auth?state=s-1" {
		t.Errorf("authConfig authUri = %v", got)
	}
	if strings.Contains(body, "secret-7") {
		t.Error("SSE body carries the client secret")
	}
}

// Sign-ins no longer come as tool confirmations: a confirmation is always
// the tool's own approval, whatever its payload.
func TestAuthInterrupt_confirmationWithAuthPayloadIsToolCall(t *testing.T) {
	cfg, err := signInConfig().WithoutSecrets()
	if err != nil {
		t.Fatalf("WithoutSecrets() error = %v", err)
	}
	intr, _ := confirmationInterrupt(t, cfg)

	if got := intr["reason"]; got != "tool_call" {
		t.Errorf("reason = %v, want tool_call", got)
	}
	if got := at(intr, "metadata", "adk", "authConfig"); got != nil {
		t.Errorf("metadata.adk.authConfig = %v, want absent", got)
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
