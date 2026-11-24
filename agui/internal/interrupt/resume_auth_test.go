package interrupt

import (
	"strings"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/tool/toolauth"
)

// signInRecord is the pending record of an auth_required interrupt for
// ADK's credential call c-9.
func signInRecord() []Record {
	return []Record{{ID: "c-9", Reason: ReasonAuthRequired, CallName: toolauth.RequestCredentialFunctionCallName}}
}

func TestEntriesToResumeContent_authRequiredResolved(t *testing.T) {
	payload := map[string]any{
		"authScheme": map[string]any{"type": "oauth2"},
		"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
			"authResponseUri": "https://app.example.com/cb?state=s1&code=c1",
			"redirectUri":     "https://app.example.com/cb",
			"authUri":         "https://accounts.example.com/o/oauth2/auth?state=s1",
			"clientSecret":    "from-the-client",
			"state":           "client-chosen",
		}},
		"tokenUri": "https://evil.example.com/token",
	}

	content, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: payload}}, signInRecord())
	if err != nil {
		t.Fatalf("EntriesToResumeContent() error = %v", err)
	}

	fr := partsByID(t, content)["c-9"]
	if fr.Name != toolauth.RequestCredentialFunctionCallName {
		t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, toolauth.RequestCredentialFunctionCallName)
	}
	want := map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": "https://app.example.com/cb?state=s1&code=c1",
		"redirectUri":     "https://app.example.com/cb",
	}}
	if diff := cmp.Diff(want, fr.Response); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

// An OpenID Connect sign-in keeps its credential type; anything else is
// answered as oauth2.
func TestEntriesToResumeContent_authRequiredKeepsOpenIDConnect(t *testing.T) {
	for _, tc := range []struct{ authType, want string }{
		{"openIdConnect", "openIdConnect"},
		{"oauth2", "oauth2"},
		{"apiKey", "oauth2"},
		{"", "oauth2"},
	} {
		payload := map[string]any{"exchangedAuthCredential": map[string]any{"authType": tc.authType, "oauth2": map[string]any{
			"authResponseUri": "https://app.example.com/cb?state=s1&code=c1",
		}}}
		content, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: payload}}, signInRecord())
		if err != nil {
			t.Fatalf("authType %q: EntriesToResumeContent() error = %v", tc.authType, err)
		}
		if got := partsByID(t, content)["c-9"].Response["authType"]; got != tc.want {
			t.Errorf("authType %q answered as %v, want %s", tc.authType, got, tc.want)
		}
	}
}

func TestEntriesToResumeContent_authRequiredNeedsResponseURI(t *testing.T) {
	payload := map[string]any{"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"redirectUri": "https://app.example.com/cb",
	}}}

	_, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: payload}}, signInRecord())

	if err == nil || !strings.Contains(err.Error(), "authResponseUri") {
		t.Errorf("EntriesToResumeContent() error = %v, want one naming authResponseUri", err)
	}
}

func TestEntriesToResumeContent_authRequiredCancelled(t *testing.T) {
	content, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusCancelled}}, signInRecord())
	if err != nil {
		t.Fatalf("EntriesToResumeContent() error = %v", err)
	}

	fr := partsByID(t, content)["c-9"]
	if fr.Name != toolauth.RequestCredentialFunctionCallName {
		t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, toolauth.RequestCredentialFunctionCallName)
	}
	if diff := cmp.Diff(map[string]any{"authType": "oauth2", "oauth2": map[string]any{}}, fr.Response); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestValidateResume_authRequiredNeedsExchangedCredential(t *testing.T) {
	pending := []Record{{ID: "c-9", Reason: ReasonAuthRequired, CallName: toolauth.RequestCredentialFunctionCallName, ResponseSchema: AuthRequiredResponseSchema()}}

	err := ValidateResumeAgainstPending([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: map[string]any{"authScheme": map[string]any{}}}}, pending, time.Now())

	if err == nil || !strings.Contains(err.Error(), `payload missing required field "exchangedAuthCredential"`) {
		t.Errorf("ValidateResumeAgainstPending() error = %v, want missing exchangedAuthCredential", err)
	}
}
