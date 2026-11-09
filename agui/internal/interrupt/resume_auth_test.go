package interrupt

import (
	"strings"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

func TestEntriesToResumeContent_authRequiredResolved(t *testing.T) {
	pending := []Record{{ID: "c-9", Reason: ReasonAuthRequired}}
	payload := map[string]any{"exchangedAuthCredential": map[string]any{"authType": "oauth2", "oauth2": map[string]any{
		"authResponseUri": "http://localhost:4200/?state=s-1&code=c-1",
	}}}

	content, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: payload}}, pending)
	if err != nil {
		t.Fatalf("EntriesToResumeContent() error = %v", err)
	}

	fr := partsByID(t, content)["c-9"]
	if fr.Name != toolconfirmation.FunctionCallName {
		t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, toolconfirmation.FunctionCallName)
	}
	if fr.Response["confirmed"] != true {
		t.Errorf("confirmed = %v, want true", fr.Response["confirmed"])
	}
	got, _ := fr.Response["payload"].(map[string]any)
	ex, _ := got["exchangedAuthCredential"].(map[string]any)
	o, _ := ex["oauth2"].(map[string]any)
	if o["authResponseUri"] != "http://localhost:4200/?state=s-1&code=c-1" {
		t.Errorf("payload = %v, want the filled auth config", fr.Response["payload"])
	}
}

func TestEntriesToResumeContent_authRequiredCancelled(t *testing.T) {
	pending := []Record{{ID: "c-9", Reason: ReasonAuthRequired}}

	content, err := EntriesToResumeContent([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusCancelled}}, pending)
	if err != nil {
		t.Fatalf("EntriesToResumeContent() error = %v", err)
	}

	fr := partsByID(t, content)["c-9"]
	if len(fr.Response) != 1 || fr.Response["confirmed"] != false {
		t.Errorf("response = %v, want {confirmed: false}", fr.Response)
	}
}

func TestValidateResume_authRequiredNeedsExchangedCredential(t *testing.T) {
	pending := []Record{{ID: "c-9", Reason: ReasonAuthRequired, ResponseSchema: AuthRequiredResponseSchema()}}

	err := ValidateResumeAgainstPending([]types.ResumeEntry{{InterruptID: "c-9", Status: types.ResumeStatusResolved, Payload: map[string]any{"authScheme": map[string]any{}}}}, pending, time.Now())

	if err == nil || !strings.Contains(err.Error(), `payload missing required field "exchangedAuthCredential"`) {
		t.Errorf("ValidateResumeAgainstPending() error = %v, want missing exchangedAuthCredential", err)
	}
}
