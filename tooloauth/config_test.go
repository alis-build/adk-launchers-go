package tooloauth

import (
	"testing"
	"time"

	"google.golang.org/adk/v2/tool/toolauth"
)

func TestNewPlugin_rejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "no tools",
			cfg:     Config{},
			wantErr: "tooloauth: Config.Tools is empty",
		},
		{
			name: "negative TTL",
			cfg: Config{
				Tools: map[string]Tool{"calendar_list": {
					Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
					Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
						AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
				}},
				RefreshTokenTTL: -1,
			},
			wantErr: "tooloauth: RefreshTokenTTL must not be negative",
		},
		{
			name: "TTL within the expiry skew",
			cfg: Config{
				Tools: map[string]Tool{"calendar_list": {
					Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
					Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
						AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
				}},
				RefreshTokenTTL: 10 * time.Second,
			},
			wantErr: "tooloauth: RefreshTokenTTL must be longer than 10s",
		},
		{
			name: "no provider",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": Provider is required`,
		},
		{
			name: "no client secret",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": ClientID and ClientSecret are required`,
		},
		{
			name: "no client ID",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": ClientID and ClientSecret are required`,
		},
		{
			name: "oauth2 without authorization code flow",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{ClientCredentials: &toolauth.OAuthFlow{
					TokenURL: "https://oauth2.example.com/token"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": oauth2 scheme needs an authorizationCode flow with authorizationUrl and tokenUrl`,
		},
		{
			name: "oauth2 without token URL",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.example.com/auth"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": oauth2 scheme needs an authorizationCode flow with authorizationUrl and tokenUrl`,
		},
		{
			name: "oauth2 without authorization URL",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					TokenURL: "https://oauth2.example.com/token"}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": oauth2 scheme needs an authorizationCode flow with authorizationUrl and tokenUrl`,
		},
		{
			name: "openIdConnect without authorization endpoint",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOpenIDConnect, TokenEndpoint: "https://oauth2.example.com/token"},
			}}},
			wantErr: `tooloauth: tool "calendar_list": openIdConnect scheme needs authorization_endpoint and token_endpoint`,
		},
		{
			name: "oauth2 without flows",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2},
			}}},
			wantErr: `tooloauth: tool "calendar_list": oauth2 scheme needs an authorizationCode flow with authorizationUrl and tokenUrl`,
		},
		{
			name: "openIdConnect by discovery only",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOpenIDConnect, OpenIDConnectURL: "https://accounts.example.com/.well-known/openid-configuration"},
			}}},
			wantErr: `tooloauth: tool "calendar_list": openIdConnect scheme needs authorization_endpoint and token_endpoint`,
		},
		{
			name: "unsupported scheme",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeAPIKey, In: "header", Name: "X-Api-Key"},
			}}},
			wantErr: `tooloauth: tool "calendar_list": unsupported scheme type "apiKey"`,
		},
		{
			name: "scopes in the oauth2 flow",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
					AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token",
					Scopes: map[string]string{"calendar.read": "Read calendars"}}}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": list scopes in Tool.Scopes, not in the scheme`,
		},
		{
			name: "scopes in the openIdConnect scheme",
			cfg: Config{Tools: map[string]Tool{"calendar_list": {
				Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7",
				Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOpenIDConnect,
					AuthorizationEndpoint: "https://accounts.example.com/auth", TokenEndpoint: "https://oauth2.example.com/token",
					Scopes: []string{"openid"}},
			}}},
			wantErr: `tooloauth: tool "calendar_list": list scopes in Tool.Scopes, not in the scheme`,
		},
		{
			name: "first tool in sorted order is reported",
			cfg: Config{Tools: map[string]Tool{
				"zz_tool": {ClientID: "cid-7", ClientSecret: "secret-7"},
				"aa_tool": {Provider: "Google Calendar", ClientSecret: "secret-7"},
			}},
			wantErr: `tooloauth: tool "aa_tool": ClientID and ClientSecret are required`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPlugin(tt.cfg)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("NewPlugin() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewPlugin_acceptsOAuth2AndOpenIDConnect(t *testing.T) {
	p, err := NewPlugin(Config{Tools: map[string]Tool{
		"calendar_list": {
			Provider: "Google Calendar", ClientID: "cid-7", ClientSecret: "secret-7", Scopes: []string{"calendar.read"},
			Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOAuth2, Flows: &toolauth.OAuthFlows{AuthorizationCode: &toolauth.OAuthFlow{
				AuthorizationURL: "https://accounts.example.com/auth", TokenURL: "https://oauth2.example.com/token"}}},
		},
		"drive_list": {
			Provider: "Example Drive", ClientID: "cid-8", ClientSecret: "secret-8", Scopes: []string{"openid"},
			Scheme: toolauth.Scheme{Type: toolauth.SchemeTypeOpenIDConnect,
				AuthorizationEndpoint: "https://id.example.com/authorize", TokenEndpoint: "https://id.example.com/token"},
		},
	}})
	if err != nil {
		t.Fatalf("NewPlugin() error = %v", err)
	}
	if got := p.Name(); got != "tooloauth" {
		t.Errorf("Name() = %q, want tooloauth", got)
	}
}
