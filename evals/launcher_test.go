package evals

import "testing"

func TestNormalizePathPrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/api", want: "/api"},
		{in: "api", want: "/api"},
		{in: "/api/", want: "/api"},
		{in: "", want: ""},
		{in: " /v1/ ", want: "/v1"},
	}
	for _, tt := range tests {
		if got := normalizePathPrefix(tt.in); got != tt.want {
			t.Errorf("normalizePathPrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDevAppBaseDefaultPrefix(t *testing.T) {
	l := NewLauncher().(*evalsLauncher)
	if got := l.devAppBase(); got != "/api/dev/apps/{app_name}" {
		t.Fatalf("devAppBase() = %q, want /api/dev/apps/{app_name}", got)
	}
}

func TestWebuiAppBaseDefaultPrefix(t *testing.T) {
	l := NewLauncher().(*evalsLauncher)
	if got := l.webuiAppBase(); got != "/api/apps/{app_name}" {
		t.Fatalf("webuiAppBase() = %q, want /api/apps/{app_name}", got)
	}
}

func TestWebuiAppBaseCustomPrefix(t *testing.T) {
	l := NewLauncher(WithPathPrefix("/v1")).(*evalsLauncher)
	if got := l.webuiAppBase(); got != "/v1/apps/{app_name}" {
		t.Fatalf("webuiAppBase() = %q, want /v1/apps/{app_name}", got)
	}
}

func TestParsePathPrefixFlag(t *testing.T) {
	l := NewLauncher().(*evalsLauncher)
	if _, err := l.Parse([]string{"-path_prefix=/v1"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.pathPrefix != "/v1" {
		t.Fatalf("pathPrefix = %q, want /v1", l.pathPrefix)
	}
}
