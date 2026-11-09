package tooloauth

import (
	"net/http"
	"testing"
	"time"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// rejectCase pauses calendar_list for sign-in as user-7, answers the pause
// with the response respond builds from the paused payload and state, and
// returns the resumed turn's events.
func rejectCase(t *testing.T, h *harness, respond func(payload map[string]any, state string) map[string]any) []*session.Event {
	t.Helper()
	fc := confirmationCall(t, h.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	return h.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, respond(payload, pausedState(t, payload))))
}

// rejected asserts the resumed call failed with want, the tool did not run,
// nothing was stored, and the token endpoint was called endpointCalls times.
func rejected(t *testing.T, events []*session.Event, cfg Config, rs *resourceServer, te *tokenEndpoint, want string, endpointCalls int) {
	t.Helper()
	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response on resume")
	}
	if got, _ := resp["error"].(string); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	if tok, ok := storedFor(t, cfg, "user-7"); ok {
		t.Errorf("stored token = %+v, want none", tok)
	}
	if got := te.callCount(); got != endpointCalls {
		t.Errorf("token endpoint called %d times, want %d", got, endpointCalls)
	}
}

// rejectSetup returns a harness for calendar_list with a token endpoint
// answering status and body.
func rejectSetup(t *testing.T, status int, body string) (*harness, Config, *resourceServer, *tokenEndpoint) {
	t.Helper()
	te := newTokenEndpoint(t, status, body, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	return newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs)), cfg, rs, te
}

// redirectResponse is a confirmed resume whose auth response URI carries
// query.
func redirectResponse(payload map[string]any, query string) map[string]any {
	return map[string]any{"confirmed": true, "payload": withADKWebResponse(payload, "http://localhost:4200/?"+query, "http://localhost:4200")}
}

func TestResumeReject_declined(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(map[string]any, string) map[string]any { return map[string]any{"confirmed": false} })
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in was declined", 0)
}

func TestResumeReject_providerDenied(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "error=access_denied&state="+state)
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in was declined", 0)
}

func TestResumeReject_providerError(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "error=server_error&error_description=db+down+for+user-7&state="+state)
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: the provider returned an error", 0)
}

func TestResumeReject_noAuthResponse(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(map[string]any, string) map[string]any {
		return map[string]any{"confirmed": true, "payload": map[string]any{}}
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: no auth response", 0)
}

func TestResumeReject_stateMismatch(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, _ string) map[string]any {
		return redirectResponse(payload, "state=forged-state&code=code-31")
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: state mismatch", 0)
}

func TestResumeReject_noCode(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "state="+state)
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: no authorization code", 0)
}

// The Config's scopes change between the pause and the resume: the token
// would be granted for the old scopes but filed under the new key.
func TestResumeReject_staleRequest(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	cfg := refreshConfig(newRecordingStore(), te, 72*time.Hour)
	rs := newResourceServer(t)
	before := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	fc := confirmationCall(t, before.run(t.Context(), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser)))
	payload := pausedPayload(t, fc)
	state := pausedState(t, payload)

	changed := refreshConfig(cfg.Store, te, 72*time.Hour)
	tl := changed.Tools["calendar_list"]
	tl.Scopes = append(append([]string(nil), tl.Scopes...), "https://www.example.com/auth/calendar.events")
	changed.Tools = map[string]Tool{"calendar_list": tl}
	after := newHarnessOn(t, before.sessions, "app-resume", "calendar_list", 1, changed, nil, calendarTool(t, changed, rs))
	events := after.run(t.Context(), "user-7", "s-1", confirmationResponse(fc, redirectResponse(payload, "state="+state+"&code=code-31")))

	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: the sign-in request is out of date", 0)
	if tok, ok := storedFor(t, changed, "user-7"); ok {
		t.Errorf("token stored under the new key = %+v, want none", tok)
	}
}

func TestResumeReject_exchangeRefused(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"code used for user-7"}`)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "state="+state+"&code=code-31")
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: token exchange refused", 1)
}

func TestResumeReject_storeFailureIsUnavailable(t *testing.T) {
	te := newTokenEndpoint(t, http.StatusOK, exchangeReply, nil)
	inner := newRecordingStore()
	cfg := refreshConfig(faultyStore{CredentialStore: inner, failSet: true}, te, 72*time.Hour)
	rs := newResourceServer(t)
	h := newHarness(t, "app-resume", "calendar_list", cfg, calendarTool(t, cfg, rs))
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "state="+state+"&code=code-31")
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in is unavailable right now", 1)
}

// A reply carrying the right state alongside another is refused: only the
// first of several would otherwise be read.
func TestResumeReject_duplicatedState(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "state="+state+"&state=forged-state&code=code-31")
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: state mismatch", 0)
}

// Any error parameter fails the sign-in, even an empty one next to the code.
func TestResumeReject_emptyErrorParameter(t *testing.T) {
	h, cfg, rs, te := rejectSetup(t, http.StatusOK, exchangeReply)
	events := rejectCase(t, h, func(payload map[string]any, state string) map[string]any {
		return redirectResponse(payload, "error=&state="+state+"&code=code-31")
	})
	rejected(t, events, cfg, rs, te, "Google Calendar sign-in failed: the provider returned an error", 0)
}
