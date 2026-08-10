package maps

// MergeStringAny shallow-merges base then overlay; overlay keys win on conflict.
// Returns nil when both inputs are empty. Always allocates a new map when either
// input is non-empty so callers cannot mutate shared backing maps.
func MergeStringAny(base, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}
