package tooloauth

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// connectedResult is the tool result of a completed sign-in. The tool itself
// does not run on the sign-in's confirmation, which ADK would also accept as
// the tool's own approval; the model's next call is a fresh one.
const connectedResult = "connected to %s, call the tool again"

// signInRequest is one sign-in request found in a session: the auth config
// the plugin sent, and the author of the event that carried it.
type signInRequest struct {
	author string
	config *toolauth.Config
}

// signInKey identifies a sign-in request by the author of its event and the
// call it paused, so an event from another author can neither replace nor
// clear the plugin's own request.
type signInKey struct {
	author string
	callID string
}

// pendingRun is what beforeRun recorded for one invocation: its sign-in
// requests, and how many runs of it are in flight. A resume runs under the
// paused run's invocation ID, so two resumes of one turn can overlap; the
// record is dropped only when the last of them ends.
type pendingRun struct {
	runs     int
	requests map[signInKey]*toolauth.Config
}

// pendingSignIns holds the sign-in requests of running invocations. A tool
// callback cannot read the session (ADK hides it from tool contexts), so the
// plugin reads it when a run starts and drops it when the run ends. A run
// whose AfterRun hook never fires leaves its record behind: in ADK's live
// mode that happens when the agent fails to start, when the event iterator
// is never ranged, or when another plugin's BeforeRun ends the run early.
// Records are small, one per such invocation.
type pendingSignIns struct {
	mu           sync.Mutex
	byInvocation map[string]*pendingRun
}

// beforeRun records the sign-in requests in the run's session, so a resumed
// call can be checked against the request the plugin sent.
func (r *resolver) beforeRun(ctx agent.InvocationContext) (*genai.Content, error) {
	reqs := signInRequests(ctx.Session())
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	// Every run is counted, even one with nothing to record yet: a resume
	// can start before the run it resumes has ended, under the same
	// invocation ID, and must not lose its record when that run ends.
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		if r.pending.byInvocation == nil {
			r.pending.byInvocation = map[string]*pendingRun{}
		}
		run = &pendingRun{}
		r.pending.byInvocation[ctx.InvocationID()] = run
	}
	run.runs++
	// The session only grows, so a later run's snapshot covers an earlier
	// one's.
	run.requests = reqs
	return nil, nil
}

// afterRun drops what beforeRun recorded once no run of the invocation is
// left.
func (r *resolver) afterRun(ctx agent.InvocationContext) {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return
	}
	if run.runs--; run.runs <= 0 {
		delete(r.pending.byInvocation, ctx.InvocationID())
	}
}

// requestedAuthConfig returns the auth config the plugin sent when it paused
// the current call for sign-in, from a request event authored by the
// current agent, as ADK itself matches confirmations. It reports false when
// there is none.
func (r *resolver) requestedAuthConfig(ctx agent.Context) (*toolauth.Config, bool) {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return nil, false
	}
	cfg, ok := run.requests[signInKey{author: ctx.AgentName(), callID: ctx.FunctionCallID()}]
	return cfg, ok
}

// signInRequests returns the sign-in requests in sess, keyed by author and
// the ID of the call each paused; a later request from the same author for
// the same call replaces an earlier one. User-authored events never count.
func signInRequests(sess session.Session) map[signInKey]*toolauth.Config {
	if sess == nil {
		return nil
	}
	out := map[signInKey]*toolauth.Config{}
	events := sess.Events()
	for i := range events.Len() {
		ev := events.At(i)
		if ev == nil || ev.Author == "" || ev.Author == "user" || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p == nil || p.FunctionCall == nil || p.FunctionCall.Name != toolconfirmation.FunctionCallName {
				continue
			}
			orig, err := toolconfirmation.OriginalCallFrom(p.FunctionCall)
			if err != nil || orig.ID == "" {
				continue
			}
			key := signInKey{author: ev.Author, callID: orig.ID}
			if cfg, ok := decodeRequestedConfig(p.FunctionCall.Args["toolConfirmation"]); ok {
				out[key] = cfg
			} else {
				delete(out, key)
			}
		}
	}
	return out
}

// decodeRequestedConfig decodes a request's toolConfirmation argument, a Go
// value in an in-memory session and a JSON map in a persisted one, and
// returns its payload when it is a sign-in request carrying OAuth2 state.
func decodeRequestedConfig(arg any) (*toolauth.Config, bool) {
	raw, err := json.Marshal(arg)
	if err != nil {
		return nil, false
	}
	var tc struct {
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &tc) != nil || len(tc.Payload) == 0 || string(tc.Payload) == "null" {
		return nil, false
	}
	var cfg toolauth.Config
	if json.Unmarshal(tc.Payload, &cfg) != nil {
		return nil, false
	}
	if c := cfg.ExchangedAuthCredential; c == nil || c.OAuth2 == nil || c.OAuth2.State == "" {
		return nil, false
	}
	return &cfg, true
}

// authResponse is the part of a client's filled auth config the plugin
// reads. Only the exchanged credential's response and redirect URIs are
// taken: the rest of a client-returned config is untrusted, and the client
// ID and secret always come from the plugin's own Config.
type authResponse struct {
	ExchangedAuthCredential struct {
		OAuth2 *toolauth.OAuth2Auth `json:"oauth2"`
	} `json:"exchangedAuthCredential"`
}

// completeSignIn handles the resumed call of a sign-in pause: it checks the
// response's state against the one the plugin sent, exchanges the code,
// stores the token and answers with connectedResult instead of running the
// tool.
func (r *resolver) completeSignIn(ctx agent.Context, t Tool, requested *toolauth.Config, conf *toolconfirmation.ToolConfirmation) (map[string]any, error) {
	failed := fmt.Errorf("%s sign-in failed: no auth response", t.Provider)
	if !conf.Confirmed {
		return nil, failed
	}
	o, ok := decodeAuthResponse(conf.Payload)
	if !ok {
		return nil, failed
	}
	u, err := url.Parse(o.AuthResponseURI)
	if err != nil {
		return nil, failed
	}
	q := u.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(requested.ExchangedAuthCredential.OAuth2.State)) != 1 {
		return nil, failed
	}
	code := q.Get("code")
	if code == "" {
		return nil, failed
	}
	// The exchange repeats the redirect URI the authorization used (RFC 6749
	// section 4.1.3); ADK Web rewrites it to its own origin and reports it.
	redirect := o.RedirectURI
	if redirect == "" {
		redirect = t.RedirectURI
	}
	ot, err := r.oauth2Config(t, redirect).Exchange(r.tokenContext(ctx), code)
	if err != nil {
		return nil, failed
	}
	tok := &Token{AccessToken: ot.AccessToken, TokenType: ot.TokenType, RefreshToken: ot.RefreshToken, Expiry: ot.Expiry}
	key := auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: t.slot()}
	if err := r.save(ctx, key, tok, time.Now()); err != nil {
		return nil, failed
	}
	return map[string]any{"result": fmt.Sprintf(connectedResult, t.Provider)}, nil
}

// decodeAuthResponse reads the exchanged OAuth2 credential from a client's
// confirmation payload, a JSON map, and reports false when it has no auth
// response URI.
func decodeAuthResponse(payload any) (*toolauth.OAuth2Auth, bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	var resp authResponse
	if json.Unmarshal(raw, &resp) != nil {
		return nil, false
	}
	o := resp.ExchangedAuthCredential.OAuth2
	if o == nil || o.AuthResponseURI == "" {
		return nil, false
	}
	return o, true
}
