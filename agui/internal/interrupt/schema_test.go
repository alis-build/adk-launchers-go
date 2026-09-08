package interrupt

import (
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestSchemaToMap(t *testing.T) {
	tests := []struct {
		name   string
		schema *jsonschema.Schema
		want   map[string]any
	}{
		{
			name:   "nil schema omits responseSchema",
			schema: nil,
			want:   nil,
		},
		{
			name:   "boolean schema",
			schema: &jsonschema.Schema{Type: "boolean"},
			want:   map[string]any{"type": "boolean"},
		},
		{
			name: "object schema with required property",
			schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"approved": {Type: "boolean", Description: "Ship it?"},
				},
				Required: []string{"approved"},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approved": map[string]any{
						"type":        "boolean",
						"description": "Ship it?",
					},
				},
				"required": []any{"approved"},
			},
		},
		{
			// Type and Types are mutually exclusive, so this fails to marshal.
			// A bad schema from an agent must not fail the run: omit and move on.
			name:   "malformed schema is omitted rather than fatal",
			schema: &jsonschema.Schema{Type: "object", Types: []string{"object"}},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SchemaToMap(tt.schema)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SchemaToMap() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// jsonschema-go collapses degenerate schemas to JSON booleans on the wire: an
// empty schema marshals as `true` and a reject-all schema as `false`. Neither is
// a JSON object, so both need explicit handling to reach a map[string]any.
func TestSchemaToMapBooleanSchemas(t *testing.T) {
	t.Run("empty schema becomes an empty map, not nil", func(t *testing.T) {
		// An empty-but-present schema still advertises "any JSON accepted"; it
		// must not collapse into the nil case that means "no schema offered".
		got := SchemaToMap(&jsonschema.Schema{})
		if got == nil {
			t.Fatal("SchemaToMap(&jsonschema.Schema{}) = nil, want non-nil empty map")
		}
		if len(got) != 0 {
			t.Errorf("SchemaToMap(&jsonschema.Schema{}) = %#v, want empty map", got)
		}
	})

	t.Run("reject-all schema is omitted", func(t *testing.T) {
		// `false` accepts no input at all, so there is no payload a client
		// could send to resume. Advertising it as an empty map would invert the
		// meaning into accept-anything, so drop it instead.
		if got := SchemaToMap(&jsonschema.Schema{Not: &jsonschema.Schema{}}); got != nil {
			t.Errorf("SchemaToMap(reject-all) = %#v, want nil", got)
		}
	})
}
