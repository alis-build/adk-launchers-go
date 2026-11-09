package tooloauth

import (
	"errors"
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// NewPlugin returns the runner plugin, named "tooloauth", that gives each
// configured tool the user's token: it serves a stored one, refreshing it
// when expired; pauses a tool with no token for the user's sign-in; and
// completes the sign-in when the user answers, without running the tool on
// that answer. When a run answering a confirmation starts, it reads the
// run's session for the sign-in requests it made. Add it through launcher.Config.PluginConfig. It
// fails when cfg is invalid.
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
// untouched, and so does a configured tool whose user has a usable token. A
// tool with no token pauses for the user's consent, and the resumed call
// completes the sign-in instead of running the tool; in a run marked by
// WithNonInteractive it stops with "connect <Provider> in chat first"
// instead. A store failure stops the call with an error the model sees,
// which names the provider but not the user.
func (r *resolver) beforeTool(ctx agent.Context, t tool.Tool, _ map[string]any) (map[string]any, error) {
	cfgTool, ok := r.cfg.Tools[t.Name()]
	if !ok {
		return nil, nil
	}
	if conf := ctx.ToolConfirmation(); conf != nil {
		if req, ok := r.lookupSignIn(ctx); ok {
			// An answered or conflicting request, or one another resume is
			// already exchanging, is never completed again and never passes
			// as the tool's own approval.
			if req.answered || req.conflicting {
				return nil, fmt.Errorf("%s sign-in failed: the sign-in request is out of date", cfgTool.Provider)
			}
			return r.completeSignIn(ctx, cfgTool, req.config, conf)
		}
		// A sign-in response with no sign-in request on record must never
		// pass as the tool's own approval, which any confirmed
		// confirmation would.
		if _, isSignIn := decodeAuthResponse(conf.Payload); isSignIn {
			return nil, fmt.Errorf("%s sign-in failed: the sign-in request is out of date", cfgTool.Provider)
		}
	}
	_, err := r.credential(ctx, t.Name(), ctx.AppName(), ctx.UserID())
	switch {
	case err == nil:
		return nil, nil
	case errors.Is(err, errMiss) && IsNonInteractive(ctx):
		// No auth URI here: this text is persisted and fed to the model.
		return nil, fmt.Errorf("connect %s in chat first", cfgTool.Provider)
	case errors.Is(err, errMiss):
		return r.pauseForConsent(ctx, t.Name(), cfgTool)
	default:
		return nil, unavailable(cfgTool)
	}
}

// pauseForConsent asks the user to sign in to t's provider: it requests a
// confirmation whose payload is the auth config, and stops the call with
// tool.ErrConfirmationRequired, ADK's own pause, so the tool does not run in
// this turn.
func (r *resolver) pauseForConsent(ctx agent.Context, name string, t Tool) (map[string]any, error) {
	key := auth.CredentialKey{AppName: ctx.AppName(), UserID: ctx.UserID(), Key: t.slot()}
	req, err := r.authRequest(t, key)
	if err != nil {
		return nil, unavailable(t)
	}
	if err := ctx.RequestConfirmation(fmt.Sprintf(consentHint, t.Provider), req); err != nil {
		return nil, unavailable(t)
	}
	return nil, fmt.Errorf("tool %q: %w", name, tool.ErrConfirmationRequired)
}

// unavailable is the error the model sees when t's sign-in cannot be checked
// or started. It names the provider but not the user.
func unavailable(t Tool) error {
	return fmt.Errorf("%s sign-in is unavailable right now", t.Provider)
}
