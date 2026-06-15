package console

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:app/dist
var distFS embed.FS

const spaDistDirEnv = "SPA_DIST_DIR"

// DistResolver supplies the production SPA http.Handler at setup time.
// Resolve is called once during SetupHostRoutes; the returned handler serves
// all GET / requests when the launcher is not in dev mode. Dev mode
// (SPA_DEV_SERVER_URL / WithDevServerURL / WithIsLocal) always takes precedence
// and bypasses the resolver entirely.
//
// Built-in implementations:
//   - [DefaultDist] — embedded app/dist; zero-config default.
//   - [DirDist] — host directory; for Docker/CI build output.
//   - [EmbedDist] — caller's own go:embed; for library consumers.
//   - [HandlerDist] — existing http.Handler; for maximum control.
type DistResolver interface {
	Resolve() (http.Handler, error)
}

// DefaultDist returns a resolver that serves the bundled console/app/dist from
// go:embed. This is the zero-config default used when neither WithDist nor
// SPA_DIST_DIR is set. Prefer this when using the stock console SPA without
// customization.
func DefaultDist() DistResolver {
	return defaultDist{}
}

// DirDist returns a resolver that serves files from a host directory. Use this
// when the SPA is built at Docker image build time (or CI) and copied alongside
// the Go binary, rather than embedded at compile time.
//
//	console.NewLauncher(console.WithDist(console.DirDist("dist")))
//
// The directory must contain an index.html; validation happens at Resolve time.
// Leading/trailing whitespace in dir is trimmed before use.
func DirDist(dir string) DistResolver {
	return dirDist{dir: dir}
}

// EmbedDist returns a resolver that serves from a caller-provided fs.FS. Use
// this when a consumer module embeds its own custom SPA build via go:embed and
// wants compile-time bundling (like DefaultDist) but with different assets.
//
//	//go:embed my-console/dist
//	var myDist embed.FS
//	console.NewLauncher(console.WithDist(console.EmbedDist(myDist, "my-console/dist")))
//
// root is the subdirectory within files that contains index.html.
func EmbedDist(files fs.FS, root string) DistResolver {
	return embedDist{
		files: files,
		root:  strings.Trim(root, "/"),
	}
}

// HandlerDist returns a resolver that wraps an existing http.Handler. Use this
// when you already have a configured file server or need full control over
// response headers, caching, or middleware. The handler is wrapped with SPA
// fallback so extensionless paths (e.g. /threads) are rewritten to / before
// delegation.
//
//	spaHandler := http.FileServer(http.Dir("dist"))
//	console.NewLauncher(console.WithDist(console.HandlerDist(spaHandler)))
//
// Prefer DirDist or EmbedDist when a simple file server suffices; HandlerDist
// skips index.html validation at resolve time since the handler is opaque.
func HandlerDist(h http.Handler) DistResolver {
	return handlerDist{h: h}
}

// WithDist sets the production dist source. Resolution priority when the
// launcher is not in dev mode:
//
//  1. WithDist resolver (if set)
//  2. SPA_DIST_DIR env var → DirDist(dir)
//  3. DefaultDist (embedded app/dist)
func WithDist(r DistResolver) Option {
	return func(l *consoleLauncher) {
		l.distResolver = r
	}
}

// defaultDist resolves the go:embed app/dist filesystem into an SPA handler.
type defaultDist struct{}

// Resolve extracts the embedded app/dist subtree, validates that index.html
// exists, and returns a file server with SPA history-mode fallback.
func (defaultDist) Resolve() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "app/dist")
	if err != nil {
		return nil, fmt.Errorf("console: DefaultDist: embed app/dist: %w", err)
	}
	if err := validateDistFS(sub); err != nil {
		return nil, fmt.Errorf("console: DefaultDist: %w", err)
	}
	return fileServerFromFS(sub), nil
}

// dirDist resolves a host directory into an SPA handler.
type dirDist struct {
	dir string
}

// Resolve trims whitespace from the directory path, validates that the
// directory and its index.html exist, and returns a file server backed by
// os.DirFS with SPA history-mode fallback.
func (d dirDist) Resolve() (http.Handler, error) {
	dir := strings.TrimSpace(d.dir)
	if err := validateDistDir(dir); err != nil {
		return nil, fmt.Errorf("console: DirDist: %w", err)
	}
	return fileServerFromFS(os.DirFS(dir)), nil
}

