package tooloauth

import (
	"context"
	"testing"
	"time"

	"google.golang.org/adk/v2/auth"
	"google.golang.org/genai"
)

func TestIsNonInteractive_unmarkedContext(t *testing.T) {
	if IsNonInteractive(context.Background()) {
		t.Error("IsNonInteractive(Background) = true, want false")
	}
}

func TestIsNonInteractive_markedContext(t *testing.T) {
	if !IsNonInteractive(WithNonInteractive(context.Background())) {
		t.Error("IsNonInteractive(WithNonInteractive(Background)) = false, want true")
	}
}

func TestPlugin_nonInteractiveMiss_returnsConnectError(t *testing.T) {
	cfg := calendarConfig(auth.NewInMemoryCredentialStore())
	rs := newResourceServer(t)
	h := newHarness(t, "app-headless", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(WithNonInteractive(t.Context()), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	resp, ok := functionResponse(events, "calendar_list")
	if !ok {
		t.Fatal("no calendar_list response")
	}
	if msg, _ := resp["error"].(string); msg != "connect Google Calendar in chat first" {
		t.Errorf("error = %q, want %q", msg, "connect Google Calendar in chat first")
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list handler ran %d times, want 0", got)
	}
	if hasFunctionCall(events, "adk_request_confirmation") {
		t.Error("run asked for confirmation, want none")
	}
}

func TestPlugin_nonInteractiveHit_runs(t *testing.T) {
	cfg := calendarConfig(auth.NewInMemoryCredentialStore())
	seed(t, cfg, "app-headless", "user-7", &Token{AccessToken: "at-seeded-43", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-headless", "calendar_list", cfg, calendarTool(t, cfg, rs))

	h.run(WithNonInteractive(t.Context()), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	if got := rs.handlerRuns(); got != 1 {
		t.Errorf("calendar_list handler ran %d times, want 1", got)
	}
	if got := rs.authorizations(); len(got) != 1 || got[0] != "Bearer at-seeded-43" {
		t.Errorf("resource server saw %q, want one %q", got, "Bearer at-seeded-43")
	}
}

func TestStoredTokensDisabled_markers(t *testing.T) {
	if StoredTokensDisabled(context.Background()) {
		t.Error("StoredTokensDisabled(Background) = true, want false")
	}
	if !StoredTokensDisabled(WithoutStoredTokens(context.Background())) {
		t.Error("StoredTokensDisabled(WithoutStoredTokens(Background)) = false, want true")
	}
}

// A run marked WithoutStoredTokens, as eval runs are by default, never uses
// the user's token, even when they are connected: an eval of a write tool
// must not act on their real account.
func TestPlugin_storedTokensDisabled_toolNotRunEvenWhenConnected(t *testing.T) {
	cfg := calendarConfig(auth.NewInMemoryCredentialStore())
	seed(t, cfg, "app-headless", "user-7", &Token{AccessToken: "at-real-account", Expiry: time.Now().Add(time.Hour)}, time.Now().Add(24*time.Hour))
	rs := newResourceServer(t)
	h := newHarness(t, "app-headless", "calendar_list", cfg, calendarTool(t, cfg, rs))

	events := h.run(WithoutStoredTokens(WithNonInteractive(t.Context())), "user-7", "s-1", genai.NewContentFromText("what is on my calendar?", genai.RoleUser))

	resp, _ := functionResponse(events, "calendar_list")
	if msg, _ := resp["error"].(string); msg != "Google Calendar is not available in this run" {
		t.Errorf("error = %q, want %q", msg, "Google Calendar is not available in this run")
	}
	if got := rs.handlerRuns(); got != 0 {
		t.Errorf("calendar_list ran %d times, want 0", got)
	}
	if got := rs.authorizations(); len(got) != 0 {
		t.Errorf("resource server saw %q, want no request", got)
	}
}
