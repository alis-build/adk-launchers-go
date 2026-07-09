package stream

import (
	"encoding/json"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

func TestTextMessageStartEvent_ToJSON_IncludesName(t *testing.T) {
	evt := NewTextMessageStartEvent("msg-1", "assistant", "sub-agent")

	raw, err := evt.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != string(events.EventTypeTextMessageStart) {
		t.Errorf("type = %v, want TEXT_MESSAGE_START", payload["type"])
	}
	if payload["name"] != "sub-agent" {
		t.Errorf("name = %v, want sub-agent", payload["name"])
	}
}

func TestTextMessageStartEvent_ToJSON_OmitsBlankName(t *testing.T) {
	evt := NewTextMessageStartEvent("msg-1", "assistant", "  ")

	raw, err := evt.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := payload["name"]; ok {
		t.Errorf("name should be omitted for blank author, got %v", payload["name"])
	}
}