// embedDist resolves a caller-provided fs.FS into an SPA handler.
type embedDist struct {
	files fs.FS
	root  string
}

// Resolve extracts the subtree at root from files, validates index.html, and
// returns a file server with SPA history-mode fallback.
func (d embedDist) Resolve() (http.Handler, error) {
	if d.files == nil {
		return nil, fmt.Errorf("console: EmbedDist: files is required")
	}
	sub, err := fs.Sub(d.files, d.root)
	if err != nil {
		return nil, fmt.Errorf("console: EmbedDist: %w", err)
	}
	if err := validateDistFS(sub); err != nil {
		return nil, fmt.Errorf("console: EmbedDist: %w", err)
	}
	return fileServerFromFS(sub), nil
}

// handlerDist wraps a caller-provided http.Handler with SPA fallback.
type handlerDist struct {
	h http.Handler
}

// Resolve validates that the handler is non-nil and wraps it with SPA
// history-mode fallback so extensionless paths (e.g. /threads) are rewritten
// to / before being served.
func (d handlerDist) Resolve() (http.Handler, error) {
	if d.h == nil {
		return nil, fmt.Errorf("console: HandlerDist: handler is required")
	}
	return withSPAFallback(d.h), nil
}

// withSPAFallback wraps h so that requests for extensionless paths (Vue
// history-mode routes like /threads) are rewritten to / before delegation.
// Used by HandlerDist where the underlying handler is opaque; FS-based
// resolvers use fileServerFromFS instead.
func withSPAFallback(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Ext(r.URL.Path) == "" {
			cloned := *r.URL
			cloned.Path = "/"
			r2 := *r
			r2.URL = &cloned
			r = &r2
		}
		h.ServeHTTP(w, r)
	})
}

// fileServerFromFS builds an SPA-aware file server from an fs.FS. Requests for
// extensioned paths (e.g. /assets/app.js) are served directly if the file
// exists, otherwise 404. Extensionless paths (e.g. /, /threads/123) fall back
// to index.html for Vue history-mode routing. The index handler is cached once
// at construction time.
func fileServerFromFS(sub fs.FS) http.Handler {
	indexHandler := serveFSFile(sub, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && path.Ext(name) != "" {
			if err := assetExists(sub, name); err != nil {
				http.NotFound(w, r)
				return
			}
			serveFSFile(sub, name).ServeHTTP(w, r)
			return
		}
		indexHandler.ServeHTTP(w, r)
	})
}

// validateDistFS checks that index.html exists and is a regular file in sub.
func validateDistFS(sub fs.FS) error {
	if err := assetExists(sub, "index.html"); err != nil {
		return fmt.Errorf("index.html: %w", err)
	}
	return nil
}

// validateDistDir checks that dir is a non-empty path pointing to an existing
// directory that contains an index.html regular file.
func validateDistDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("dir is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}
	index := filepath.Join(dir, "index.html")
	info, err = os.Stat(index)
	if err != nil {
		return fmt.Errorf("index.html: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("index.html is a directory")
	}
	return nil
}

// effectiveDistResolver returns the active DistResolver based on priority:
// WithDist (explicit), then SPA_DIST_DIR env var, then DefaultDist (embedded).
func (l *consoleLauncher) effectiveDistResolver() DistResolver {
	if l.distResolver != nil {
		return l.distResolver
	}
	if dir := os.Getenv(spaDistDirEnv); dir != "" {
		return DirDist(dir)
	}
	return DefaultDist()
}

// resolveProdHandler resolves the effective DistResolver into prodHandler.
// Idempotent: subsequent calls are no-ops once prodHandler is set. Called from
// mountHostRoutes before route registration.
func (l *consoleLauncher) resolveProdHandler() error {
	if l.prodHandler != nil {
		return nil
	}
	h, err := l.effectiveDistResolver().Resolve()
	if err != nil {
		return err
	}
	l.prodHandler = h
	return nil
}

// distSourceMessage returns a human-readable description of the active dist
// source for UserMessage startup output.
func (l *consoleLauncher) distSourceMessage() string {
	if l.distResolver != nil {
		return "serving custom SPA dist"
	}
	if dir := os.Getenv(spaDistDirEnv); dir != "" {
		return fmt.Sprintf("serving SPA dist from %s=%s", spaDistDirEnv, dir)
	}
	return "serving embedded app/dist"
}
