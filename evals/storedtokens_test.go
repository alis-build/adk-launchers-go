package evals

import (
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
)

// The evals launcher's runs never use stored OAuth tokens unless the
// embedder opts in with WithStoredOAuthTokens.
func TestLocalEvalService_storedOAuthTokensOptIn(t *testing.T) {
	for name, tc := range map[string]struct {
		opts []Option
		want bool
	}{
		"default": {want: false},
		"opt in":  {opts: []Option{WithStoredOAuthTokens()}, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			a, err := agent.New(agent.Config{Name: "probe-agent"})
			if err != nil {
				t.Fatalf("agent.New: %v", err)
			}
			l := NewLauncher(tc.opts...).(*evalsLauncher)
			l.launcherCfg = &launcher.Config{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService()}

			svc, err := l.localEvalService("probe-agent")
			if err != nil {
				t.Fatalf("localEvalService: %v", err)
			}
			if got := svc.Generator.UseStoredOAuthTokens; got != tc.want {
				t.Errorf("Generator.UseStoredOAuthTokens = %v, want %v", got, tc.want)
			}
		})
	}
}
