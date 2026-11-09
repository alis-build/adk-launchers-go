package tooloauth

import (
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// NewPlugin returns the runner plugin, named "tooloauth", that checks the
// user's token before each configured tool runs. Add it through
// launcher.Config.PluginConfig. It fails when cfg is invalid.
func NewPlugin(cfg Config) (*plugin.Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	r := newResolver(cfg)
	return plugin.New(plugin.Config{
		Name:               "tooloauth",
		BeforeToolCallback: r.beforeTool,
	})
}

// beforeTool runs before every tool call. Tools the Config does not name run
// untouched, and so does a configured tool whose user has a usable token. In
// a run marked by WithNonInteractive, a tool with no token stops with
// "connect <Provider> in chat first". A store failure stops the call with an
// error the model sees, which names the provider but not the user.
func (r *resolver) beforeTool(ctx agent.Context, t tool.Tool, _ map[string]any) (map[string]any, error) {
	cfgTool, ok := r.cfg.Tools[t.Name()]
	if !ok {
		return nil, nil
	}
	_, err := r.credential(ctx, t.Name(), ctx.AppName(), ctx.UserID())
	switch {
	case err == nil:
		return nil, nil
	case errors.Is(err, errMiss) && IsNonInteractive(ctx):
		// No auth URI here: this text is persisted and fed to the model.
		return nil, fmt.Errorf("connect %s in chat first", cfgTool.Provider)
	case errors.Is(err, errMiss):
		return nil, nil
	default:
		return nil, fmt.Errorf("%s sign-in is unavailable right now", cfgTool.Provider)
	}
}
