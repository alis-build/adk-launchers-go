package interrupt

import "google.golang.org/adk/v2/session"

// ReasonClassifier maps a workflow input request to an AG-UI interrupt reason.
// Returning "" defers to the default schema-shape rule.
type ReasonClassifier func(req session.RequestInput) string

// ClassifyReason picks the AG-UI reason for a workflow input request. A host
// classifier runs first and its non-empty result is used verbatim, since the
// AG-UI reason taxonomy is open to custom namespaced reasons.
//
// responseSchema is req.ResponseSchema already converted by [SchemaToMap]; it
// is passed in rather than re-derived because the caller needs the same map to
// build the interrupt.
func ClassifyReason(req session.RequestInput, responseSchema map[string]any, classify ReasonClassifier) string {
	if classify != nil {
		if reason := classify(req); reason != "" {
			return reason
		}
	}
	return ReasonForSchema(responseSchema)
}

// ReasonForSchema applies the default rule: a request whose response is a bare
// yes/no is a confirmation, and everything else asks for input.
func ReasonForSchema(schema map[string]any) string {
	if isBooleanShaped(schema) {
		return ReasonConfirmation
	}
	return ReasonInputRequired
}

// isBooleanShaped reports whether schema describes a single yes/no answer:
// a boolean directly, or an object wrapping exactly one boolean property.
//
// The single-property limit is what separates a confirmation from a form. An
// object carrying a boolean plus anything else (a reason field, an amount) needs
// a real input UI, so it stays input_required.
func isBooleanShaped(schema map[string]any) bool {
	if typeName, _ := schema["type"].(string); typeName == "boolean" {
		return true
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) != 1 {
		return false
	}
	var only any
	for _, prop := range props {
		only = prop
	}
	// JSON Schema allows a bare `true`/`false` in place of a property schema.
	// Neither is an object, and neither pins the property to a boolean.
	propMap, ok := only.(map[string]any)
	if !ok {
		return false
	}
	typeName, _ := propMap["type"].(string)
	return typeName == "boolean"
}
