package tooloauth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolauth"
	"google.golang.org/genai"
)

// pendingTTL is how long a run's record of sign-in requests is kept after
// the run's last start. A resumed call is dispatched right after its run
// starts, so the record only has to outlive that moment; an hour leaves a
// wide margin and bounds memory without tying the record to run ends, which
// ADK does not report reliably to every plugin.
const pendingTTL = time.Hour

// signInKey identifies a sign-in request by the author of its event, the
// credential key it asked for and the state it was issued with. ADK sends
// one request per credential key, so the state tells requests for the same
// key apart, and the author keeps another agent's events from standing in
// for the plugin's own.
type signInKey struct {
	author        string
	credentialKey string
	state         string
}

// pendingRun is the record of one invocation's sign-in requests, as of the
// latest start of a run under that invocation. ADK reruns an answered call
// under the paused run's invocation ID, so the answering run and the rerun
// share a record.
type pendingRun struct {
	requests map[signInKey]*toolauth.Config
	// claimed holds, for each request whose answer a call has taken up, the
	// sign-in that call runs, so the code is exchanged once.
	claimed map[signInKey]*signInFlight
	seen    time.Time
}

// signInFlight is one sign-in completing. The call that claimed it
// exchanges the code; other calls the answer reaches wait for it and share
// its outcome.
type signInFlight struct {
	callID string        // the call that claimed the request
	done   chan struct{} // closed when that call finishes
	err    error         // its outcome, set before done closes; nil on success
}

// pendingSignIns holds the sign-in requests of recent invocations. A tool
// callback cannot read the session (ADK hides it from tool contexts), so the
// plugin reads it when a run starts.
type pendingSignIns struct {
	mu           sync.Mutex
	byInvocation map[string]*pendingRun
}

// beforeRun records the sign-in requests in the run's session, so an answer
// can be checked against the requests the plugin issued, and prunes records
// unused for pendingTTL. Only a run answering a credential request can
// resume a call, so other runs record nothing.
func (r *resolver) beforeRun(ctx agent.InvocationContext) (*genai.Content, error) {
	if !answersCredentialRequest(ctx.UserContent()) {
		return nil, nil
	}
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
		run = &pendingRun{claimed: map[signInKey]*signInFlight{}}
		r.pending.byInvocation[ctx.InvocationID()] = run
	}
	// The session only grows, so a later snapshot covers an earlier one.
	run.requests = reqs
	run.seen = now
	return nil, nil
}

// lookupSignIn returns the request key names in the current invocation's
// record, if the plugin issued it.
func (r *resolver) lookupSignIn(ctx agent.Context, key signInKey) (*toolauth.Config, bool) {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return nil, false
	}
	run.seen = time.Now()
	req, ok := run.requests[key]
	return req, ok
}

// answersCredentialRequest reports whether content, a run's new user
// message, answers an adk_request_credential call: only such a run can
// resume a paused call.
func answersCredentialRequest(content *genai.Content) bool {
	if content == nil {
		return false
	}
	for _, p := range content.Parts {
		if p != nil && p.FunctionResponse != nil && p.FunctionResponse.Name == toolauth.RequestCredentialFunctionCallName {
			return true
		}
	}
	return false
}

// claim is the outcome of a call taking up a sign-in request's answer.
type claim int

const (
	// claimTaken: the call is the first to take up the answer and completes
	// the sign-in.
	claimTaken claim = iota
	// claimSameCall: the same call took it up before, so a duplicate answer
	// re-dispatched the call.
	claimSameCall
	// claimOtherCall: another call took it up. ADK keeps the answer in temp:
	// state for the rest of the invocation, so later calls see it too.
	claimOtherCall
)

// claimSignIn records that the current call takes up the answer to the
// request key names, and reports who had taken it up already, with the
// sign-in that call runs.
func (r *resolver) claimSignIn(ctx agent.Context, key signInKey) (claim, *signInFlight) {
	r.pending.mu.Lock()
	defer r.pending.mu.Unlock()
	run := r.pending.byInvocation[ctx.InvocationID()]
	if run == nil {
		return claimSameCall, nil
	}
	switch f, ok := run.claimed[key]; {
	case !ok:
		f = &signInFlight{callID: ctx.FunctionCallID(), done: make(chan struct{})}
		run.claimed[key] = f
		return claimTaken, f
	case f.callID == ctx.FunctionCallID():
		return claimSameCall, f
	default:
		return claimOtherCall, f
	}
}

// signInRequests returns the sign-in requests in sess, read from the
// RequestedAuthConfigs of events not authored by the user, keyed by author,
// credential key and issued state. The call arguments ADK sends are not
// read: a client can write events in an agent's name through ADK's REST
// API, but never their RequestedAuthConfigs. Requests without an OAuth2
// state are skipped.
func signInRequests(sess session.Session) map[signInKey]*toolauth.Config {
	if sess == nil {
		return nil
	}
	out := map[signInKey]*toolauth.Config{}
	events := sess.Events()
	for i := range events.Len() {
		ev := events.At(i)
		if ev == nil || ev.Author == "" || ev.Author == "user" {
			continue
		}
		for _, cfg := range ev.Actions.RequestedAuthConfigs {
			c := cfg.ExchangedAuthCredential
			if c == nil || c.OAuth2 == nil || c.OAuth2.State == "" {
				continue
			}
			out[signInKey{author: ev.Author, credentialKey: cfg.CredentialKey, state: c.OAuth2.State}] = &cfg
		}
	}
	return out
}

