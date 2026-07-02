package agui

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/cmd/launcher"
)

// AppNameResolver extracts an ADK app name from a RunAgentInput. Return an empty
// string to fall through to state, context, and default resolution.
type AppNameResolver func(req types.RunAgentInput) string

// WithAppNameResolver registers a custom app name extractor. It runs before
// state and context lookup and after the resolver returns a non-empty value
// that value is validated against AgentLoader.ListAgents when available.
func WithAppNameResolver(resolver AppNameResolver) Option {
	return func(c *AGUIConfig) {
		c.appNameResolver = resolver
	}
}

// resolveAppName picks the ADK app name for a /run_sse request.
//
// Resolution order:
//  1. WithAppNameResolver when it returns a non-empty string
//  2. req.State app_name or appName
//  3. req.Context entry with description "app"
//  4. AgentLoader.RootAgent().Name()
//  5. launcher default from NewLauncher(appName)
//
// Names from steps 1–3 are validated against AgentLoader.ListAgents when set.
func resolveAppName(l *aguiLauncher, req types.RunAgentInput, cfg *launcher.Config) (string, error) {
	if l.config.appNameResolver != nil {
		if name := strings.TrimSpace(l.config.appNameResolver(req)); name != "" {
			return validateAppName(cfg, name)
		}
	}
	var state map[string]any
	if m, ok := req.State.(map[string]any); ok {
		state = m
	}
	return resolveAppNameFallback(l, cfg, state, req.Context, "")
}

// resolveAppNameFromSources resolves an app name for handlers that do not have a
// full RunAgentInput (for example GET thread messages or POST /agents/state).
// Pass explicitAgentID from the agentId query/body parameter when present. The
// resolver (if configured) is called with a partial RunAgentInput; when
// explicitAgentID is set it is also injected as context description "app" so
// resolvers that only inspect RunAgentInput.context behave like browser clients.
func resolveAppNameFromSources(l *aguiLauncher, cfg *launcher.Config, state map[string]any, context []types.Context, explicitAgentID string) (string, error) {
	context = contextForAppNameResolution(context, explicitAgentID)
	if l.config.appNameResolver != nil {
		partial := types.RunAgentInput{Context: context}
		if state != nil {
			partial.State = any(state)
		}
		if name := strings.TrimSpace(l.config.appNameResolver(partial)); name != "" {
			return validateAppName(cfg, name)
		}
	}
	return resolveAppNameFallback(l, cfg, state, context, explicitAgentID)
}

// contextForAppNameResolution mirrors browser clients that pass the selected agent
// as RunAgentInput.context {description:"app", value: agentId} on GET handlers.
func contextForAppNameResolution(context []types.Context, explicitAgentID string) []types.Context {
	explicitAgentID = strings.TrimSpace(explicitAgentID)
	if explicitAgentID == "" || appNameFromContext(context) != "" {
		return context
	}
	appCtx := types.Context{Description: "app", Value: explicitAgentID}
	if len(context) == 0 {
		return []types.Context{appCtx}
	}
	out := make([]types.Context, 0, len(context)+1)
	out = append(out, appCtx)
	out = append(out, context...)
	return out
}

// agentDisplayName returns a human-friendly label for thread metadata.
// Prefers the loaded agent's Description(); for a single-agent host falls back
// to the NewLauncher default when Description is empty.
func agentDisplayName(cfg *launcher.Config, launcherDefault, agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return strings.TrimSpace(launcherDefault)
	}
	if cfg != nil && cfg.AgentLoader != nil {
		if ag, err := cfg.AgentLoader.LoadAgent(agentID); err == nil && ag != nil {
			if desc := strings.TrimSpace(ag.Description()); desc != "" {
				return desc
			}
		}
		agents := cfg.AgentLoader.ListAgents()
		if len(agents) == 1 && agents[0] == agentID {
			if name := strings.TrimSpace(launcherDefault); name != "" {
				return name
			}
		}
	}
	return agentID
}

// resolveAppNameFallback is the shared fallback chain after the custom resolver
// has been consulted (or is unconfigured).
func resolveAppNameFallback(l *aguiLauncher, cfg *launcher.Config, state map[string]any, context []types.Context, explicitAgentID string) (string, error) {
	if explicitAgentID != "" {
		return validateAppName(cfg, strings.TrimSpace(explicitAgentID))
	}
	if name := appNameFromState(state); name != "" {
		return validateAppName(cfg, name)
	}
	if name := appNameFromContext(context); name != "" {
		return validateAppName(cfg, name)
	}
	if cfg != nil && cfg.AgentLoader != nil {
		return cfg.AgentLoader.RootAgent().Name(), nil
	}
	if l.config.appName != "" {
		return l.config.appName, nil
	}
	return "", fmt.Errorf("app name is required")
}

func appNameFromState(state map[string]any) string {
	if state == nil {
		return ""
	}
	for _, key := range []string{"app_name", "appName"} {
		if v, ok := state[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func appNameFromContext(context []types.Context) string {
	for _, c := range context {
		if c.Description == "app" {
			return strings.TrimSpace(c.Value)
		}
	}
	return ""
}

func validateAppName(cfg *launcher.Config, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("app name is required")
	}
	if cfg == nil || cfg.AgentLoader == nil {
		return name, nil
	}
	agents := cfg.AgentLoader.ListAgents()
	if len(agents) == 0 {
		return name, nil
	}
	if !slices.Contains(agents, name) {
		log.Printf("agui: unknown agent %q (available: %v)", name, agents)
		return "", fmt.Errorf("unknown agent %q", name)
	}
	return name, nil
}
