package tooloauth_test

import (
	"net/http"

	"go.alis.build/adk/launchers/tooloauth"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/tool/toolauth"
)

// The plugin is added to the runner, and the tool sends its requests through
// the provider from the same Config. Store is nil here, which keeps tokens in
// memory only; use tooloauth/spannerstore or your own store in production.
func ExampleNewPlugin() {
	cfg := tooloauth.Config{
		Tools: map[string]tooloauth.Tool{"calendar_list": {
			Provider:     "Google Calendar",
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			Scopes:       []string{"https://www.googleapis.com/auth/calendar.readonly"},
			RedirectURI:  "https://app.example.com/oauth/callback",
			Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{
				AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
					TokenURL:         "https://oauth2.googleapis.com/token",
				},
			}},
		}},
	}
	p, err := tooloauth.NewPlugin(cfg)
	if err != nil {
		panic(err)
	}
	_ = &launcher.Config{PluginConfig: runner.PluginConfig{Plugins: []*plugin.Plugin{p}}}

	provider, err := cfg.Provider("calendar_list")
	if err != nil {
		panic(err)
	}
	// The tool calls client.Do with requests built on its tool context, which
	// carries the acting user.
	client := &http.Client{Transport: &auth.Transport{Provider: provider}}
	_ = client
}
