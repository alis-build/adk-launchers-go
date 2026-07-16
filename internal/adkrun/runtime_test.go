package adkrun

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// testRuntime returns a Runtime with a minimal no-op agent backed by in-memory sessions.
func testRuntime(t *testing.T) *Runtime {
	t.Helper()

	a, err := agent.New(agent.Config{
		Name:        "test-agent",
		Description: "no-op agent for unit tests",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("create test agent: %v", err)
	}

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "test-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

func TestNewRuntime_validation(t *testing.T) {
	t.Parallel()

	a, _ := agent.New(agent.Config{Name: "a", Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {}
	}})

	tests := []struct {
		name   string
		config *launcher.Config
		app    string
	}{
		{"nil config", nil, "app"},
		{"empty app name", &launcher.Config{}, ""},
		{"nil AgentLoader", &launcher.Config{AgentLoader: nil, SessionService: nil}, "app"},
		{"nil SessionService", &launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: nil}, "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuntime(tt.config, tt.app)
			if err == nil {
				t.Fatalf("NewRuntime() expected error for %s", tt.name)
			}
		})
	}
}

func TestNewRuntime_appNameTrimmed(t *testing.T) {
	t.Parallel()

	a, _ := agent.New(agent.Config{Name: "app", Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {}
	}})
	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "  app  ")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if rt.AppName() != "app" {
		t.Fatalf("AppName() = %q, want %q", rt.AppName(), "app")
	}
}

func TestRunRequestMarshal(t *testing.T) {
	req := RunRequest{
		AppName:   "my.agent",
		UserID:    "user-1",
		SessionID: "session-1",
		NewMessage: Content{
			Role: "user",
			Parts: []*Part{
				genai.NewPartFromText("hello"),
				genai.NewPartFromFunctionResponse("lookup", map[string]any{"id": 42}),
			},
		},
		Streaming:                 true,
		SaveInputBlobsAsArtifacts: true,
		StateDelta:                map[string]any{"key": "value"},
		FunctionCallEventID:       "evt-1",
		InvocationID:              "inv-1",
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{
		"appName", "userId", "sessionId", "newMessage", "streaming",
		"saveInputBlobsAsArtifacts", "stateDelta", "functionCallEventId", "invocationId",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing top-level field %q in %s", key, string(body))
		}
	}

	newMessage, ok := got["newMessage"].(map[string]any)
	if !ok {
		t.Fatalf("newMessage type = %T", got["newMessage"])
	}
	parts, ok := newMessage["parts"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("parts = %#v", newMessage["parts"])
	}
	first, ok := parts[0].(map[string]any)
	if !ok || first["text"] != "hello" {
		t.Fatalf("first part = %#v", parts[0])
	}
	second, ok := parts[1].(map[string]any)
	if !ok {
		t.Fatalf("second part type = %T", parts[1])
	}
	if _, ok := second["functionResponse"]; !ok {
		t.Fatalf("second part missing functionResponse: %#v", second)
	}
}

