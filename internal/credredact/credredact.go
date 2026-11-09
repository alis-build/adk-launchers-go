// Package credredact strips credential secrets from decoded JSON, matching
// adk-python's _redact_credential_secrets (src/google/adk/auth/auth_credential.py)
// so the Go launchers return the same redacted payloads its dev server does.
//
// It works on the values encoding/json produces when decoding into any: maps
// keyed by string, slices, and scalars. It never modifies its input.
package credredact

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// secretKeys are the keys removed from anything treated as an auth config, in
// both the camelCase wire form and the snake_case form.
var secretKeys = map[string]bool{
	"password":           true,
	"token":              true,
	"additionalHeaders":  true,
	"additional_headers": true,
	"clientSecret":       true,
	"client_secret":      true,
	"authResponseUri":    true,
	"auth_response_uri":  true,
	"authCode":           true,
	"auth_code":          true,
	"accessToken":        true,
	"access_token":       true,
	"refreshToken":       true,
	"refresh_token":      true,
	"idToken":            true,
	"id_token":           true,
	"codeVerifier":       true,
	"code_verifier":      true,
	"privateKeyId":       true,
	"private_key_id":     true,
	"privateKey":         true,
	"private_key":        true,
	"apiKey":             true,
	"api_key":            true,
}

// authTypes are adk-python's AuthCredentialTypes values, member names and
// lower-cased member names. A map whose authType is one of them is a
// credential.
var authTypes = map[string]bool{
	"apiKey": true, "http": true, "oauth2": true, "openIdConnect": true, "serviceAccount": true,
	"API_KEY": true, "HTTP": true, "OAUTH2": true, "OPEN_ID_CONNECT": true, "SERVICE_ACCOUNT": true,
	"api_key": true, "open_id_connect": true, "service_account": true,
}

// requestCredentialCall names the function call ADK uses to ask the client for
// end-user credentials.
const requestCredentialCall = "adk_request_credential"

// Value returns a redacted copy of v, a value produced by encoding/json
// decoding into any. As in adk-python, the args of an adk_request_credential
// call are copied shallowly: values other than its authConfig are shared with
// v, so a caller must not modify them in the result.
func Value(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return redactMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = Value(item)
		}
		return out
	default:
		return v
	}
}

// JSON encodes v through encoding/json, redacts the result, and returns the
// redacted JSON bytes. Use it to answer an HTTP request with a stored value
// that may carry credentials.
func JSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("credredact: encode: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("credredact: decode: %w", err)
	}
	out, err := json.Marshal(Value(decoded))
	if err != nil {
		return nil, fmt.Errorf("credredact: encode redacted: %w", err)
	}
	return out, nil
}

// redactMap applies adk-python's rules to one map, in its order.
func redactMap(m map[string]any) map[string]any {
	// A credential: strip secret keys everywhere inside it.
	for _, key := range []string{"authType", "auth_type"} {
		if s, ok := m[key].(string); ok && authTypes[s] {
			return redactAuthConfig(m).(map[string]any)
		}
	}

	// An adk_request_credential call: its auth config and response carry
	// credentials; its other args are copied as they are.
	if name, _ := m["name"].(string); name == requestCredentialCall {
		out := make(map[string]any, len(m))
		for key, val := range m {
			switch args, isMap := val.(map[string]any); {
			case key == "args" && isMap:
				newArgs := maps.Clone(args)
				for _, authKey := range []string{"authConfig", "auth_config"} {
					if cfg, ok := newArgs[authKey]; ok {
						newArgs[authKey] = redactAuthConfig(cfg)
					}
				}
				out[key] = newArgs
			case key == "response":
				out[key] = redactAuthConfig(val)
			default:
				out[key] = Value(val)
			}
		}
		return out
	}

	// Event actions requesting credentials, then credential holders: those
	// keys hold auth configs; their siblings are redacted normally.
	for _, carriers := range [][]string{
		{"requestedAuthConfigs", "requested_auth_configs"},
		{"rawAuthCredential", "raw_auth_credential", "exchangedAuthCredential", "exchanged_auth_credential"},
	} {
		if !hasAny(m, carriers) {
			continue
		}
		out := make(map[string]any, len(m))
		for key, val := range m {
			if slices.Contains(carriers, key) {
				out[key] = redactAuthConfig(val)
			} else {
				out[key] = Value(val)
			}
		}
		return out
	}

	out := make(map[string]any, len(m))
	for key, val := range m {
		out[key] = Value(val)
	}
	return out
}

// redactAuthConfig removes secret keys at every depth of an auth config.
func redactAuthConfig(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, val := range t {
			if !secretKeys[key] {
				out[key] = redactAuthConfig(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = redactAuthConfig(item)
		}
		return out
	default:
		return v
	}
}

func hasAny(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}
