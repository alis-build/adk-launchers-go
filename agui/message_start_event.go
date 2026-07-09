package agui

import (
	"encoding/json"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// TextMessageStartEvent is an emit-only shim matching the upstream AG-UI wire
// shape until the Go SDK adds optional Name on events.TextMessageStartEvent.
// Delete this type and use events.NewTextMessageStartEvent(..., events.WithName(...))
// after upstream lands.
//
// Type-assertion note: [CallInterceptor.OnEmit] implementations that inspect
// TEXT_MESSAGE_START will receive *agui.TextMessageStartEvent, not the upstream
// *events.TextMessageStartEvent. Assert on this shim (or on the events.Event
// interface + event.Type()) to observe author names on the live SSE path.
type TextMessageStartEvent struct {
	*events.TextMessageStartEvent
	// Name is the optional sender name that ends up on the wire as JSON "name".
	// The MarshalJSON / ToJSON hooks preserve the tag; do not rely on default
	// struct marshalling for the embedded upstream type.
	Name string `json:"name,omitempty"`
}

// newTextMessageStartEvent creates a TEXT_MESSAGE_START event with an optional
// sender name. Blank / whitespace-only names are dropped so the wire JSON omits
// the field via omitempty.
func newTextMessageStartEvent(messageID, role, name string) *TextMessageStartEvent {
	opts := []events.TextMessageStartOption{events.WithRole(role)}
	return &TextMessageStartEvent{
		TextMessageStartEvent: events.NewTextMessageStartEvent(messageID, opts...),
		Name:                  messageSenderName(name),
	}
}

func (e *TextMessageStartEvent) Type() events.EventType {
	return e.TextMessageStartEvent.Type()
}

func (e *TextMessageStartEvent) Timestamp() *int64 {
	return e.TextMessageStartEvent.Timestamp()
}

func (e *TextMessageStartEvent) SetTimestamp(timestamp int64) {
	e.TextMessageStartEvent.SetTimestamp(timestamp)
}

func (e *TextMessageStartEvent) ThreadID() string {
	return e.TextMessageStartEvent.ThreadID()
}

func (e *TextMessageStartEvent) RunID() string {
	return e.TextMessageStartEvent.RunID()
}

func (e *TextMessageStartEvent) Validate() error {
	return e.TextMessageStartEvent.Validate()
}

func (e *TextMessageStartEvent) GetBaseEvent() *events.BaseEvent {
	return e.TextMessageStartEvent.GetBaseEvent()
}

// MarshalJSON produces the same wire shape as ToJSON so that any caller that
// json.Marshal's this event (loggers, snapshot serializers, tests capturing
// events) sees the lowercase "name" field instead of the promoted Go field name.
func (e *TextMessageStartEvent) MarshalJSON() ([]byte, error) {
	return e.ToJSON()
}

// ToJSON serializes to the AG-UI TEXT_MESSAGE_START wire shape, including the
// optional "name" field consumed by JS/Python/.NET/Dart clients.
func (e *TextMessageStartEvent) ToJSON() ([]byte, error) {
	payload := map[string]any{
		"type":      events.EventTypeTextMessageStart,
		"messageId": e.TextMessageStartEvent.MessageID,
	}
	if e.TextMessageStartEvent.Role != nil {
		payload["role"] = *e.TextMessageStartEvent.Role
	}
	if e.Name != "" {
		payload["name"] = e.Name
	}
	if ts := e.TextMessageStartEvent.Timestamp(); ts != nil {
		payload["timestamp"] = *ts
	}
	return json.Marshal(payload)
}

var _ events.Event = (*TextMessageStartEvent)(nil)
var _ json.Marshaler = (*TextMessageStartEvent)(nil)
