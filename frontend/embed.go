// Package frontend embeds the built Station Manager SPAs into the
// daemon binary. The app SPA's Vite output (app/dist) is compiled in via
// //go:embed; the daemon serves it at /app/.
//
// Build pipeline: `task frontend:build:all` runs `npm run build` in each
// SPA project to populate its dist/ before `go build` reaches the
// embed directive. CI runs the same steps; dist/ is not committed
// (except for a placeholder index.html that ships with the scaffold so
// the embed compiles before the first npm build runs locally).
//
// Three former SPAs are gone — the app SPA is now the SOLE embedded
// operator client (ADR 0044 / W-0003). The legacy logging SPA that once
// owned the root ("/") was retired 2026-07-21 ("/" now redirects to
// "/app/"); the config SPA (once "/config/") and the logbook SPA (once
// "/logbook/") were retired 2026-08-19, and "/config" and "/logbook" now
// 307-redirect to "/app/config" and "/app/logbook". Each source tree was
// deleted once its operator-significant behavior was ported into the app,
// and is preserved under the `legacy-logging-spa-retired`,
// `legacy-config-spa-retired`, and `legacy-logbook-spa-retired` tags (see
// docs/work/W-0003).
//
// See docs/v2-design/frontend-spa.md for the full design.
package frontend

import (
	"embed"
	"io/fs"
)

// appSPA holds the built consolidated app SPA's static assets (ADR 0044)
// — the full-replacement operator client, served by the daemon at the
// /app/ sub-path and the target of the root redirect. The `all:` prefix
// makes the embed pick up dot-prefixed files (Vite emits none today, but
// a future .well-known/ asset would otherwise be silently dropped).
//
//go:embed all:app/dist
var appSPA embed.FS

// AppFS returns the consolidated app SPA's filesystem rooted at the dist/
// directory so http.FileServer treats index.html as the root document.
//
// fs.Sub on a hard-coded valid path is infallible at runtime; an invalid
// embed path (or a missing dist/ directory at compile time) fails the Go
// build, not this call. The function therefore returns only fs.FS — no
// error to plumb.
func AppFS() fs.FS {
	sub, err := fs.Sub(appSPA, "app/dist")
	if err != nil {
		// Unreachable: the embed directive guarantees this path exists and
		// is well-formed. Panicking here would only fire for a programmer
		// error in the embed directive, which we want surfaced loudly.
		panic("frontend: fs.Sub on embedded appSPA failed: " + err.Error())
	}
	return sub
}
