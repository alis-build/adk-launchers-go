package agui

import (
	"testing"

	"google.golang.org/adk/v2/session"
)

func TestWithInterruptReasonClassifier(t *testing.T) {
	t.Run("stores the classifier on the config", func(t *testing.T) {
		cfg := &AGUIConfig{}
		WithInterruptReasonClassifier(func(req session.RequestInput) string {
			if req.InterruptID == "i-1" {
				return "acme.manager_approval"
			}
			return ""
		})(cfg)

		if cfg.interruptReasonClassifier == nil {
			t.Fatal("WithInterruptReasonClassifier() left config.interruptReasonClassifier nil")
		}
		if got := cfg.interruptReasonClassifier(session.RequestInput{InterruptID: "i-1"}); got != "acme.manager_approval" {
			t.Errorf("classifier() = %q, want %q", got, "acme.manager_approval")
		}
	})

	t.Run("unset by default so the schema-shape rule applies", func(t *testing.T) {
		// A nil classifier is the signal to fall through to the default rule,
		// so the zero config must not carry a stand-in function.
		if cfg := (&AGUIConfig{}); cfg.interruptReasonClassifier != nil {
			t.Error("AGUIConfig.interruptReasonClassifier is non-nil before any option ran")
		}
	})
}
