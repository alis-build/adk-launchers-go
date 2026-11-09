package tooloauth

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
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

// pendingTTL is how long a run's record of sign-in requests is kept after
// the run's last start. A resumed call is dispatched right after its run
// starts, so the record only has to outlive that moment; an hour leaves a
// wide margin and bounds memory without tying the record to run ends, which
// ADK does not report reliably to every plugin.
const pendingTTL = time.Hour

// signInKey identifies a sign-in request by the author of its event and the
// call it paused, so an event from another author can neither replace nor
// clear the plugin's own request.
type signInKey struct {
	author string
	callID string
}

// signInRequest is one sign-in request found in a session.
type signInRequest struct {
	config *toolauth.Config
	// answered is set when a reply to the paused call follows the request:
	// the sign-in already completed or failed, and its code must not be sent
	// again.
	answered bool
	// conflicting is set when the session holds two different sign-in
	// requests for the same call, which only a tampered session does.
	conflicting bool
}

// pendingRun is the record of one invocation's sign-in requests, as of the
// latest start of a run under that invocation. Resumes run under the paused
// run's invocation ID, so several runs can share a record.
type pendingRun struct {
	requests map[signInKey]signInRequest
	// claimed holds the requests whose code a resume is exchanging, so a
	// concurrent duplicate of the reply does not send the code again.
	claimed map[signInKey]bool
	seen    time.Time
}

// pendingSignIns holds the sign-in requests of recent invocations. A tool
// callback cannot read the session (ADK hides it from tool contexts), so the
// plugin reads it when a run starts.
type pendingSignIns struct {
	mu           sync.Mutex
	byInvocation map[string]*pendingRun
}

// beforeRun records the sign-in requests in the run's session, so a resumed
// call can be checked against the request the plugin sent, and prunes
// records no run has started under for pendingTTL.
func (r *resolver) beforeRun(ctx agent.InvocationContext) (*genai.Content, error) {
	reqs := signInRequests(ctx.Session())
	now := time.Now()
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	for id, run := range r.pending.byInvocation {
		if now.Sub(run.seen) > pendingTTL {
			delete(r.pending.byInvocation, id)
		}
	}
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		if len(reqs) == 0 {
			return nil, nil
		}
		if r.pending.byInvocation == nil {
			r.pending.byInvocation = map[string]*pendingRun{}
		}
		run = &pendingRun{claimed: map[signInKey]bool{}}
		r.pending.byInvocation[ctx.InvocationID()] = run
	}
	// The session only grows, so a later snapshot covers an earlier one.
	run.requests = reqs
	run.seen = now
	return nil, nil
}

// lookupSignIn returns the sign-in request the current agent made for the
// current call, if any, as ADK itself matches confirmations by author.
func (r *resolver) lookupSignIn(ctx agent.Context) (signInRequest, bool) {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return signInRequest{}, false
	}
	req, ok := run.requests[signInKey{author: ctx.AgentName(), callID: ctx.FunctionCallID()}]
	return req, ok
}

// claimSignIn marks the current call's sign-in request as being exchanged,
// and reports false when another resume already claimed it.
func (r *resolver) claimSignIn(ctx agent.Context) bool {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return false
	}
	key := signInKey{author: ctx.AgentName(), callID: ctx.FunctionCallID()}
	if run.claimed[key] {
		return false
	}
	run.claimed[key] = true
	return true
}

// signInRequests returns the sign-in requests in sess, keyed by author and
// the ID of the call each paused. User-authored events never count. A
// request is marked answered when a non-user reply to its call follows it,
// and conflicting when another request for the same call carries a
// different state.
func signInRequests(sess session.Session) map[signInKey]signInRequest {
	if sess == nil {
		return nil
	}
	out := map[signInKey]signInRequest{}
	events := sess.Events()
	for i := range events.Len() {
		ev := events.At(i)
		if ev == nil || ev.Author == "" || ev.Author == "user" || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			switch {
			case p == nil:
			case p.FunctionResponse != nil:
				key := signInKey{author: ev.Author, callID: p.FunctionResponse.ID}
				if req, ok := out[key]; ok {
					req.answered = true
					out[key] = req
				}
			case p.FunctionCall != nil && p.FunctionCall.Name == toolconfirmation.FunctionCallName:
				orig, err := toolconfirmation.OriginalCallFrom(p.FunctionCall)
				if err != nil || orig.ID == "" {
					continue
				}
				cfg, ok := decodeRequestedConfig(p.FunctionCall.Args["toolConfirmation"])
				if !ok {
					continue
				}
				key := signInKey{author: ev.Author, callID: orig.ID}
				if prev, seen := out[key]; seen && prev.config.ExchangedAuthCredential.OAuth2.State != cfg.ExchangedAuthCredential.OAuth2.State {
					out[key] = signInRequest{config: cfg, conflicting: true}
					continue
				}
				out[key] = signInRequest{config: cfg}
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
// tool. The checks run in a fixed order, so the state is verified before the
// code is ever sent, and no failure writes to the store. Failure texts name
// the provider only: never the provider's own error description, the
// endpoint's response, or the state.
func (r *resolver) completeSignIn(ctx agent.Context, t Tool, requested *toolauth.Config, conf *toolconfirmation.ToolConfirmation) (map[string]any, error) {
	failed := func(reason string) error {
		return fmt.Errorf("%s sign-in failed: %s", t.Provider, reason)
	}
	if !conf.Confirmed {
		return nil, fmt.Errorf("%s sign-in was declined", t.Provider)
	}
	o, ok := decodeAuthResponse(conf.Payload)
	if !ok {
		return nil, failed("no auth response")
	}
	u, err := url.Parse(o.AuthResponseURI)
	if err != nil {
		return nil, failed("no auth response")
	}
	q := u.Query()
	if q.Has("error") {
		if slices.Contains(q["error"], "access_denied") {
			return nil, fmt.Errorf("%s sign-in was declined", t.Provider)
		}
		return nil, failed("the provider returned an error")
	}
	// Exactly one state: url.Values.Get would read only the first of several.
	if len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(requested.ExchangedAuthCredential.OAuth2.State)) != 1 {
		return nil, failed("state mismatch")
	}
	code := q.Get("code")
	if code == "" {
		return nil, failed("no authorization code")
	}
	key := auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: t.slot()}
	// A Config changed between pause and resume would file a token granted
	// for the old scopes or client under the new key.
	if subtle.ConstantTimeCompare([]byte(requested.CredentialKey), []byte(key.Key)) != 1 {
		return nil, failed("the sign-in request is out of date")
	}
	// A code may be sent once: a concurrent duplicate of this reply waits
	// for nothing and fails instead.
	if !r.claimSignIn(ctx) {
		return nil, failed("the sign-in request is out of date")
	}
	// The exchange repeats the redirect URI the authorization used (RFC 6749
	// section 4.1.3); ADK Web rewrites it to its own origin and reports it.
	redirect := o.RedirectURI
	if redirect == "" {
		redirect = t.RedirectURI
	}
	ot, err := r.oauth2Config(t, redirect).Exchange(r.tokenContext(ctx), code)
	if err != nil {
		// Not wrapped: a RetrieveError's text carries the endpoint's body.
		return nil, failed("token exchange refused")
	}
	tok := &Token{AccessToken: ot.AccessToken, TokenType: ot.TokenType, RefreshToken: ot.RefreshToken, Expiry: ot.Expiry}
	if err := r.save(ctx, key, tok, time.Now()); err != nil {
		return nil, unavailable(t)
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
