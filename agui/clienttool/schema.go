package clienttool

// cleanSchemaForGenAI recursively sanitises an AG-UI JSON Schema value for
// use with genai.FunctionDeclaration.ParametersJsonSchema.
//
// Three transformations are applied (mirroring the Python middleware's
// _clean_schema_for_genai):
//
//  1. Strip $-prefixed keys ($schema, $id, $ref, $defs, $comment) — these are
//     JSON Schema infrastructure that genai does not accept.
//  2. Map "examples" → "example" (first element) and "const" → "enum"
//     (single-value list), preserving useful context in genai-accepted form.
//  3. Pass all remaining keys through — ParametersJsonSchema accepts any
//     JSON-serialisable value, and Gemini's schema support is broad enough
//     that filtering to a strict allowlist would be overly conservative.
func cleanSchemaForGenAI(schema any) any {
	if schema == nil {
		return nil
	}

	switch v := schema.(type) {
	case map[string]any:
		return cleanSchemaMap(v)
	case []any:
		out := make([]any, len(v))
		for i, elem := range v {
			out[i] = cleanSchemaForGenAI(elem)
		}
		return out
	default:
		return schema
	}
}

func cleanSchemaMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if len(k) > 0 && k[0] == '$' {
			continue
		}

		if k == "examples" {
			if arr, ok := v.([]any); ok && len(arr) > 0 {
				out["example"] = arr[0]
			}
			continue
		}

		if k == "const" {
			out["enum"] = []any{v}
			continue
		}

		out[k] = cleanSchemaForGenAI(v)
	}
	return out
}
