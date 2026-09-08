package interrupt

import (
	"encoding/json"
	"log"

	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaToMap converts an ADK response schema into the plain JSON object AG-UI
// carries on an interrupt's responseSchema, by JSON round-trip. It returns nil
// when there is no schema to advertise.
//
// Every failure path omits the schema rather than propagating an error. A
// dropped schema costs the client its form hints; a failed conversion that
// aborted the run would cost the user their turn.
func SchemaToMap(schema *jsonschema.Schema) map[string]any {
	if schema == nil {
		return nil
	}

	data, err := json.Marshal(schema)
	if err != nil {
		// Reachable: Schema rejects mutually exclusive field pairs (Type with
		// Types, Items with ItemsArray) only at marshal time, not at build time.
		log.Printf("agui: interrupt responseSchema omitted: schema failed to marshal: %v", err)
		return nil
	}

	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		return obj
	}

	// jsonschema-go collapses degenerate schemas to JSON booleans: an empty
	// schema marshals as `true`, a reject-all schema as `false`. Only `true` has
	// a map form, the empty object that accepts anything. `false` leaves the
	// interrupt unanswerable, and an empty map would invert it into
	// accept-anything, so it is dropped with everything else non-object.
	var alwaysValid bool
	if err := json.Unmarshal(data, &alwaysValid); err == nil && alwaysValid {
		return map[string]any{}
	}

	log.Printf("agui: interrupt responseSchema omitted: schema %s is not an object", data)
	return nil
}
