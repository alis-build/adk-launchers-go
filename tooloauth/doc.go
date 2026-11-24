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
// With no usable token, the plugin asks the user to sign in the way ADK's
// own tools do: it records the auth config as the call's requested
// credential and answers the call with "waiting for the user to sign in to
// <Provider>". ADK then sends an adk_request_credential call, one per
// credential key however many calls asked, and ends the run. The auth
// config holds the scheme with the tool's scopes, the client ID and
// redirect URI, and an authorization URI carrying a fresh random state,
// offline access and forced consent. The client secret is stripped before
// the request is stored or sent.
//
// When the user answers, ADK keeps the answer for the rest of the run, out
// of the session's stored state and events, and runs the waiting calls
// again. The plugin checks the answer against the requests it issued in the
// session and the redirect's state against the one it sent, exchanges the
// code with its own client credentials, stores the token, and lets the tool
// run in that same turn. Calls sharing one sign-in exchange the code once.
// A forged, stale or failed answer fails with an error naming the provider
// and stores nothing; an answer with no auth response URI cancels with
// "<Provider> sign-in was cancelled". ADK keeps the answer for the rest of
// the run, so a failed or cancelled sign-in answers every later call to a
// tool on that sign-in in the same run with the same error; the user can
// sign in on their next message. A replayed answer never resumes the
// call or sends the code twice: ADK drops an answer to a call it has
// already answered, and the plugin refuses a duplicate that still reaches
// it.
//
// # Resuming from a client
//
// ADK's dev web UI completes the flow itself: it opens the authorization
// URI in a popup and answers the request. The popup returns to /dev-ui/,
// which go.alis.build/adk/launchers/web forwards to ADK Go's /ui/; a host
// serving the UI some other way needs the same route.
//
// Over AG-UI (go.alis.build/adk/launchers/agui) the pause is an interrupt
// with reason "auth_required" and the stripped config in
// metadata.adk.authConfig. The client opens
// exchangedAuthCredential.oauth2.authUri, catches the provider's redirect,
// and resumes with the config, setting exchangedAuthCredential.oauth2's
// authResponseUri to the URL the provider redirected to (and redirectUri if
// it used its own redirect). A "cancelled" resume cancels.
//
// Over ADK's REST API the client answers the adk_request_credential call
// with a FunctionResponse whose response is the filled config, or a bare
// credential such as {"authType": "oauth2", "oauth2": {"authResponseUri":
// ..., "redirectUri": ...}}; an empty oauth2 object cancels. Only
// authResponseUri and redirectUri are read from it: ADK takes the rest,
// the state included, from the request it sent, and the client ID and
// secret always come from the plugin's Config. A sign-in answer sent as a
// tool confirmation, as clients of the plugin's earlier versions sent it,
// is refused as out of date.
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
// carries a sign-in reply still completes the sign-in, unless it is also
// marked WithoutStoredTokens.
//
// A run that must not use anyone's stored token at all is marked with
// [WithoutStoredTokens]; every configured tool then fails with "<Provider>
// is not available in this run". Eval runs execute as the caller, so the
// evals launcher marks them this way unless the embedder opts in with
// evals.WithStoredOAuthTokens: an eval of a tool that writes would otherwise
// act on the caller's real account.
//
// # Limitations
//
//   - Only confidential clients (with a client secret) and the oauth2
//     authorization code flow or openIdConnect with inline endpoints are
//     supported.
//   - The user's answer is kept in the session as the user's message, so
//     its auth response URI, with the spent single-use code and the state,
//     appears in the session's history (and an agui thread's history and
//     snapshots); ADK never puts it in the model's input.
//   - An answer ADK cannot read as an auth config or a credential ends the
//     run with an error rather than a tool error the model sees.
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
//     answers arriving at the same moment on different instances can both be
//     exchanged. A replay after an answer completed never resumes the call
//     on any instance: ADK finds the call already answered in the session.
//   - A tool that also requires the user's own approval does not ask for it
//     on ADK's rerun after the sign-in: the rerun answers that the tool
//     requires confirmation, and the approval prompt comes on the model's
//     next call. Likewise, when a tool's own approval is resumed and the
//     call then needs a sign-in, the sign-in prompt comes on the model's next
//     call.
package tooloauth
