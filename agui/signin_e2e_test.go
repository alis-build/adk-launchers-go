package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// These tests run a sign-in through every layer together: tooloauth asks
// for it, ADK sends adk_request_credential, agui turns that into an
// auth_required interrupt, and agui's resume answer goes back through ADK
// to tooloauth, which exchanges the code and lets the tool run.

const (
	e2eApp     = "signin-agent"
	e2eUser    = "user-1"
	e2eSession = "thread-1"
	e2eSecret  = "secret-e2e"
)

// callOnceModel calls calendar_list for a user message and answers "done"
// once a tool response comes back.
type callOnceModel struct{}

func (callOnceModel) Name() string { return "call-once" }

func (callOnceModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if n := len(req.Contents); n > 0 {
			for _, p := range req.Contents[n-1].Parts {
				if p != nil && p.FunctionResponse != nil {
					yield(&model.LLMResponse{Content: genai.NewContentFromText("done", genai.RoleModel)}, nil)
					return
				}
			}
		}
		yield(&model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{ID: "call-cal-1", Name: "calendar_list", Args: map[string]any{}},
		}}}}, nil)
	}
}

// signInStack is an ADK runtime whose agent's one tool needs the tooloauth
// sign-in, with a token endpoint that counts its calls.
type signInStack struct {
	rt       *adkrun.Runtime
	l        *aguiLauncher
	exchange func() int
	runs     func() int
}

func newSignInStack(t *testing.T) *signInStack {
	t.Helper()
	var mu sync.Mutex
	exchanges, runs := 0, 0
	te := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		exchanges++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-e2e","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-e2e"}`))
	}))
	t.Cleanup(te.Close)
	cfg := tooloauth.Config{
		HTTPClient: te.Client(),
		Tools: map[string]tooloauth.Tool{"calendar_list": {
			Provider:     "Google Calendar",
			ClientID:     "cid-e2e",
			ClientSecret: e2eSecret,
			RedirectURI:  "https://app.example.com/cb",
			Scopes:       []string{"calendar.read"},
			Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
				AuthorizationURL: "https://accounts.example.com/o/oauth2/auth",
				TokenURL:         te.URL + "/token",
			}}},
		}},
	}
	p, err := tooloauth.NewPlugin(cfg)
	if err != nil {
		t.Fatalf("tooloauth.NewPlugin() error = %v", err)
	}
	tl, err := functiontool.New(functiontool.Config{Name: "calendar_list", Description: "Lists calendar events."},
		func(agent.Context, struct{}) (map[string]any, error) {
			mu.Lock()
			runs++
			mu.Unlock()
			return map[string]any{"events": 3}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	a, err := llmagent.New(llmagent.Config{Name: e2eApp, Model: callOnceModel{}, Tools: []tool.Tool{tl}})
	if err != nil {
		t.Fatalf("llmagent.New() error = %v", err)
	}
	svc := session.InMemoryService()
	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: svc,
		PluginConfig:   runner.PluginConfig{Plugins: []*plugin.Plugin{p}},
	}, e2eApp)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := svc.Create(t.Context(), &session.CreateRequest{AppName: e2eApp, UserID: e2eUser, SessionID: e2eSession}); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	count := func(n *int) func() int {
		return func() int {
			mu.Lock()
			defer mu.Unlock()
			return *n
		}
	}
	return &signInStack{rt: rt, l: newTestLauncher(e2eApp, svc), exchange: count(&exchanges), runs: count(&runs)}
}

// run sends msg and streams the run's events through agui's processor. It
// returns the SSE body, the stream state, and the invocation IDs seen.
func (s *signInStack) run(t *testing.T, msg *genai.Content) (string, *streamState, []string) {
	t.Helper()
	_, evs, err := s.rt.RunSSE(t.Context(), adkrun.RunRequest{AppName: e2eApp, UserID: e2eUser, SessionID: e2eSession, NewMessage: *msg, Streaming: true})
	if err != nil {
		t.Fatalf("RunSSE: %v", err)
	}
	e, rec := newTestEmitter()
	state := &streamState{RunID: "r1", ThreadID: e2eSession, RootAppName: e2eApp}
	var invocations []string
	for ev, err := range evs {
		if err != nil {
			t.Fatalf("run event error: %v", err)
		}
		if ev == nil {
			continue
		}
		invocations = append(invocations, ev.InvocationID)
		done, err := s.l.processEvent(e, ev, state, nil)
		if err != nil {
			t.Fatalf("processEvent: %v", err)
		}
		if done {
			break
		}
	}
	return rec.Body.String(), state, invocations
}

