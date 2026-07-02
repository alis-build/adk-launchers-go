package agui

import (
	"iter"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"go.alis.build/adk/launchers/internal/adkrun"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

func TestResumeEntriesRunSSEReusesInvocationID(t *testing.T) {
	const (
		appName   = "resume-agent"
		userID    = "user-1"
		sessionID = "sess-1"
		callID    = "confirm-1"
		pauseInv  = "e-pause"
	)
	ctx := t.Context()
	svc := session.InMemoryService()

	a, err := agent.New(agent.Config{
		Name: appName,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = appName
				ev.Content = genai.NewContentFromText("resumed", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rt, err := adkrun.NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: svc,
	}, appName)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: appName, UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	pauseEv := session.NewEvent(ctx, pauseInv)
	pauseEv.Content = &genai.Content{
		Role: string(genai.RoleModel),
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{
				ID:   callID,
				Name: toolconfirmation.FunctionCallName,
				Args: map[string]any{
					"toolConfirmation": map[string]any{"hint": "approve?"},
				},
			},
		}},
	}
	if err := svc.AppendEvent(ctx, createResp.Session, pauseEv); err != nil {
		t.Fatalf("AppendEvent pause: %v", err)
	}

	resumeContent, err := interrupt.EntriesToConfirmationContent([]types.ResumeEntry{{
		InterruptID: callID,
		Status:      types.ResumeStatusResolved,
		Payload:     map[string]any{"approved": true},
	}})
	if err != nil {
		t.Fatalf("EntriesToConfirmationContent: %v", err)
	}

	_, events, err := rt.RunSSE(ctx, adkrun.RunRequest{
		UserID:     userID,
		SessionID:  sessionID,
		NewMessage: *resumeContent,
	})
	if err != nil {
		t.Fatalf("RunSSE resume: %v", err)
	}

	var sawPauseInv bool
	for ev, err := range events {
		if err != nil {
			t.Fatalf("event error: %v", err)
		}
		if ev != nil && ev.InvocationID == pauseInv {
			sawPauseInv = true
		}
	}
	if !sawPauseInv {
		t.Fatalf("expected agent event with invocationId %q after AG-UI resume mapping", pauseInv)
	}
}
