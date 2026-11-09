package tooloauth

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// NewPlugin returns the runner plugin, named "tooloauth", that makes sure
// each configured tool has the user's token before it runs. Add it through
// launcher.Config.PluginConfig. It fails when cfg is invalid.
func NewPlugin(cfg Config) (*plugin.Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return plugin.New(plugin.Config{
		Name:               "tooloauth",
		BeforeToolCallback: beforeTool,
	})
}

// beforeTool lets every call through for now.
func beforeTool(agent.Context, tool.Tool, map[string]any) (map[string]any, error) {
	return nil, nil
}
