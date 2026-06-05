package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

func TestTrailingToolMessages_NoTools(t *testing.T) {
	msgs := []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "hello"},
		{ID: "2", Role: types.RoleAssistant, Content: "hi"},
	}
	if got := trailingToolMessages(msgs); len(got) != 0 {
		t.Errorf("expected 0 trailing tools, got %d", len(got))
	}
}

func TestTrailingToolMessages_TrailingTools(t *testing.T) {
	msgs := []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "call search"},
		{ID: "2", Role: types.RoleAssistant, Content: "", ToolCalls: []types.ToolCall{
			{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "search", Arguments: `{"q":"test"}`}},
		}},
		{ID: "3", Role: types.RoleTool, ToolCallID: "tc1", Content: `{"result":"found"}`},
	}
	got := trailingToolMessages(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 trailing tool, got %d", len(got))
	}
	if got[0].ToolCallID != "tc1" {
		t.Errorf("expected toolCallId=tc1, got %s", got[0].ToolCallID)
	}
}

func TestTrailingToolMessages_HistoricalToolsIgnored(t *testing.T) {
	msgs := []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "call search"},
		{ID: "2", Role: types.RoleAssistant, ToolCalls: []types.ToolCall{
			{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "search"}},
		}},
		{ID: "3", Role: types.RoleTool, ToolCallID: "tc1", Content: `{"result":"found"}`},
		{ID: "4", Role: types.RoleAssistant, Content: "I found it"},
		{ID: "5", Role: types.RoleUser, Content: "thanks"},
	}
	if got := trailingToolMessages(msgs); len(got) != 0 {
		t.Errorf("expected 0 trailing tools (historical tool followed by user msg), got %d", len(got))
	}
}

func TestTrailingToolMessages_MultipleTrailingTools(t *testing.T) {
	msgs := []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "do stuff"},
		{ID: "2", Role: types.RoleTool, ToolCallID: "tc1", Content: `"a"`},
		{ID: "3", Role: types.RoleTool, ToolCallID: "tc2", Content: `"b"`},
	}
	got := trailingToolMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 trailing tools, got %d", len(got))
	}
}

func TestTrailingToolMessages_ToolWithoutIDIgnored(t *testing.T) {
	msgs := []types.Message{
		{ID: "1", Role: types.RoleUser, Content: "hi"},
		{ID: "2", Role: types.RoleTool, Content: "no call id"},
	}
	if got := trailingToolMessages(msgs); len(got) != 0 {
		t.Errorf("expected 0 (tool without callID), got %d", len(got))
	}
}

func TestIsToolResultSubmission(t *testing.T) {
	tests := []struct {
		name string
		msgs []types.Message
		want bool
	}{
		{
			name: "user message only",
			msgs: []types.Message{{Role: types.RoleUser, Content: "hello"}},
			want: false,
		},
		{
			name: "trailing tool message",
			msgs: []types.Message{
				{Role: types.RoleUser, Content: "hello"},
				{Role: types.RoleTool, ToolCallID: "tc1", Content: "result"},
			},
			want: true,
		},
		{
			name: "historical tool then user message",
			msgs: []types.Message{
				{Role: types.RoleTool, ToolCallID: "tc1", Content: "result"},
				{Role: types.RoleUser, Content: "thanks"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isToolResultSubmission(tt.msgs); got != tt.want {
				t.Errorf("isToolResultSubmission() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentToResponseMap(t *testing.T) {
	tests := []struct {
		name    string
		content any
		wantKey string
		wantVal any
	}{
		{
			name:    "nil content",
			content: nil,
		},
		{
			name:    "empty string",
			content: "",
		},
		{
			name:    "JSON string object",
			content: `{"answer":42}`,
			wantKey: "answer",
			wantVal: float64(42),
		},
		{
			name:    "plain string",
			content: "hello world",
			wantKey: "result",
			wantVal: "hello world",
		},
		{
			name:    "map content",
			content: map[string]any{"key": "value"},
			wantKey: "key",
			wantVal: "value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentToResponseMap(tt.content)
			if got == nil {
				t.Fatal("expected non-nil map")
			}
			if tt.wantKey != "" {
				if v, ok := got[tt.wantKey]; !ok || v != tt.wantVal {
					t.Errorf("got[%q] = %v, want %v", tt.wantKey, v, tt.wantVal)
				}
			}
		})
	}
}

func TestIsPendingProxyResponse(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		want     bool
	}{
		{"nil", nil, false},
		{"empty", map[string]any{}, false},
		{"pending", map[string]any{"status": "pending"}, true},
		{"success", map[string]any{"status": "success"}, false},
		{"non-string status", map[string]any{"status": 123}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPendingProxyResponse(tt.response); got != tt.want {
				t.Errorf("isPendingProxyResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}
