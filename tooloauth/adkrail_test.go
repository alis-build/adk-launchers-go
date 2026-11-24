package tooloauth

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// These tests pin how ADK v2.8.0 runs its end-user sign-in rail, which the
// plugin builds on: a tool response that sets RequestedAuthConfigs makes ADK
// send an adk_request_credential call and end the run; the user's answer is
// stored under "temp:" plus the request's credential key and the call runs
// again. They use a probe plugin and tool of their own, so an ADK upgrade
// that changes the rail fails here before it fails the plugin's tests.

const railProbeKey = "probe-key"

// railRead is what the probe plugin saw on a call that found an answer.
type railRead struct {
	callID string
	cred   toolauth.Credential
}

// railProbe drives probe_tool through ADK's sign-in rail: with no stored
// answer the plugin requests the tooloauth auth config under railProbeKey,
// and with one it records the answer and lets the tool run.
type railProbe struct {
	h      *harness
	issued string // the state of the last request

	mu    sync.Mutex
	runs  int
	reads []railRead
}

func newRailProbe(t *testing.T) *railProbe {
	t.Helper()
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	r := newResolver(cfg)
	signIn := cfg.Tools["calendar_list"]
	p := &railProbe{}
	probeTool, err := functiontool.New(functiontool.Config{Name: "probe_tool", Description: "Probes the sign-in rail."},
		func(agent.Context, struct{}) (map[string]any, error) {
			p.mu.Lock()
			p.runs++
			p.mu.Unlock()
			return map[string]any{"ran": true}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	probe, err := plugin.New(plugin.Config{Name: "rail-probe", BeforeToolCallback: func(ctx agent.Context, tl tool.Tool, _ map[string]any) (map[string]any, error) {
		if tl.Name() != "probe_tool" {
			return nil, nil
		}
		if v, err := ctx.State().Get("temp:" + railProbeKey); err == nil && v != nil {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			var cred toolauth.Credential
			if err := json.Unmarshal(raw, &cred); err != nil {
				return nil, err
			}
			p.mu.Lock()
			p.reads = append(p.reads, railRead{callID: ctx.FunctionCallID(), cred: cred})
			p.mu.Unlock()
			return nil, nil
		}
		req, err := r.authRequest(signIn, auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: railProbeKey})
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.issued = req.ExchangedAuthCredential.OAuth2.State
		p.mu.Unlock()
		actions := ctx.Actions()
		if actions.RequestedAuthConfigs == nil {
			actions.RequestedAuthConfigs = map[string]toolauth.Config{}
		}
		actions.RequestedAuthConfigs[ctx.FunctionCallID()] = *req
		return map[string]any{"status": "waiting"}, nil
	}})
	if err != nil {
		t.Fatalf("plugin.New() error = %v", err)
	}
	p.h = newHarnessOn(t, session.InMemoryService(), "app-rail", "probe_tool", 1, cfg, []*plugin.Plugin{probe}, probeTool)
	return p
}

func (p *railProbe) toolRuns() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runs
}

func (p *railProbe) answers() []railRead {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.reads)
}

// pause runs the first turn and returns its events and the credential call.
func (p *railProbe) pause(t *testing.T) ([]*session.Event, *genai.FunctionCall) {
	t.Helper()
	events := p.h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("probe", genai.RoleUser))
	var found *genai.FunctionCall
	for _, ev := range events {
		for _, fc := range eventCalls(ev) {
			if fc.Name == toolauth.RequestCredentialFunctionCallName {
				if found != nil {
					t.Fatalf("more than one %s call", fc.Name)
				}
				found = fc
			}
		}
	}
	if found == nil {
		t.Fatal("no adk_request_credential call")
	}
	return events, found
}

