package agui

import (
	"iter"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
)

func testAgent(t *testing.T, name string) agent.Agent {
	t.Helper()
	return testAgentWithDescription(t, name, "")
}

func testAgentWithDescription(t *testing.T, name, description string) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name:        name,
		Description: description,
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("create agent %q: %v", name, err)
	}
	return a
}

func TestResolveAppNameFromContext(t *testing.T) {
	t.Parallel()
	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}
	l := &aguiLauncher{config: &AGUIConfig{appName: "fallback"}}
	cfg := &launcher.Config{AgentLoader: loader}

	name, err := resolveAppName(l, types.RunAgentInput{
		Context: []types.Context{{Description: "app", Value: "other"}},
	}, cfg)
	if err != nil {
		t.Fatalf("resolveAppName() error = %v", err)
	}
	if name != "other" {
		t.Fatalf("resolveAppName() = %q, want other", name)
	}
}

func TestResolveAppNameFromState(t *testing.T) {
	t.Parallel()
	solo := testAgent(t, "solo")
	l := &aguiLauncher{config: &AGUIConfig{appName: "fallback"}}
	cfg := &launcher.Config{AgentLoader: agent.NewSingleLoader(solo)}

	name, err := resolveAppName(l, types.RunAgentInput{
		State: map[string]any{"app_name": "solo"},
	}, cfg)
	if err != nil {
		t.Fatalf("resolveAppName() error = %v", err)
	}
	if name != "solo" {
		t.Fatalf("resolveAppName() = %q, want solo", name)
	}
}

func TestResolveAppNameUnknownAgent(t *testing.T) {
	t.Parallel()
	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}
	l := &aguiLauncher{config: &AGUIConfig{appName: "fallback"}}
	cfg := &launcher.Config{AgentLoader: loader}

	_, err = resolveAppName(l, types.RunAgentInput{
		Context: []types.Context{{Description: "app", Value: "missing"}},
	}, cfg)
	if err == nil {
		t.Fatal("resolveAppName() expected error for unknown agent")
	}
}

func TestResolveAppNameDefaultsToRoot(t *testing.T) {
	t.Parallel()
	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}
	l := &aguiLauncher{config: &AGUIConfig{appName: "fallback"}}
	cfg := &launcher.Config{AgentLoader: loader}

	name, err := resolveAppName(l, types.RunAgentInput{}, cfg)
	if err != nil {
		t.Fatalf("resolveAppName() error = %v", err)
	}
	if name != "root" {
		t.Fatalf("resolveAppName() = %q, want root", name)
	}
}

func TestResolveAppNameFromQuery(t *testing.T) {
	t.Parallel()
	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}
	l := &aguiLauncher{config: &AGUIConfig{appName: "fallback"}}
	cfg := &launcher.Config{AgentLoader: loader}

	name, err := resolveAppNameFromSources(l, cfg, nil, nil, "other")
	if err != nil {
		t.Fatalf("resolveAppNameFromSources() error = %v", err)
	}
	if name != "other" {
		t.Fatalf("resolveAppNameFromSources() = %q, want other", name)
	}
}

func TestResolveAppNameFromSourcesResolverUsesAgentIDQuery(t *testing.T) {
	t.Parallel()
	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}
	l := &aguiLauncher{config: &AGUIConfig{
		appName: "fallback",
		appNameResolver: func(req types.RunAgentInput) string {
			return appNameFromContext(req.Context)
		},
	}}
	cfg := &launcher.Config{AgentLoader: loader}

	name, err := resolveAppNameFromSources(l, cfg, nil, nil, "other")
	if err != nil {
		t.Fatalf("resolveAppNameFromSources() error = %v", err)
	}
	if name != "other" {
		t.Fatalf("resolveAppNameFromSources() = %q, want other", name)
	}
}

func TestAgentDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("uses agent description", func(t *testing.T) {
		t.Parallel()
		ag := testAgentWithDescription(t, "weather.agent", "Weather Assistant")
		cfg := &launcher.Config{AgentLoader: agent.NewSingleLoader(ag)}
		if got := agentDisplayName(cfg, "Launcher Default", "weather.agent"); got != "Weather Assistant" {
			t.Fatalf("agentDisplayName() = %q, want Weather Assistant", got)
		}
	})

	t.Run("single agent falls back to launcher default", func(t *testing.T) {
		t.Parallel()
		ag := testAgent(t, "solo")
		cfg := &launcher.Config{AgentLoader: agent.NewSingleLoader(ag)}
		if got := agentDisplayName(cfg, "My Friendly Agent", "solo"); got != "My Friendly Agent" {
			t.Fatalf("agentDisplayName() = %q, want My Friendly Agent", got)
		}
	})

	t.Run("multi agent without description uses id", func(t *testing.T) {
		t.Parallel()
		root := testAgent(t, "root")
		other := testAgent(t, "other")
		loader, err := agent.NewMultiLoader(root, other)
		if err != nil {
			t.Fatalf("NewMultiLoader: %v", err)
		}
		cfg := &launcher.Config{AgentLoader: loader}
		if got := agentDisplayName(cfg, "Launcher Default", "other"); got != "other" {
			t.Fatalf("agentDisplayName() = %q, want other", got)
		}
	})
}
