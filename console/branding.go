package console

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// BrandingURLPrefix is the HTTP path prefix for console-hosted branding assets.
const BrandingURLPrefix = "/console/branding/"

// AssetResolver resolves one branding asset to a browser URL at setup time.
// Implementations that serve local files register routes via registrar.
type AssetResolver interface {
	Resolve(registrar BrandingRouteRegistrar) (publicURL string, err error)
}

// BrandingRouteRegistrar lets asset resolvers mount handlers before the SPA catch-all.
//
// Get registers a route that requires a caller identity in the request context
// (the launcher authorizes but does not authenticate; the web launcher's
// authorization gateway injects the identity from the upstream header).
type BrandingRouteRegistrar interface {
	Get(pattern string, handler http.Handler)
}

type urlAsset struct {
	href string
}

// URLAsset returns a resolver that uses an existing HTTP URL with no route registration.
// Use for CDN links, agent-served paths, or assets already in the embedded SPA dist.
func URLAsset(href string) AssetResolver {
	return urlAsset{href: strings.TrimSpace(href)}
}

func (a urlAsset) Resolve(_ BrandingRouteRegistrar) (string, error) {
	if a.href == "" {
		return "", fmt.Errorf("console: URLAsset: href is required")
	}
	return a.href, nil
}

type embedAsset struct {
	files        fs.FS
	root         string
	relativePath string
}

// EmbedAsset serves a file from fs.FS at BrandingURLPrefix plus the relative path.
func EmbedAsset(files fs.FS, root, relativePath string) AssetResolver {
	return embedAsset{
		files:        files,
		root:         strings.Trim(root, "/"),
		relativePath: cleanRelativePath(relativePath),
	}
}

func (a embedAsset) Resolve(registrar BrandingRouteRegistrar) (string, error) {
	if a.files == nil {
		return "", fmt.Errorf("console: EmbedAsset: files is required")
	}
	if a.relativePath == "" {
		return "", fmt.Errorf("console: EmbedAsset: relativePath is required")
	}

	fsPath := path.Join(append([]string{a.root}, strings.Split(a.relativePath, "/")...)...)
	if err := assetExists(a.files, fsPath); err != nil {
		return "", fmt.Errorf("console: EmbedAsset: %w", err)
	}

	publicPath := BrandingURLPrefix + a.relativePath
	registrar.Get(publicPath, serveFSFile(a.files, fsPath))
	return publicPath, nil
}

type dirAsset struct {
	dir          string
	relativePath string
}

// DirAsset serves a file from a host directory via EmbedAsset(os.DirFS(dir), ".", relativePath).
func DirAsset(dir, relativePath string) AssetResolver {
	return dirAsset{
		dir:          dir,
		relativePath: relativePath,
	}
}

func (a dirAsset) Resolve(registrar BrandingRouteRegistrar) (string, error) {
	if strings.TrimSpace(a.dir) == "" {
		return "", fmt.Errorf("console: DirAsset: dir is required")
	}
	info, err := os.Stat(a.dir)
	if err != nil {
		return "", fmt.Errorf("console: DirAsset: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("console: DirAsset: %q is not a directory", a.dir)
	}
	embed := EmbedAsset(os.DirFS(a.dir), ".", a.relativePath)
	return embed.Resolve(registrar)
}

func resolveBranding(b *Branding, registrar BrandingRouteRegistrar) error {
	if b == nil {
		return nil
	}
	if b.Logo != nil {
		url, err := b.Logo.Resolve(registrar)
		if err != nil {
			return fmt.Errorf("logo: %w", err)
		}
		b.LogoURL = url
	}
	if b.Favicon != nil {
		url, err := b.Favicon.Resolve(registrar)
		if err != nil {
			return fmt.Errorf("favicon: %w", err)
		}
		b.FaviconURL = url
	}
	return nil
}

func cleanRelativePath(p string) string {
	p = path.Clean(strings.TrimPrefix(p, "/"))
	if p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func assetExists(files fs.FS, fsPath string) error {
	f, err := files.Open(fsPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory", fsPath)
	}
	return nil
}

func serveFSFile(files fs.FS, fsPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := files.Open(fsPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if rs, ok := f.(io.ReadSeeker); ok {
			http.ServeContent(w, r, path.Base(fsPath), info.ModTime(), rs)
			return
		}
		data, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if ct := contentTypeForPath(fsPath); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(data)
	})
}

func contentTypeForPath(fsPath string) string {
	switch strings.ToLower(path.Ext(fsPath)) {
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