// answer sends response to credential call fc and returns the run's events
// and its error, if any.
func (p *railProbe) answer(t *testing.T, fc *genai.FunctionCall, response map[string]any) ([]*session.Event, error) {
	t.Helper()
	content := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: fc.ID, Name: toolauth.RequestCredentialFunctionCallName, Response: response,
	}}}}
	var events []*session.Event
	for ev, err := range p.h.runner.Run(t.Context(), "user-7", "s-1", content, agent.RunConfig{}) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func eventCalls(ev *session.Event) []*genai.FunctionCall {
	if ev == nil || ev.Content == nil {
		return nil
	}
	var calls []*genai.FunctionCall
	for _, part := range ev.Content.Parts {
		if part != nil && part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

// credentialArgs decodes an adk_request_credential call's arguments.
func credentialArgs(t *testing.T, fc *genai.FunctionCall) (toolauth.ToolArguments, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(fc.Args)
	if err != nil {
		t.Fatalf("marshal %s args: %v", fc.Name, err)
	}
	var args toolauth.ToolArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatalf("decode %s args %s: %v", fc.Name, raw, err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &struct {
		AuthConfig *map[string]any `json:"authConfig"`
	}{&config}); err != nil {
		t.Fatalf("decode %s authConfig %s: %v", fc.Name, raw, err)
	}
	return args, config
}

// noStateDelta fails when any event writes state: the rail must keep the
// answer out of events.
func noStateDelta(t *testing.T, events []*session.Event) {
	t.Helper()
	for _, ev := range events {
		if len(ev.Actions.StateDelta) > 0 {
			t.Errorf("event by %s carries a state delta with keys %v", ev.Author, slices.Sorted(maps.Keys(ev.Actions.StateDelta)))
		}
	}
}

const railResponseURI = "http://localhost:4200/?state=%s&code=code-9"

func TestADKCredentialRail_requestEndsTheRun(t *testing.T) {
	p := newRailProbe(t)

	events, fc := p.pause(t)

	last := events[len(events)-1]
	if !slices.Contains(last.LongRunningToolIDs, fc.ID) {
		t.Errorf("credential event LongRunningToolIDs = %v, want it to hold %q", last.LongRunningToolIDs, fc.ID)
	}
	if calls := eventCalls(last); len(calls) != 1 || calls[0].ID != fc.ID {
		t.Errorf("the run's last event calls %v, want only the credential call %q", calls, fc.ID)
	}
	args, _ := credentialArgs(t, fc)
	if args.AuthConfig.CredentialKey != railProbeKey {
		t.Errorf("authConfig.credentialKey = %q, want %q", args.AuthConfig.CredentialKey, railProbeKey)
	}
	if args.FunctionCallID != "call-1" {
		t.Errorf("functionCallId = %q, want call-1", args.FunctionCallID)
	}
	if raw, _ := json.Marshal(fc.Args); strings.Contains(string(raw), "clientSecret") || strings.Contains(string(raw), "secret-7") {
		t.Errorf("credential call args carry the client secret: %s", raw)
	}
	if got := p.toolRuns(); got != 0 {
		t.Errorf("probe_tool ran %d times, want 0", got)
	}
}

func TestADKCredentialRail_answerRerunsTheCall(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)
	_, config := credentialArgs(t, fc)
	uri := strings.Replace(railResponseURI, "%s", p.issued, 1)
	o := config["exchangedAuthCredential"].(map[string]any)["oauth2"].(map[string]any)
	o["authResponseUri"] = uri
	o["redirectUri"] = "http://localhost:4200"

	events, err := p.answer(t, fc, config)
	if err != nil {
		t.Fatalf("answer run error = %v", err)
	}

	reads := p.answers()
	if len(reads) != 1 {
		t.Fatalf("probe saw %d answered calls, want 1", len(reads))
	}
	if reads[0].callID != "call-1" {
		t.Errorf("rerun call ID = %q, want call-1", reads[0].callID)
	}
	o2 := reads[0].cred.OAuth2
	if o2 == nil || o2.State != p.issued || o2.AuthResponseURI != uri || o2.RedirectURI != "http://localhost:4200" {
		t.Errorf("stored answer oauth2 = %+v, want state %q, authResponseUri %q, redirectUri http://localhost:4200", o2, p.issued, uri)
	}
	if o2 != nil && o2.ClientSecret != "" {
		t.Error("stored answer carries the client secret")
	}
	if got := p.toolRuns(); got != 1 {
		t.Errorf("probe_tool ran %d times, want 1", got)
	}
	noStateDelta(t, events)
}

func TestADKCredentialRail_bareCredentialAnswer(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)
	uri := strings.Replace(railResponseURI, "%s", p.issued, 1)

	events, err := p.answer(t, fc, map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": uri, "redirectUri": "http://localhost:4200",
	}})
	if err != nil {
		t.Fatalf("answer run error = %v", err)
	}

	reads := p.answers()
	if len(reads) != 1 {
		t.Fatalf("probe saw %d answered calls, want 1", len(reads))
	}
	if o := reads[0].cred.OAuth2; o == nil || o.State != p.issued || o.AuthResponseURI != uri {
		t.Errorf("stored answer oauth2 = %+v, want the issued state %q and authResponseUri %q", o, p.issued, uri)
	}
	if got := p.toolRuns(); got != 1 {
		t.Errorf("probe_tool ran %d times, want 1", got)
	}
	noStateDelta(t, events)
}

