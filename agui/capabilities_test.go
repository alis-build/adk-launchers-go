package agui

import "testing"

func TestMergeClientToolCapabilities_SetsClientProvided(t *testing.T) {
	caps := Capabilities{}
	MergeClientToolCapabilities(&caps)

	if caps.Tools == nil {
		t.Fatal("expected Tools to be non-nil")
	}
	if caps.Tools.ClientProvided == nil || !*caps.Tools.ClientProvided {
		t.Error("expected tools.clientProvided to be true")
	}
}

func TestMergeClientToolCapabilities_PreservesExisting(t *testing.T) {
	supported := true
	caps := Capabilities{
		Tools: &ToolsCapabilities{
			Supported: &supported,
		},
	}
	MergeClientToolCapabilities(&caps)

	if caps.Tools.Supported == nil || !*caps.Tools.Supported {
		t.Error("expected tools.supported to remain true")
	}
	if caps.Tools.ClientProvided == nil || !*caps.Tools.ClientProvided {
		t.Error("expected tools.clientProvided to be true")
	}
}

func TestMergeClientToolCapabilities_DoesNotOverrideExplicitFalse(t *testing.T) {
	clientProvided := false
	caps := Capabilities{
		Tools: &ToolsCapabilities{
			ClientProvided: &clientProvided,
		},
	}
	MergeClientToolCapabilities(&caps)

	if *caps.Tools.ClientProvided != false {
		t.Error("expected explicit false to be preserved")
	}
}

func TestWithCapabilities_AutoMergesClientTools(t *testing.T) {
	opt := WithCapabilities(Capabilities{})
	cfg := &AGUIConfig{}
	opt(cfg)

	if cfg.capabilities == nil {
		t.Fatal("expected capabilities to be set")
	}
	if cfg.capabilities.Tools == nil || cfg.capabilities.Tools.ClientProvided == nil {
		t.Error("expected tools.clientProvided to be auto-merged")
	}
}
