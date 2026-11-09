package web

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	iam "go.alis.build/iam/v3"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	adkapi "google.golang.org/adk/v2/cmd/launcher/web/api"
	"google.golang.org/adk/v2/server/authn"
	"google.golang.org/adk/v2/server/authz"
	"google.golang.org/adk/v2/session"
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

func TestApplyAuthDefaults(t *testing.T) {
	t.Run("fills both hooks when unset", func(t *testing.T) {
		config := &launcher.Config{}
		applyAuthDefaults(config)
		if config.Authenticator == nil {
			t.Error("Authenticator is nil, want the identity authenticator")
		}
		if config.Authorizer == nil {
			t.Error("Authorizer is nil, want a strict authorizer")
		}
	})

	t.Run("keeps an authorizer the embedder set", func(t *testing.T) {
		noop := authz.NewNoop()
		config := &launcher.Config{Authorizer: noop}
		applyAuthDefaults(config)
		if config.Authorizer != noop {
			t.Errorf("Authorizer = %T, want the embedder's no-op authorizer kept", config.Authorizer)
		}
		if config.Authenticator == nil {
			t.Error("Authenticator is nil, want the identity authenticator")
		}
	})
}

// TestAPIIdentityThroughLauncher drives ADK's real REST API sublauncher through
// buildRouter, which must apply the identity defaults itself: nothing here
// calls applyAuthDefaults.
func TestAPIIdentityThroughLauncher(t *testing.T) {
	root, err := agent.New(agent.Config{
		Name: "a",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() failed: %v", err)
	}
	sessions := session.InMemoryService()
	if _, err := sessions.Create(context.Background(), &session.CreateRequest{AppName: "a", UserID: "u"}); err != nil {
		t.Fatalf("creating a session for u failed: %v", err)
	}
	config := &launcher.Config{SessionService: sessions, AgentLoader: agent.NewSingleLoader(root)}

	l := NewLauncher(adkapi.NewLauncher()).(*webLauncher)
	if _, err := l.Parse([]string{"api"}); err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	applyServiceDefaults(config)
	router, err := l.buildRouter(config)
	if err != nil {
		t.Fatalf("buildRouter() failed: %v", err)
	}

	for _, tc := range []struct {
		name     string
		identity string
		want     int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "the session's own user", identity: "u", want: http.StatusOK},
		{name: "another user", identity: "v", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/api/apps/a/users/u/sessions", nil)
			if tc.identity != "" {
				req = req.WithContext((&iam.Identity{ID: tc.identity, Type: iam.User}).Context(req.Context()))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("GET sessions as %q = %d, want %d (body %q)", tc.identity, rec.Code, tc.want, rec.Body)
			}
		})
	}
	// ADK keeps its public routes open under an authenticator; probes rely on it.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("anonymous GET /api/health = %d, want %d", rec.Code, http.StatusOK)
	}
}
