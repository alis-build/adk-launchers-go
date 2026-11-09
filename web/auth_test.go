package web

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	iam "go.alis.build/iam/v3"
	"google.golang.org/adk/v2/server/authn"
)

func TestIdentityAuthenticator(t *testing.T) {
	t.Run("caller from the forwarded identity", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://localhost/api/list-apps", nil)
		id := &iam.Identity{ID: "123", Email: "a@x.com", Type: iam.User}
		req = req.WithContext(id.Context(req.Context()))

		got, err := IdentityAuthenticator().Authenticate(req)
		if err != nil {
			t.Fatalf("Authenticate() error = %v", err)
		}
		want := &authn.Caller{UserID: "123", Claims: map[string]any{"email": "a@x.com", "type": "user"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Authenticate() caller mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("no identity is unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://localhost/api/list-apps", nil)

		_, err := IdentityAuthenticator().Authenticate(req)
		if !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("Authenticate() error = %v, want one wrapping authn.ErrUnauthenticated", err)
		}
	})
}