// A client cannot pick the state: ADK takes it from its own request.
func TestADKCredentialRail_answerStateComesFromTheRequest(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)

	if _, err := p.answer(t, fc, map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"state": "forged", "authResponseUri": strings.Replace(railResponseURI, "%s", "forged", 1),
	}}); err != nil {
		t.Fatalf("answer run error = %v", err)
	}

	reads := p.answers()
	if len(reads) != 1 {
		t.Fatalf("probe saw %d answered calls, want 1", len(reads))
	}
	if o := reads[0].cred.OAuth2; o == nil || o.State != p.issued {
		t.Errorf("stored answer state = %+v, want the issued state %q", o, p.issued)
	}
}

func TestADKCredentialRail_cancelAnswer(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)

	if _, err := p.answer(t, fc, map[string]any{"authType": "oauth2", "oauth2": map[string]any{}}); err != nil {
		t.Fatalf("answer run error = %v", err)
	}

	reads := p.answers()
	if len(reads) != 1 {
		t.Fatalf("probe saw %d answered calls, want 1", len(reads))
	}
	if o := reads[0].cred.OAuth2; o == nil || o.State != p.issued || o.AuthResponseURI != "" {
		t.Errorf("stored answer oauth2 = %+v, want the issued state %q and no authResponseUri", o, p.issued)
	}
}

func TestADKCredentialRail_unparseableAnswerEndsTheRun(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)

	_, err := p.answer(t, fc, map[string]any{"exchangedAuthCredential": map[string]any{"oauth2": map[string]any{
		"authResponseUri": strings.Replace(railResponseURI, "%s", p.issued, 1),
	}}})

	if err == nil || !strings.Contains(err.Error(), "neither an auth config nor a credential") {
		t.Errorf("answer run error = %v, want one saying the answer is neither an auth config nor a credential", err)
	}
	if got := p.toolRuns(); got != 0 {
		t.Errorf("probe_tool ran %d times, want 0", got)
	}
}

func TestADKCredentialRail_repeatedAnswerRerunsNothing(t *testing.T) {
	p := newRailProbe(t)
	_, fc := p.pause(t)
	answer := map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": strings.Replace(railResponseURI, "%s", p.issued, 1),
	}}
	if _, err := p.answer(t, fc, answer); err != nil {
		t.Fatalf("first answer run error = %v", err)
	}

	events, err := p.answer(t, fc, answer)
	if err != nil {
		t.Fatalf("repeated answer run error = %v", err)
	}

	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if r := part.FunctionResponse; r != nil && r.ID == "call-1" {
				t.Errorf("repeated answer produced a response for call-1: %v", r.Response)
			}
		}
	}
	if got := len(p.answers()); got != 1 {
		t.Errorf("probe saw %d answered calls, want 1", got)
	}
}
