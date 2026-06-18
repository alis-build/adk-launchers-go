package agui

import (
	"context"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

func TestResolveInvocationIDForResume(t *testing.T) {
	pending := []interrupt.Record{{ID: "confirm-1", InvocationID: "e-pending"}}
	entries := []types.ResumeEntry{{InterruptID: "confirm-1", Status: types.ResumeStatusResolved}}

	t.Run("prefers pending over client state", func(t *testing.T) {
		client := map[string]any{"adk": map[string]any{"invocationId": "e-client"}}
		got := resolveInvocationIDForResume(pending, entries, client, nil)
		if got != "e-pending" {
			t.Errorf("got %q, want e-pending", got)
		}
	})

	t.Run("client state when pending missing id", func(t *testing.T) {
		got := resolveInvocationIDForResume(nil, entries, map[string]any{
			"adk": map[string]any{"invocationId": "e-client"},
		}, nil)
		if got != "e-client" {
			t.Errorf("got %q, want e-client", got)
		}
	})

	t.Run("session events fallback", func(t *testing.T) {
		svc := session.InMemoryService()
		ctx := context.Background()
		createResp, err := svc.Create(ctx, &session.CreateRequest{
			AppName: "test-app", UserID: "u1", SessionID: "t1",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ev := session.NewEvent("inv0")
		ev.InvocationID = "e-from-session"
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{
					ID:   "confirm-1",
					Name: toolconfirmation.FunctionCallName,
				},
			}},
		}
		if err := svc.AppendEvent(ctx, createResp.Session, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		got := resolveInvocationIDForResume(nil, entries, nil, createResp.Session)
		if got != "e-from-session" {
			t.Errorf("got %q, want e-from-session", got)
		}
	})
}
