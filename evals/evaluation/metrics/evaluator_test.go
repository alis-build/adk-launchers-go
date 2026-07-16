package metrics

import "testing"

func TestAvgScore(t *testing.T) {
	score1 := 1.0
	score3 := 3.0
	got := avgScore([]PerInvocationResult{
		{Score: &score1},
		{Score: nil},
		{Score: &score3},
	})
	if got == nil {
		t.Fatal("avgScore() = nil, want 2.0")
	}
	if *got != 2.0 {
		t.Fatalf("avgScore() = %v, want 2.0", *got)
	}
	if avgScore(nil) != nil {
		t.Fatal("avgScore(nil) should be nil")
	}
	if avgScore([]PerInvocationResult{{Score: nil}}) != nil {
		t.Fatal("avgScore with no scored results should be nil")
	}
}
