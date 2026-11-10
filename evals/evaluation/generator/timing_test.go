package generator

import (
	"math"
	"testing"
)

// Mirrors the scenarios of adk-python test_instrumentation.py (:2702-2770)
// with one-second slices.
func TestUnionSeconds(t *testing.T) {
	tests := []struct {
		name                string
		model, tool         []interval
		wantModel, wantTool float64
	}{
		{"nothing (:2702)", nil, nil, 0, 0},
		{"back-to-back tools (:2713)", nil, []interval{{0, 1}, {1, 2}}, 0, 2},
		{"concurrent tools (:2722)", nil, []interval{{0, 1}, {0, 1}}, 0, 1},
		{"model inside a tool (:2731)", []interval{{0, 1}}, []interval{{0, 2}}, 1, 1},
		{"streamed chunks (:2770)", []interval{{0, 1}, {1, 2}, {2, 3}}, nil, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := busyIntervals{model: tt.model, tool: tt.tool}
			if got := b.modelSeconds(); math.Abs(got-tt.wantModel) > 1e-9 {
				t.Errorf("model = %v, want %v", got, tt.wantModel)
			}
			if got := b.toolSeconds(); math.Abs(got-tt.wantTool) > 1e-9 {
				t.Errorf("tool = %v, want %v", got, tt.wantTool)
			}
		})
	}
	if busy := unionSeconds([]interval{{0, 1}, {0, 2}}); busy != 2 {
		t.Errorf("busy = %v, want 2", busy)
	}
}