func TestUserTextMessage(t *testing.T) {
	msg := UserTextMessage("ping")
	if msg.Role != string(genai.RoleUser) {
		t.Fatalf("role = %q", msg.Role)
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text != "ping" {
		t.Fatalf("parts = %#v", msg.Parts)
	}
}

func TestRunSSE_validation(t *testing.T) {
	t.Parallel()

	rt := testRuntime(t)

	_, events, err := rt.RunSSE(t.Context(), RunRequest{})
	if err == nil {
		t.Fatal("expected validation error for empty UserID")
	}
	if events != nil {
		t.Fatal("expected nil events iterator on validation error")
	}

	_, _, err = rt.RunSSE(t.Context(), RunRequest{UserID: "user"})
	if err == nil {
		t.Fatal("expected validation error for empty Parts")
	}
}

func TestRunSSE_happyPath(t *testing.T) {
	rt := testRuntime(t)

	sessionID, events, err := rt.RunSSE(t.Context(), RunRequest{
		UserID:     "user-1",
		NewMessage: UserTextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("RunSSE: %v", err)
	}
	if sessionID == "" {
		t.Fatal("expected non-empty sessionID")
	}

	var count int
	for _, err := range events {
		if err != nil {
			t.Fatalf("event error: %v", err)
		}
		count++
	}
	// No-op agent produces no events; verify iteration completed without error.
	_ = count
}

func TestRunSSE_sessionIDPreserved(t *testing.T) {
	rt := testRuntime(t)

	sessionID, events, err := rt.RunSSE(t.Context(), RunRequest{
		UserID:     "user-1",
		SessionID:  "custom-session",
		NewMessage: UserTextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("RunSSE: %v", err)
	}
	if sessionID != "custom-session" {
		t.Fatalf("sessionID = %q, want custom-session", sessionID)
	}
	for _, err := range events {
		if err != nil {
			t.Fatalf("event error: %v", err)
		}
	}
}

func TestRunSSE_partsNotMutated(t *testing.T) {
	rt := testRuntime(t)

	originalPart := genai.NewPartFromText("original")
	req := RunRequest{
		UserID:     "user-1",
		NewMessage: Content{Role: "user", Parts: []*Part{originalPart}},
	}

	_, events, err := rt.RunSSE(t.Context(), req)
	if err != nil {
		t.Fatalf("RunSSE: %v", err)
	}
	for _, err := range events {
		if err != nil {
			t.Fatalf("event error: %v", err)
		}
	}

	if req.NewMessage.Parts[0] != originalPart {
		t.Fatal("caller's Parts slice was mutated")
	}
	if req.NewMessage.Parts[0].Text != "original" {
		t.Fatalf("original part text = %q", req.NewMessage.Parts[0].Text)
	}
}

func TestRunSSE_resumeReusesInvocationID(t *testing.T) {
	// adk v1.5.0's runner.Run unconditionally allocates a fresh e-<uuid> invocation, so
	// there is no way to have it reuse a paused invocation id from session history yet.
	// See the TODO(adk-invocation-resume) note in runtime.go. Re-enable this test once
	// google.golang.org/adk/runner exposes invocation resume (e.g. runner.WithInvocationID).
	t.Skip("adk v1.5.0 runner does not yet support invocation resume; see TODO(adk-invocation-resume)")
}

func TestRunSSE_resumeMismatchMintsNewInvocationID(t *testing.T) {
	const (
		callID   = "confirm-1"
		pauseInv = "e-pause"
	)
	rt, svc, ctx := newResumeTestRuntime(t)
	seedConfirmationPause(t, svc, ctx, rt.AppName(), "user-1", "sess-1", callID, pauseInv)

	resumeMsg := confirmationResumeMessage("wrong-id", map[string]any{"confirmed": true})
	invIDs := drainInvocationIDs(t, rt, ctx, "user-1", "sess-1", resumeMsg)

	if len(invIDs) == 0 {
		t.Fatal("expected at least one event with an invocation id")
	}
	if invIDs[pauseInv] {
		t.Fatalf("expected a fresh invocation id, reused paused id %q", pauseInv)
	}
	for id := range invIDs {
		if id == "" {
			t.Fatal("expected non-empty invocation id on agent events")
		}
	}
}

func newResumeTestRuntime(t *testing.T) (*Runtime, session.Service, context.Context) {
	t.Helper()
	ctx := t.Context()
	svc := session.InMemoryService()
	appName := "resume-agent"

	a, err := agent.New(agent.Config{
		Name: appName,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				got := ctx.InvocationID()
				if got == "" {
					t.Error("agent InvocationID() is empty")
				}
				ev := session.NewEvent(got)
				ev.Author = appName
				ev.Content = genai.NewContentFromText("resumed", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: svc,
	}, appName)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt, svc, ctx
}

func seedConfirmationPause(t *testing.T, svc session.Service, ctx context.Context, appName, userID, sessionID, callID, pauseInv string) {
	t.Helper()
	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: appName, UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	pauseEv := session.NewEvent(pauseInv)
	pauseEv.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				ID:   callID,
				Name: "adk_request_confirmation",
				Args: map[string]any{
					"toolConfirmation": map[string]any{"hint": "approve?"},
				},
			},
		}},
	}
	if err := svc.AppendEvent(ctx, createResp.Session, pauseEv); err != nil {
		t.Fatalf("AppendEvent pause: %v", err)
	}
}

func confirmationResumeMessage(callID string, response map[string]any) Content {
	return Content{
		Role: string(genai.RoleUser),
		Parts: []*Part{{
			FunctionResponse: &genai.FunctionResponse{
				ID:       callID,
				Name:     "adk_request_confirmation",
				Response: response,
			},
		}},
	}
}

func drainInvocationIDs(t *testing.T, rt *Runtime, ctx context.Context, userID, sessionID string, msg Content) map[string]bool {
	t.Helper()
	gotSessionID, events, err := rt.RunSSE(ctx, RunRequest{
		UserID:     userID,
		SessionID:  sessionID,
		NewMessage: msg,
	})
	if err != nil {
		t.Fatalf("RunSSE resume: %v", err)
	}
	if gotSessionID != sessionID {
		t.Fatalf("sessionID = %q, want %q", gotSessionID, sessionID)
	}

	invIDs := make(map[string]bool)
	for ev, err := range events {
		if err != nil {
			t.Fatalf("event error: %v", err)
		}
		if ev == nil || ev.InvocationID == "" {
			continue
		}
		invIDs[ev.InvocationID] = true
	}
	return invIDs
}

func TestRunSSE_midStreamError(t *testing.T) {
	// Create an agent that yields an error mid-stream, then confirm RunSSE
	// surfaces the failure through the event iterator (runtimeAdapter, which
	// wraps RunSSE, relies on this behavior in the scheduler).
	a, err := agent.New(agent.Config{
		Name: "err-agent",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(nil, fmt.Errorf("mid-stream failure"))
			}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}, "err-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	_, events, err := rt.RunSSE(t.Context(), RunRequest{
		UserID:     "user-1",
		NewMessage: UserTextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("RunSSE setup: %v", err)
	}
	var seenErr error
	for _, evErr := range events {
		if evErr != nil {
			seenErr = evErr
			break
		}
	}
	if seenErr == nil {
		t.Fatal("expected mid-stream error from event iterator")
	}
}
