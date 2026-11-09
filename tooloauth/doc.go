// Package tooloauth gives agent tools the end user's OAuth token. It is an
// ADK runner plugin that serves a stored token, asks the user to sign in
// mid-run when there is none, and stores the tokens it obtains through ADK's
// [auth.CredentialStore]. The sign-in request uses the wire format of ADK's
// toolauth types, the auth config adk-python sends.
//
// # Setup
//
// Name each tool that needs a token in a [Config], add the plugin to the
// runner, and have the tool send its requests through the provider the same
// Config hands out:
//
//	cfg := tooloauth.Config{
//		Tools: map[string]tooloauth.Tool{"calendar_list": {
//			Provider:     "Google Calendar",
//			Scheme:       scheme, // oauth2 with an authorization code flow
//			ClientID:     clientID,
//			ClientSecret: clientSecret,
//			Scopes:       []string{"https://www.googleapis.com/auth/calendar.readonly"},
//			RedirectURI:  "https://app.example.com/oauth/callback",
//		}},
//		Store: store, // nil keeps tokens in memory only
//	}
//	plugin, err := tooloauth.NewPlugin(cfg)
//	// launcher.Config{PluginConfig: runner.PluginConfig{Plugins: []*plugin.Plugin{plugin}}}
//
//	provider, err := cfg.Provider("calendar_list")
//	client := &http.Client{Transport: &auth.Transport{Provider: provider}}
//	// In the tool: http.NewRequestWithContext(toolCtx, ...) and client.Do.
//
// The provider reads the acting user from the tool's context, so the request
// must carry that context.
//
// # What the plugin does
//
// Before a configured tool runs, the plugin looks the user's token up in the
// store. A usable token lets the tool run; an expired one with a refresh
// token is refreshed first, once even when several calls in one turn need
// it, and kept for [Config.RefreshTokenTTL] from each refresh (90 days by
// default), so a user who keeps using a tool stays signed in. A refresh the
// provider refuses because the grant cannot be used (invalid_grant,
// invalid_scope, or a bare 400) drops the token so the user signs in again;
// an outage or a client-setup error keeps it and reports the sign-in as
// unavailable.
//
// With no usable token, the plugin pauses the call with ADK's confirmation
// request. Its hint is "Sign in to <Provider> to continue." and its payload
// is the auth config: the scheme with the tool's scopes, the client ID and
// redirect URI, and an authorization URI carrying a fresh random state,
// offline access and forced consent. The client secret is stripped before
// the request is stored or sent.
//
// When the user answers, the plugin checks the reply's state against the one
// it sent, exchanges the code with its own client credentials, stores the
// token, and answers the call with "connected to <Provider>, call the tool
// again". The tool does not run on that answer: ADK would take the sign-in's
// confirmation as the tool's own approval. The model's next call is a fresh
// one, so a tool that requires confirmation asks for it then. A declined,
// forged, stale, replayed or failed reply fails with an error naming the
// provider and stores nothing; the code is never sent twice.
//
// # Resuming from a client
//
// Over AG-UI (go.alis.build/adk/launchers/agui) the pause is an interrupt
// with reason "auth_required" and the stripped config in
// metadata.adk.authConfig. The client opens
// exchangedAuthCredential.oauth2.authUri, catches the provider's redirect,
// and resumes with the config, setting exchangedAuthCredential.oauth2's
// authResponseUri to the URL the provider redirected to (and redirectUri if
// it used its own redirect). A "cancelled" resume declines.
//
// Over ADK's REST API the client answers the adk_request_confirmation call
// with a FunctionResponse of {"confirmed": true, "payload": <the filled
// config>}. Only exchangedAuthCredential.oauth2.authResponseUri and
// redirectUri are read from it; everything else in a client's reply is
// ignored.
//
// # Storing tokens
//
// [Config.Store] takes any [auth.CredentialStore], so you can bring your own.
// A nil Store uses one in-memory store for the whole process, which loses
// every token on restart; a warning is logged the first time it is used.
// Package go.alis.build/adk/launchers/tooloauth/spannerstore keeps tokens in
// Cloud Spanner under Cloud KMS envelope encryption. The plugin stores only
// [*Token] values, so a store needs to encode only that type, and it passes
// the expiry the store must honour.
//
// To revoke a user's token, delete it under the key [Config.CredentialKey]
// returns.
//
// # Headless runs
//
// A run nobody can answer, such as a scheduled job or an eval, must not
// pause. Mark its context with [WithNonInteractive]; a configured tool with
// no token then fails with "connect <Provider> in chat first". The scheduler
// and evals launchers in this module mark their runs. A headless run that
// carries a sign-in reply still completes the sign-in.
//
// # Limitations
//
//   - Only confidential clients (with a client secret) and the oauth2
//     authorization code flow or openIdConnect with inline endpoints are
//     supported.
//   - One sign-in per tool call per turn.
//   - ADK's own dev web UI cannot complete the flow: its sign-in popup opens
//     only for adk_request_credential, which this plugin does not emit.
//     AG-UI clients and REST clients can.
//   - agui strips secrets from the interrupt only; message snapshots and
//     history carry the request's arguments as stored, which the plugin has
//     already stripped. The client's reply is kept in the session as the
//     user's confirmation response, so its auth response URI, with the
//     spent single-use code and the state, appears in that thread's agui
//     history and snapshots; ADK never puts it in the model's input.
//   - A token the provider revokes is served until its stored entry
//     expires: the provider does not refresh or drop a token a downstream
//     API rejects. A token with neither an expiry nor a refresh token is
//     kept for RefreshTokenTTL. Revoke one early by deleting it under
//     [Config.CredentialKey].
//   - A configured tool used inside an ADK agenttool sub-agent gets no
//     plugin: ADK's inner runner has no plugins and uses the sub-agent's
//     name as the app name. It never pauses, and its provider reports it is
//     not connected.
//   - Refusing a reused authorization code is per process: two duplicate
//     replies arriving at the same moment on different instances can both be
//     exchanged. A replay after a reply completed is refused everywhere.
//   - When a tool's own approval is resumed and the call then needs a
//     sign-in, ADK emits no request on that resume path; the sign-in prompt
//     comes on the model's next call.
package tooloauth
