// Package console implements an ADK web sublauncher that serves the Vue operator
// console (embedded SPA), deploy-time runtime configuration, and /auth/me.
//
// # Registration order
//
// Pass console last when composing launchers so the SPA catch-all on GET /
// does not shadow agui, scheduler, or other host routes:
//
//	web.NewLauncher(
//	    agui.NewLauncher("my-agent", agui.WithThreadService(historySvc)),
//	    scheduler.NewLauncher("my-agent", schedSvc),
//	    console.NewLauncher(console.WithBranding(console.Branding{
//	        Title:       "My Console",
//	        DisplayName: "My Console",
//	        Favicon:     console.URLAsset("/my-agent/branding/favicon.ico"),
//	        Logo:        console.EmbedAsset(brandingFS, "branding", "logo.svg"),
//	    })),
//	)
//
// # Branding
//
// WithBranding accepts Title and DisplayName as plain strings. Logo and Favicon
// are optional AssetResolver values resolved at setup into logoUrl and faviconUrl
// in runtime-config.json. Nil resolvers omit those URLs; the SPA uses bundled
// defaults (/logo.svg and index.html favicon).
//
// Out-of-the-box resolvers:
//   - URLAsset(href) — existing HTTP path or external URL (agent routes, CDN, dist)
//   - EmbedAsset(fs, root, relativePath) — go:embed files at /console/branding/...
//   - DirAsset(dir, relativePath) — host directory files at /console/branding/...
//
// Register agent branding routes before console so paths like /my-agent/... are
// not shadowed by the SPA catch-all.
//
// # Runtime configuration
//
// GET /assets/config/runtime-config.json returns agents and defaultAgentId from
// launcher.Config.AgentLoader.ListAgents() and RootAgent().Name(), plus resolved
// shell branding from WithBranding when set.
//
// # Local development
//
// By default the launcher serves the embedded app/dist build (rebuild with
// cd console/app && pnpm build and recompile Go). For HMR, run Vite and set
// SPA_DEV_SERVER_URL so GET / proxies to the dev server with mux auth:
//
//	cd console/app && pnpm dev
//	SPA_DEV_SERVER_URL=http://localhost:8000 adk web --port 8080 agui scheduler console
//
// Override explicitly with WithIsLocal(func() bool { return true }) or
// WithIsLocal(func() bool { return false }) when composing NewLauncher.
//
// For frontend-only work on the Vite port, see console/app/vite.config.ts — it
// forwards /agui and JSON-RPC paths to AGENT_HOST (default http://localhost:8080).
//
// # Embedded SPA build
//
// app/dist is checked in for go:embed (all: prefix so Vite "_" chunks are included).
// Regenerate with go generate ./console/...
// or cd console/app && pnpm build. The Husky pre-commit hook rebuilds dist when
// console/app changes are committed.
package console
