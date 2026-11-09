package credredact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// decode turns a JSON literal into the value shape encoding/json produces.
func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return v
}

// oauth2AuthConfig is adk-python's oauth2_auth_config_dict fixture
// (tests/unittests/cli/test_fast_api.py).
const oauth2AuthConfig = `{
	"authScheme": {
		"type": "oauth2",
		"flows": {"authorizationCode": {
			"scopes": {"read": "read"},
			"authorizationUrl": "https://idp.example.com/oauth2/auth",
			"tokenUrl": "https://idp.example.com/oauth2/token"
		}}
	},
	"rawAuthCredential": {
		"authType": "oauth2",
		"oauth2": {"clientId": "public-client-id", "clientSecret": "should-never-reach-the-client"}
	},
	"exchangedAuthCredential": {
		"authType": "oauth2",
		"oauth2": {
			"clientId": "public-client-id",
			"clientSecret": "should-never-reach-the-client",
			"authUri": "https://idp.example.com/oauth2/auth?client_id=public-client-id&state=xyz",
			"state": "xyz",
			"codeVerifier": "pkce-verifier-should-not-leak-either"
		}
	},
	"credentialKey": "my_tool:oauth2:abcd1234"
}`

func TestValueMatchesPython(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			// adk-python test_redact_credential_secrets_strips_snake_case_secrets.
			name: "snake_case secrets",
			in: `{"auth_type": "oauth2",
				"oauth2": {"client_id": "public-client-id", "client_secret": "s1", "access_token": "s2", "refresh_token": "s3"},
				"raw_auth_credential": {"oauth2": {"client_id": "public-client-id-2", "client_secret": "s4"}}}`,
			want: `{"auth_type": "oauth2",
				"oauth2": {"client_id": "public-client-id"},
				"raw_auth_credential": {"oauth2": {"client_id": "public-client-id-2"}}}`,
		},
		{
			// adk-python test_redact_credential_secrets_strips_snake_case_auth_types.
			name: "snake_case auth types",
			in: `{"api_key_cred": {"auth_type": "api_key", "api_key": "secret-api-key"},
				"sa_cred": {"auth_type": "service_account", "service_account": {"private_key": "secret-private-key"}},
				"oidc_cred": {"auth_type": "open_id_connect", "oauth2": {"client_secret": "secret-client-secret"}}}`,
			want: `{"api_key_cred": {"auth_type": "api_key"},
				"sa_cred": {"auth_type": "service_account", "service_account": {}},
				"oidc_cred": {"auth_type": "open_id_connect", "oauth2": {}}}`,
		},
		{
			// adk-python test_redact_credential_secrets_handles_unhashable_auth_type:
			// a non-string authType is not an auth carrier and is left alone.
			name: "non-string auth type",
			in: `{"authType": {"nested": "dict"}, "auth_type": ["nested", "list"],
				"args": {"authType": {"inner": "value"}, "auth_type": [{"another": "item"}]}}`,
			want: `{"authType": {"nested": "dict"}, "auth_type": ["nested", "list"],
				"args": {"authType": {"inner": "value"}, "auth_type": [{"another": "item"}]}}`,
		},
		{
			// Rule 2 (auth_credential.py:405-420), with adk-python's fixture as
			// the call's authConfig: both credentials lose clientSecret, the
			// exchanged one loses codeVerifier; clientId, authUri, state and
			// credentialKey stay. Other args are copied as they are, not redacted.
			name: "adk_request_credential call",
			in: `{"name": "adk_request_credential", "id": "fc-1",
				"args": {"functionCallId": "call-1", "authConfig": ` + oauth2AuthConfig + `, "note": {"token": "kept-in-args"}}}`,
			want: `{"name": "adk_request_credential", "id": "fc-1",
				"args": {"functionCallId": "call-1", "note": {"token": "kept-in-args"}, "authConfig": {
					"authScheme": {"type": "oauth2", "flows": {"authorizationCode": {
						"scopes": {"read": "read"},
						"authorizationUrl": "https://idp.example.com/oauth2/auth",
						"tokenUrl": "https://idp.example.com/oauth2/token"}}},
					"rawAuthCredential": {"authType": "oauth2", "oauth2": {"clientId": "public-client-id"}},
					"exchangedAuthCredential": {"authType": "oauth2", "oauth2": {
						"clientId": "public-client-id",
						"authUri": "https://idp.example.com/oauth2/auth?client_id=public-client-id&state=xyz",
						"state": "xyz"}},
					"credentialKey": "my_tool:oauth2:abcd1234"}}}`,
		},
		{
			// Rule 2: the response half of the call goes through the
			// auth-config redactor; other keys through the full redactor.
			name: "adk_request_credential response",
			in: `{"name": "adk_request_credential", "response": {"exchangedAuthCredential": {"oauth2": {"accessToken": "at", "clientId": "c"}}},
				"other": {"authType": "apiKey", "apiKey": "k"}}`,
			want: `{"name": "adk_request_credential", "response": {"exchangedAuthCredential": {"oauth2": {"clientId": "c"}}},
				"other": {"authType": "apiKey"}}`,
		},
		{
			// Rule 3 (auth_credential.py:422-429).
			name: "requestedAuthConfigs",
			in: `{"requestedAuthConfigs": {"call-1": {"exchangedAuthCredential": {"oauth2": {"refreshToken": "rt", "clientId": "c"}}}},
				"stateDelta": {"x": {"authType": "http", "http": {"credentials": {"password": "p", "username": "u"}}}}}`,
			want: `{"requestedAuthConfigs": {"call-1": {"exchangedAuthCredential": {"oauth2": {"clientId": "c"}}}},
				"stateDelta": {"x": {"authType": "http", "http": {"credentials": {"username": "u"}}}}}`,
		},
		{
			// Rule 4 (auth_credential.py:431-454): credential keys keep their
			// value, minus secret keys; siblings use the full redactor.
			name: "raw and exchanged credential keys",
			in: `{"exchanged_auth_credential": {"oauth2": {"id_token": "it", "client_id": "c"}},
				"sibling": {"token": "not-a-carrier"}}`,
			want: `{"exchanged_auth_credential": {"oauth2": {"client_id": "c"}},
				"sibling": {"token": "not-a-carrier"}}`,
		},
		{
			// Rule 2 with non-map args: they go through the full redactor.
			name: "adk_request_credential with list args",
			in:   `{"name": "adk_request_credential", "args": [{"authType": "oauth2", "oauth2": {"clientSecret": "s"}}]}`,
			want: `{"name": "adk_request_credential", "args": [{"authType": "oauth2", "oauth2": {}}]}`,
		},
		{
			// Rule 1 wins over rule 2: the whole map is an auth config.
			name: "auth type beats call name",
			in:   `{"authType": "oauth2", "name": "adk_request_credential", "args": {"x": {"token": "t"}}}`,
			want: `{"authType": "oauth2", "name": "adk_request_credential", "args": {"x": {}}}`,
		},
		{
			// Rule 3 wins over rule 4, so a sibling rawAuthCredential goes
			// through the full redactor and, holding no auth type, keeps its
			// token, exactly as adk-python does.
			name: "requested configs beat credential keys",
			in:   `{"requestedAuthConfigs": {"a": {"token": "t1"}}, "rawAuthCredential": {"token": "t2"}}`,
			want: `{"requestedAuthConfigs": {"a": {}}, "rawAuthCredential": {"token": "t2"}}`,
		},
		{
			// Rule 5: other maps and lists recurse; scalars pass through.
			name: "nested list",
			in:   `[1, "x", {"deep": [{"authType": "OAUTH2", "oauth2": {"clientSecret": "s"}}]}]`,
			want: `[1, "x", {"deep": [{"authType": "OAUTH2", "oauth2": {}}]}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Value(decode(t, tc.in))
			if diff := cmp.Diff(decode(t, tc.want), got); diff != "" {
				t.Errorf("Value() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValueDoesNotMutateInput(t *testing.T) {
	in := decode(t, `{"authType": "oauth2", "oauth2": {"clientSecret": "s", "clientId": "c"}}`)
	_ = Value(in)
	if diff := cmp.Diff(decode(t, `{"authType": "oauth2", "oauth2": {"clientSecret": "s", "clientId": "c"}}`), in); diff != "" {
		t.Errorf("input mutated (-want +got):\n%s", diff)
	}
}

func TestJSONRedactsTypedValues(t *testing.T) {
	type cred struct {
		AuthType string            `json:"authType"`
		OAuth2   map[string]string `json:"oauth2"`
	}
	b, err := JSON(cred{AuthType: "oauth2", OAuth2: map[string]string{"clientId": "c", "clientSecret": "s3cret"}})
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("JSON() = %s, still contains the secret", b)
	}
	if !strings.Contains(string(b), `"clientId":"c"`) {
		t.Errorf("JSON() = %s, lost the client ID", b)
	}
}
