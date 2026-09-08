package interrupt

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/session"
)

func TestReasonForSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{
			name:   "no schema asks for input",
			schema: nil,
			want:   ReasonInputRequired,
		},
		{
			name:   "empty schema asks for input",
			schema: map[string]any{},
			want:   ReasonInputRequired,
		},
		{
			name:   "boolean type is a confirmation",
			schema: map[string]any{"type": "boolean"},
			want:   ReasonConfirmation,
		},
		{
			name: "object wrapping a single boolean is a confirmation",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approved": map[string]any{"type": "boolean"},
				},
			},
			want: ReasonConfirmation,
		},
		{
			// Two fields is a form, not a yes/no, even when one is boolean.
			name: "object with a second property asks for input",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approved": map[string]any{"type": "boolean"},
					"note":     map[string]any{"type": "string"},
				},
			},
			want: ReasonInputRequired,
		},
		{
			name: "object wrapping a single non-boolean asks for input",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount": map[string]any{"type": "number"},
				},
			},
			want: ReasonInputRequired,
		},
		{
			name:   "object with no properties asks for input",
			schema: map[string]any{"type": "object"},
			want:   ReasonInputRequired,
		},
		{
			// JSON Schema allows a bare `true` in place of a property schema.
			// It permits any value, so it says nothing about being a yes/no.
			name: "object whose only property is a boolean schema asks for input",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"anything": true},
			},
			want: ReasonInputRequired,
		},
		{
			name:   "string type asks for input",
			schema: map[string]any{"type": "string"},
			want:   ReasonInputRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReasonForSchema(tt.schema); got != tt.want {
				t.Errorf("ReasonForSchema() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyReason(t *testing.T) {
	boolSchema := map[string]any{"type": "boolean"}
	req := session.RequestInput{
		InterruptID:    "i-1",
		Message:        "Ship it?",
		ResponseSchema: &jsonschema.Schema{Type: "boolean"},
	}

	t.Run("nil classifier uses the schema-shape rule", func(t *testing.T) {
		if got := ClassifyReason(req, boolSchema, nil); got != ReasonConfirmation {
			t.Errorf("ClassifyReason() = %q, want %q", got, ReasonConfirmation)
		}
	})

	t.Run("host classifier wins over the schema-shape rule", func(t *testing.T) {
		// The AG-UI taxonomy is open, so a custom reason is passed through
		// verbatim rather than validated against the core reasons.
		classify := func(session.RequestInput) string { return "acme.manager_approval" }
		if got := ClassifyReason(req, boolSchema, classify); got != "acme.manager_approval" {
			t.Errorf("ClassifyReason() = %q, want %q", got, "acme.manager_approval")
		}
	})

	t.Run("empty classifier return falls through to the schema-shape rule", func(t *testing.T) {
		classify := func(session.RequestInput) string { return "" }
		if got := ClassifyReason(req, boolSchema, classify); got != ReasonConfirmation {
			t.Errorf("ClassifyReason() = %q, want %q", got, ReasonConfirmation)
		}
	})

	t.Run("classifier sees the request it is classifying", func(t *testing.T) {
		var seen session.RequestInput
		classify := func(r session.RequestInput) string {
			seen = r
			return ""
		}
		ClassifyReason(req, boolSchema, classify)
		if seen.InterruptID != req.InterruptID || seen.Message != req.Message {
			t.Errorf("classifier saw %+v, want InterruptID=%q Message=%q", seen, req.InterruptID, req.Message)
		}
	})

	t.Run("no schema and no classifier asks for input", func(t *testing.T) {
		if got := ClassifyReason(session.RequestInput{InterruptID: "i-2"}, nil, nil); got != ReasonInputRequired {
			t.Errorf("ClassifyReason() = %q, want %q", got, ReasonInputRequired)
		}
	})
}
