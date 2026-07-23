package threadmeta

import "testing"

func TestThreadIDFromResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "resource name", input: "threads/sess-1", want: "sess-1"},
		{name: "resource with spaces", input: "  threads/sess-1  ", want: "sess-1"},
		{name: "bare id", input: "sess-1", want: ""},
		{name: "empty", input: "", want: ""},
		{name: "whitespace", input: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ThreadIDFromResource(tt.input); got != tt.want {
				t.Fatalf("ThreadIDFromResource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestThreadIDFromSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "resource name", input: "threads/sess-1", want: "sess-1"},
		{name: "bare id", input: "sess-1", want: "sess-1"},
		{name: "bare id trimmed", input: "  sess-1  ", want: "sess-1"},
		{name: "empty", input: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ThreadIDFromSession(tt.input); got != tt.want {
				t.Fatalf("ThreadIDFromSession(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
