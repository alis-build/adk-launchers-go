package agui

import (
	"encoding/json"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

func TestTextMessageStartEvent_ToJSON_IncludesName(t *testing.T) {
	ev := newTextMessageStartEvent("msg-1", "assistant", "sub-agent-a")

	data, err := ev.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["type"] != string(events.EventTypeTextMessageStart) {
		t.Errorf("type = %v, want %v", raw["type"], events.EventTypeTextMessageStart)
	}
	if raw["messageId"] != "msg-1" {
		t.Errorf("messageId = %v, want msg-1", raw["messageId"])
	}
	if raw["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", raw["role"])
	}
	if raw["name"] != "sub-agent-a" {
		t.Errorf("name = %v, want sub-agent-a", raw["name"])
	}
}

func TestTextMessageStartEvent_ToJSON_OmitsBlankName(t *testing.T) {
	ev := newTextMessageStartEvent("msg-1", "assistant", "   ")

	data, err := ev.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["name"]; ok {
		t.Errorf("name key present for blank author, want omitted: %v", raw)
	}
}

func TestTextMessageStartEvent_MarshalJSON_MatchesToJSON(t *testing.T) {
	ev := newTextMessageStartEvent("msg-1", "assistant", "sub-agent-a")

	viaMarshal, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	viaToJSON, err := ev.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON error = %v", err)
	}
	if string(viaMarshal) != string(viaToJSON) {
		t.Errorf("json.Marshal = %s, ToJSON = %s", viaMarshal, viaToJSON)
	}
}
