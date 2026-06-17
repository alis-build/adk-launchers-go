package console

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testLogoSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)

type recordingRegistrar struct {
	routes map[string]http.Handler
}

func (r *recordingRegistrar) Get(pattern string, handler http.Handler) {
	if r.routes == nil {
		r.routes = make(map[string]http.Handler)
	}
	r.routes[pattern] = handler
}

func TestURLAsset(t *testing.T) {
	t.Parallel()

	url, err := URLAsset("/my-agent/favicon.ico").Resolve(&recordingRegistrar{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if url != "/my-agent/favicon.ico" {
		t.Fatalf("url = %q", url)
	}
}

func TestURLAsset_Empty(t *testing.T) {
	t.Parallel()

	if _, err := URLAsset("").Resolve(&recordingRegistrar{}); err == nil {
		t.Fatal("expected error for empty href")
	}
}

func TestEmbedAsset(t *testing.T) {
	t.Parallel()

	reg := &recordingRegistrar{}
	files := fstestMapFS{
		"branding/logo.svg": testLogoSVG,
	}

	url, err := EmbedAsset(files, "branding", "logo.svg").Resolve(reg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantURL := BrandingURLPrefix + "logo.svg"
	if url != wantURL {
		t.Fatalf("url = %q, want %q", url, wantURL)
	}

	handler, ok := reg.routes[wantURL]
	if !ok {
		t.Fatalf("route %q not registered, got %v", wantURL, reg.routes)
	}

	req := httptest.NewRequest(http.MethodGet, wantURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestEmbedAsset_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := EmbedAsset(fstestMapFS{}, "branding", "missing.svg").Resolve(&recordingRegistrar{})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDirAsset(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), testLogoSVG, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	reg := &recordingRegistrar{}
	url, err := DirAsset(dir, "logo.svg").Resolve(reg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if url != BrandingURLPrefix+"logo.svg" {
		t.Fatalf("url = %q", url)
	}
}

func TestResolveBranding_MixedSources(t *testing.T) {
	t.Parallel()

	files := fstestMapFS{"branding/logo.svg": testLogoSVG}
	b := Branding{
		Title:       "Test Agent V1",
		DisplayName: "Test Agent V1",
		Favicon:     URLAsset("/my-agent/favicon.ico"),
		Logo:        EmbedAsset(files, "branding", "logo.svg"),
	}
	reg := &recordingRegistrar{}

	if err := resolveBranding(&b, reg); err != nil {
		t.Fatalf("resolveBranding: %v", err)
	}
	if b.FaviconURL != "/my-agent/favicon.ico" {
		t.Fatalf("FaviconURL = %q", b.FaviconURL)
	}
	if b.LogoURL != BrandingURLPrefix+"logo.svg" {
		t.Fatalf("LogoURL = %q", b.LogoURL)
	}
}

func TestResolveBranding_TitleOnlyOmitsAssetURLs(t *testing.T) {
	t.Parallel()

	b := Branding{Title: "Test"}
	if err := resolveBranding(&b, &recordingRegistrar{}); err != nil {
		t.Fatalf("resolveBranding: %v", err)
	}
	if b.LogoURL != "" || b.FaviconURL != "" {
		t.Fatalf("expected empty asset URLs, got logo=%q favicon=%q", b.LogoURL, b.FaviconURL)
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["logoUrl"] != "" || payload["faviconUrl"] != "" {
		t.Fatalf("unexpected asset URLs in JSON: %v", payload)
	}
	if payload["title"] != "Test" {
		t.Fatalf("title = %q", payload["title"])
	}
}

type fstestMapFS map[string][]byte

func (m fstestMapFS) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &mapFile{data: data, name: name}, nil
}

type mapFile struct {
	data   []byte
	name   string
	offset int
}

func (f *mapFile) Stat() (fs.FileInfo, error) {
	return mapFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *mapFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	return n, nil
}

func (f *mapFile) Seek(offset int64, whence int) (int64, error) {
	var absolute int64
	switch whence {
	case io.SeekStart:
		absolute = offset
	case io.SeekCurrent:
		absolute = int64(f.offset) + offset
	case io.SeekEnd:
		absolute = int64(len(f.data)) + offset
	default:
		return 0, fmt.Errorf("invalid whence")
	}
	if absolute < 0 || absolute > int64(len(f.data)) {
		return 0, fmt.Errorf("invalid offset")
	}
	f.offset = int(absolute)
	return absolute, nil
}

func (f *mapFile) Close() error { return nil }

type mapFileInfo struct {
	name string
	size int64
}

func (i mapFileInfo) Name() string       { return i.name }
func (i mapFileInfo) Size() int64        { return i.size }
func (i mapFileInfo) Mode() fs.FileMode  { return 0o644 }
func (i mapFileInfo) ModTime() time.Time { return time.Time{} }
func (i mapFileInfo) IsDir() bool        { return false }
func (i mapFileInfo) Sys() any           { return nil }
