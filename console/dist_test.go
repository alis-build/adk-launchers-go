package console

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestDirDist_ServesIndex(t *testing.T) {
	t.Parallel()

	dir := writeDistIndex(t, "<html>custom</html>")
	h, err := DirDist(dir).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestDirDist_SPAFallback(t *testing.T) {
	t.Parallel()

	dir := writeDistIndex(t, "<html>custom</html>")
	h, err := DirDist(dir).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for SPA fallback", rec.Code)
	}
	if body := rec.Body.String(); body != "<html>custom</html>" {
		t.Fatalf("body = %q, want custom index.html", body)
	}
}

func TestDirDist_MissingAsset(t *testing.T) {
	t.Parallel()

	dir := writeDistIndex(t, "<html>custom</html>")
	h, err := DirDist(dir).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing extensioned file", rec.Code)
	}
}

func TestDirDist_EmptyDir(t *testing.T) {
	t.Parallel()

	_, err := DirDist("").Resolve()
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestDirDist_WhitespaceDir(t *testing.T) {
	t.Parallel()

	_, err := DirDist("   ").Resolve()
	if err == nil {
		t.Fatal("expected error for whitespace-only dir")
	}
}

func TestDirDist_MissingDir(t *testing.T) {
	t.Parallel()

	_, err := DirDist(filepath.Join(t.TempDir(), "missing")).Resolve()
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestDirDist_MissingIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := DirDist(dir).Resolve()
	if err == nil {
		t.Fatal("expected error for missing index.html")
	}
}

func TestEmbedDist_ServesIndex(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>embed</html>")},
	}
	h, err := EmbedDist(files, ".").Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestEmbedDist_SPAFallback(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa</html>")},
	}
	h, err := EmbedDist(files, ".").Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for SPA fallback", rec.Code)
	}
}

func TestEmbedDist_MissingIndex(t *testing.T) {
	t.Parallel()

	_, err := EmbedDist(fstest.MapFS{}, ".").Resolve()
	if err == nil {
		t.Fatal("expected error for missing index.html")
	}
}

func TestEmbedDist_NilFiles(t *testing.T) {
	t.Parallel()

	_, err := EmbedDist(nil, ".").Resolve()
	if err == nil {
		t.Fatal("expected error for nil files")
	}
}

func TestHandlerDist_Nil(t *testing.T) {
	t.Parallel()

	_, err := HandlerDist(nil).Resolve()
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestHandlerDist_Delegates(t *testing.T) {
	t.Parallel()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	h, err := HandlerDist(inner).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if !called {
		t.Fatal("expected inner handler to be called")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}

func TestDefaultDist_ServesEmbedded(t *testing.T) {
	t.Parallel()

	h, err := DefaultDist().Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, findUnderscoreAsset(t), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
}

func TestEffectiveDistResolver_Env(t *testing.T) {
	t.Setenv(spaDistDirEnv, t.TempDir())
	l := &consoleLauncher{}
	resolver := l.effectiveDistResolver()
	if _, ok := resolver.(dirDist); !ok {
		t.Fatalf("expected dirDist, got %T", resolver)
	}
}

func TestWithDist_OverridesEnv(t *testing.T) {
	t.Setenv(spaDistDirEnv, t.TempDir())
	l := &consoleLauncher{distResolver: DefaultDist()}
	resolver := l.effectiveDistResolver()
	if _, ok := resolver.(defaultDist); !ok {
		t.Fatalf("expected defaultDist, got %T", resolver)
	}
}

func TestResolveProdHandler_CustomDist(t *testing.T) {
	t.Parallel()

	dir := writeDistIndex(t, "<html>from-dir</html>")
	l := NewLauncher(WithDist(DirDist(dir)), WithIsLocal(func() bool { return false })).(*consoleLauncher)
	if err := l.resolveProdHandler(); err != nil {
		t.Fatalf("resolveProdHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	if err := l.spaHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatalf("spaHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "<html>from-dir</html>" {
		t.Fatalf("body = %q, want custom dist", body)
	}
}

func TestSPAHandler_DevProxyIgnoresDist(t *testing.T) {
	t.Setenv("SPA_DEV_SERVER_URL", "http://localhost:8000")
	dir := writeDistIndex(t, "<html>ignored</html>")
	l := NewLauncher(WithDist(DirDist(dir))).(*consoleLauncher)
	if !l.isLocal() {
		t.Fatal("expected dev proxy when SPA_DEV_SERVER_URL is set")
	}
}

func TestDistSourceMessage_Default(t *testing.T) {
	t.Parallel()

	l := &consoleLauncher{}
	if msg := l.distSourceMessage(); msg != "serving embedded app/dist" {
		t.Fatalf("distSourceMessage = %q, want default", msg)
	}
}

func TestDistSourceMessage_WithDist(t *testing.T) {
	t.Parallel()

	l := &consoleLauncher{distResolver: DefaultDist()}
	if msg := l.distSourceMessage(); msg != "serving custom SPA dist" {
		t.Fatalf("distSourceMessage = %q, want custom", msg)
	}
}

func TestDistSourceMessage_Env(t *testing.T) {
	t.Setenv(spaDistDirEnv, "/some/dir")
	l := &consoleLauncher{}
	msg := l.distSourceMessage()
	if msg != "serving SPA dist from SPA_DIST_DIR=/some/dir" {
		t.Fatalf("distSourceMessage = %q, want env message", msg)
	}
}

func writeDistIndex(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(content), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}