// storedAnswer reads the user's answer to a sign-in request for t, which
// ADK keeps in session state under "temp:" plus the request's credential
// key for the rest of the invocation. It reports false when there is none.
func storedAnswer(ctx agent.Context, t Tool) (*toolauth.OAuth2Auth, bool) {
	v, err := ctx.State().Get("temp:" + t.slot())
	if err != nil || v == nil {
		return nil, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var cred toolauth.Credential
	if json.Unmarshal(raw, &cred) != nil || cred.OAuth2 == nil {
		return nil, false
	}
	return cred.OAuth2, true
}

// resumeSignIn completes a sign-in when ADK reruns a call after the user
// answered the plugin's request, and reports false, so the call goes on to
// the stored-token lookup, when there is no answer for t. An answer without
// an auth response URI cancels the sign-in. The answer counts
// only for a request the plugin issued in this invocation's session; ADK
// pins the answer's state to the request it sent. When another call in this
// invocation already took the answer up, the call waits for that sign-in to
// finish and shares its outcome: it fails with the same error, or runs on
// the token that sign-in stored, or fails as out of date when there is none.
func (r *resolver) resumeSignIn(ctx agent.Context, toolName string, t Tool) (result map[string]any, handled bool, err error) {
	answer, ok := storedAnswer(ctx, t)
	if !ok {
		return nil, false, nil
	}
	outOfDate := fmt.Errorf("%s sign-in failed: the sign-in request is out of date", t.Provider)
	key := signInKey{author: ctx.AgentName(), credentialKey: t.slot(), state: answer.State}
	requested, ok := r.lookupSignIn(ctx, key)
	switch {
	case !ok && answer.AuthResponseURI == "":
		// Nothing to cancel: an empty answer for a request the plugin did not
		// issue changes nothing.
		return nil, false, nil
	case !ok:
		// Not a request this agent issued. ADK keeps an answer for the whole
		// invocation under a key with no agent in it, so this can be another
		// agent's sign-in: run on the token it stored, never exchanging.
		return r.runOnStoredToken(ctx, toolName, t, outOfDate)
	}
	c, f := r.claimSignIn(ctx, key)
	switch c {
	case claimSameCall:
		return nil, true, outOfDate
	case claimOtherCall:
		select {
		case <-f.done:
		case <-ctx.Done():
			return nil, true, unavailable(t)
		}
		if f.err != nil {
			return nil, true, f.err
		}
		return r.runOnStoredToken(ctx, toolName, t, outOfDate)
	}
	defer close(f.done)
	if answer.AuthResponseURI == "" {
		// An answer with no auth response is the user cancelling the sign-in.
		f.err = fmt.Errorf("%s sign-in was cancelled", t.Provider)
		return nil, true, f.err
	}
	result, err = r.completeSignIn(ctx, t, requested, answer)
	f.err = err
	return result, true, err
}

// authResponse is the part of a confirmation payload that marks it as a
// sign-in answer: an exchanged OAuth2 credential. Sign-ins now complete only
// through ADK's credential requests; the plugin reads this only to refuse a
// sign-in answer sent as a confirmation.
type authResponse struct {
	ExchangedAuthCredential struct {
		OAuth2 *toolauth.OAuth2Auth `json:"oauth2"`
	} `json:"exchangedAuthCredential"`
}

// runOnStoredToken lets a call that saw a sign-in answer it does not
// complete run on the user's stored token, and fails it with noToken when
// there is none.
func (r *resolver) runOnStoredToken(ctx agent.Context, toolName string, t Tool, noToken error) (map[string]any, bool, error) {
	switch _, err := r.credential(ctx, toolName, ctx.AppName(), ctx.UserID()); {
	case err == nil:
		return nil, true, nil
	case errors.Is(err, errMiss):
		return nil, true, noToken
	default:
		return nil, true, unavailable(t)
	}
}

// completeSignIn completes the sign-in of a rerun call, given the user's
// stored answer: it checks the response's
// state against the one the plugin sent, exchanges the code and stores the
// token, then returns nil so the tool runs with it in this turn. The checks
// run in a fixed order, so the state is verified before the code is ever
// sent, and no failure writes to the store. The caller has claimed the
// request, so the code is sent once. Failure texts name
// the provider only: never the provider's own error description, the
// endpoint's response, or the state.
func (r *resolver) completeSignIn(ctx agent.Context, t Tool, requested *toolauth.Config, o *toolauth.OAuth2Auth) (map[string]any, error) {
	failed := func(reason string) error {
		return fmt.Errorf("%s sign-in failed: %s", t.Provider, reason)
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
	// The request was looked up under t's current credential key, so a
	// Config changed since the pause never files a token granted for the old
	// scopes or client under the new key.
	key := auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: t.slot()}
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
	return nil, nil
}

// isSignInPayload reports whether a confirmation payload, a JSON map, is a
// sign-in config: one with an exchanged OAuth2 credential, with or without
// an auth response URI. Sign-ins now complete only through ADK's credential
// requests; the plugin uses this only to refuse a sign-in config sent as a
// confirmation.
func isSignInPayload(payload any) bool {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	var resp authResponse
	if json.Unmarshal(raw, &resp) != nil {
		return false
	}
	return resp.ExchangedAuthCredential.OAuth2 != nil
}
