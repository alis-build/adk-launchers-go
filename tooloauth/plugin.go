package tooloauth

import (
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolauth"
)

// NewPlugin returns the runner plugin, named "tooloauth", that gives each
// configured tool the user's token: it serves a stored one, refreshing it
// when expired; asks the user to sign in, through ADK's
// adk_request_credential call, when a tool has none; and completes the
// sign-in when ADK reruns the call after the user answers, so the tool runs
// in that same turn. When a run answering a credential request starts, it
// reads the run's session for the sign-in requests it made. Add it through
// launcher.Config.PluginConfig. It fails when cfg is invalid.
func NewPlugin(cfg Config) (*plugin.Plugin, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	r := newResolver(cfg)
	return plugin.New(plugin.Config{
		Name:               "tooloauth",
		BeforeRunCallback:  r.beforeRun,
		BeforeToolCallback: r.beforeTool,
	})
}

// beforeTool runs before every tool call. Tools the Config does not name run
// untouched. In a run marked WithoutStoredTokens every configured tool stops
// with "<Provider> is not available in this run", before any lookup or
// sign-in. A call ADK reruns after the user answered a sign-in request
// completes that sign-in and runs. Otherwise a configured tool whose user
// has a usable token runs, and a tool with no token asks the user to sign
// in; in a run marked by WithNonInteractive it stops with "connect
// <Provider> in chat first" instead. A store failure stops the call with an
// error the model sees, which names the provider but not the user.
func (r *resolver) beforeTool(ctx agent.Context, t tool.Tool, _ map[string]any) (map[string]any, error) {
	cfgTool, ok := r.cfg.Tools[t.Name()]
	if !ok {
		return nil, nil
	}
	if StoredTokensDisabled(ctx) {
		return nil, notAvailable(cfgTool)
	}
	// Sign-ins complete only through ADK's credential requests. A sign-in
	// answer sent as a confirmation, as clients of the plugin's earlier
	// versions sent it, never completes and must never pass as the tool's
	// own approval, which any confirmed confirmation would.
	if conf := ctx.ToolConfirmation(); conf != nil {
		if _, isSignIn := decodeAuthResponse(conf.Payload); isSignIn {
			return nil, fmt.Errorf("%s sign-in failed: the sign-in request is out of date", cfgTool.Provider)
		}
	}
	if result, handled, err := r.resumeSignIn(ctx, t.Name(), cfgTool); handled {
		return result, err
	}
	_, err := r.credential(ctx, t.Name(), ctx.AppName(), ctx.UserID())
	switch {
	case err == nil:
		return nil, nil
	case errors.Is(err, errMiss) && IsNonInteractive(ctx):
		// No auth URI here: this text is persisted and fed to the model.
		return nil, fmt.Errorf("connect %s in chat first", cfgTool.Provider)
	case errors.Is(err, errMiss):
		return r.pauseForConsent(ctx, cfgTool)
	default:
		return nil, unavailable(cfgTool)
	}
}

// pauseForConsent asks the user to sign in to t's provider: it records the
// auth config as the call's requested credential and answers the call with
// a waiting result instead of running the tool. ADK then sends an
// adk_request_credential call, one per credential key, and ends the run.
func (r *resolver) pauseForConsent(ctx agent.Context, t Tool) (map[string]any, error) {
	key := auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: t.slot()}
	req, err := r.authRequest(t, key)
	if err != nil {
		return nil, unavailable(t)
	}
	actions := ctx.Actions()
	if actions.RequestedAuthConfigs == nil {
		actions.RequestedAuthConfigs = map[string]toolauth.Config{}
	}
	actions.RequestedAuthConfigs[ctx.FunctionCallID()] = *req
	return map[string]any{"status": fmt.Sprintf(waitingResult, t.Provider)}, nil
}

// notAvailable is the error the model sees for a configured tool in a run
// marked WithoutStoredTokens.
func notAvailable(t Tool) error {
	return fmt.Errorf("%s is not available in this run", t.Provider)
}

// unavailable is the error the model sees when t's sign-in cannot be checked
// or started. It names the provider but not the user.
func unavailable(t Tool) error {
	return fmt.Errorf("%s sign-in is unavailable right now", t.Provider)
}