// pause runs the user's first message, which ends on one auth_required
// interrupt, and returns it with its records as stored in session state.
func (s *signInStack) pause(t *testing.T) (types.Interrupt, []interrupt.Record, string) {
	t.Helper()
	body, state, _ := s.run(t, genai.NewContentFromText("what is on my calendar?", genai.RoleUser))
	if len(state.EmittedInterrupts) != 1 {
		t.Fatalf("interrupts = %+v, want one", state.EmittedInterrupts)
	}
	intr := state.EmittedInterrupts[0]
	if intr.Reason != interrupt.ReasonAuthRequired || intr.ToolCallID != "call-cal-1" {
		t.Fatalf("interrupt = %+v, want auth_required for call-cal-1", intr)
	}
	raw, err := json.Marshal(interrupt.RecordsFromInterrupts(state.EmittedInterrupts))
	if err != nil {
		t.Fatalf("marshal records: %v", err)
	}
	var records []interrupt.Record
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatalf("decode records: %v", err)
	}
	if strings.Contains(body, e2eSecret) {
		t.Error("pause SSE body carries the client secret")
	}
	return intr, records, body
}

// issuedState returns the OAuth state in an auth_required interrupt's
// authConfig.
func issuedState(t *testing.T, intr types.Interrupt) string {
	t.Helper()
	raw, err := json.Marshal(intr.Metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	var meta struct {
		ADK struct {
			AuthConfig toolauth.Config `json:"authConfig"`
		} `json:"adk"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.ADK.AuthConfig.ExchangedAuthCredential == nil || meta.ADK.AuthConfig.ExchangedAuthCredential.OAuth2 == nil {
		t.Fatalf("interrupt metadata %s has no auth config: %v", raw, err)
	}
	return meta.ADK.AuthConfig.ExchangedAuthCredential.OAuth2.State
}

// resume validates entry against records and runs it, as agui's executor
// does.
func (s *signInStack) resume(t *testing.T, records []interrupt.Record, entry types.ResumeEntry) (string, []string) {
	t.Helper()
	entries := []types.ResumeEntry{entry}
	if err := interrupt.ValidateResumeAgainstPending(entries, records, time.Now()); err != nil {
		t.Fatalf("ValidateResumeAgainstPending: %v", err)
	}
	content, err := interrupt.EntriesToResumeContent(entries, records)
	if err != nil {
		t.Fatalf("EntriesToResumeContent: %v", err)
	}
	body, _, invocations := s.run(t, content)
	return body, invocations
}

// toolResult returns the content of the TOOL_CALL_RESULT for callID in an
// SSE body.
func toolResult(t *testing.T, body, callID string) (string, bool) {
	t.Helper()
	for _, sev := range parseSSEEvents(body) {
		if sev.Raw["type"] == "TOOL_CALL_RESULT" && sev.Raw["toolCallId"] == callID {
			content, _ := sev.Raw["content"].(string)
			return content, true
		}
	}
	return "", false
}

func TestAuthRequiredResume_completesToolOAuthSignIn(t *testing.T) {
	s := newSignInStack(t)
	intr, records, _ := s.pause(t)

	body, invocations := s.resume(t, records, types.ResumeEntry{InterruptID: intr.ID, Status: types.ResumeStatusResolved, Payload: map[string]any{
		"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
			"authResponseUri": fmt.Sprintf("https://app.example.com/cb?state=%s&code=code-e2e", issuedState(t, intr)),
			"redirectUri":     "https://app.example.com/cb",
		}},
	}})

	for _, id := range invocations {
		if id != records[0].InvocationID {
			t.Errorf("resume event invocation = %q, want the paused %q", id, records[0].InvocationID)
		}
	}
	if got := s.exchange(); got != 1 {
		t.Errorf("token endpoint called %d times, want 1", got)
	}
	if got := s.runs(); got != 1 {
		t.Errorf("calendar_list ran %d times, want 1", got)
	}
	if content, ok := toolResult(t, body, "call-cal-1"); !ok || content != `{"events":3}` {
		t.Errorf("TOOL_CALL_RESULT for call-cal-1 = %q, %v; want the tool's result", content, ok)
	}
	if strings.Contains(body, e2eSecret) {
		t.Error("resume SSE body carries the client secret")
	}
}

func TestAuthRequiredResume_cancelReachesToolOAuth(t *testing.T) {
	s := newSignInStack(t)
	intr, records, _ := s.pause(t)

	body, _ := s.resume(t, records, types.ResumeEntry{InterruptID: intr.ID, Status: types.ResumeStatusCancelled})

	if got := s.exchange(); got != 0 {
		t.Errorf("token endpoint called %d times, want 0", got)
	}
	if got := s.runs(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	content, ok := toolResult(t, body, "call-cal-1")
	if !ok || !strings.Contains(content, "Google Calendar sign-in was cancelled") {
		t.Errorf("TOOL_CALL_RESULT for call-cal-1 = %q, %v; want the cancelled error", content, ok)
	}
}
