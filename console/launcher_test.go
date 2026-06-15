package console

import (
	"encoding/json"
	"io/fs"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/adk/agent"
	adklauncher "google.golang.org/adk/cmd/launcher"
	adkweb "google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/session"
)

func testAgent(t *testing.T, name string) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: name,
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return a
}

func TestDefaultUseDevProxy(t *testing.T) {
	t.Run("embedded dist by default", func(t *testing.T) {
		t.Setenv("SPA_DEV_SERVER_URL", "")
		l := NewLauncher().(*consoleLauncher)
		if l.isLocal() {
			t.Fatal("expected embedded dist when SPA_DEV_SERVER_URL is unset")
		}
	})

	t.Run("SPA_DEV_SERVER_URL enables proxy", func(t *testing.T) {
		t.Setenv("SPA_DEV_SERVER_URL", "http://localhost:8000")
		l := NewLauncher().(*consoleLauncher)
		if !l.isLocal() {
			t.Fatal("expected dev proxy when SPA_DEV_SERVER_URL is set")
		}
	})

	t.Run("WithDevServerURL enables proxy", func(t *testing.T) {
		t.Setenv("SPA_DEV_SERVER_URL", "")
		l := NewLauncher(WithDevServerURL("http://127.0.0.1:9000")).(*consoleLauncher)
		if !l.isLocal() {
			t.Fatal("expected dev proxy when WithDevServerURL is set")
		}
	})

	t.Run("WithIsLocal overrides env", func(t *testing.T) {
		t.Setenv("SPA_DEV_SERVER_URL", "http://localhost:8000")
		l := NewLauncher(WithIsLocal(func() bool { return false })).(*consoleLauncher)
		if l.isLocal() {
			t.Fatal("expected WithIsLocal(false) to force embedded dist")
		}
	})
}

func findUnderscoreAsset(t *testing.T) string {
	t.Helper()
	var asset string
	err := fs.WalkDir(distFS, "app/dist/assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") && strings.HasSuffix(name, ".js") {
			asset = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embed FS: %v", err)
	}
	if asset == "" {
		t.Fatal("no underscore-prefixed JS asset in embed FS; rebuild app/dist")
	}
	return strings.TrimPrefix(asset, "app/dist")
}

func TestEmbedIncludesUnderscorePrefixedAssets(t *testing.T) {
	// Vite emits chunks like _plugin-vue_export-helper-*.js; go:embed skips "_" files
	// unless the pattern uses the all: prefix.
	findUnderscoreAsset(t)
}

func TestSPAHandler_ServesEmbeddedAsset(t *testing.T) {
	t.Setenv("SPA_DEV_SERVER_URL", "")
	l := NewLauncher(WithIsLocal(func() bool { return false })).(*consoleLauncher)
	if err := l.resolveProdHandler(); err != nil {
		t.Fatalf("resolveProdHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, findUnderscoreAsset(t), nil)
	rec := httptest.NewRecorder()
	if err := l.spaHandler(rec, req); err != nil {
		t.Fatalf("spaHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestNewLauncherImplementsSublauncher(t *testing.T) {
	t.Parallel()
	var _ adkweb.Sublauncher = NewLauncher()
}

func TestRuntimeConfigHandler_AgentsFromLoader(t *testing.T) {
	t.Parallel()

	root := testAgent(t, "root")
	other := testAgent(t, "other")
	loader, err := agent.NewMultiLoader(root, other)
	if err != nil {
		t.Fatalf("NewMultiLoader: %v", err)
	}

	l := &consoleLauncher{branding: &Branding{Title: "Test"}}
	if err := resolveBranding(l.branding, &recordingRegistrar{}); err != nil {
		t.Fatalf("resolveBranding: %v", err)
	}
	handler := l.runtimeConfigHandler(&adklauncher.Config{AgentLoader: loader})

	req := httptest.NewRequest(http.MethodGet, "/assets/config/runtime-config.json", nil)
	rec := httptest.NewRecorder()
	if err := handler(rec, req); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var resp runtimeConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DefaultAgentID != "root" {
		t.Fatalf("defaultAgentId = %q, want root", resp.DefaultAgentID)
	}
	if len(resp.Agents) != 2 {
		t.Fatalf("agents = %v, want 2 entries", resp.Agents)
	}
	if resp.Branding == nil || resp.Branding.Title != "Test" {
		t.Fatalf("branding = %+v, want title Test", resp.Branding)
	}
}
