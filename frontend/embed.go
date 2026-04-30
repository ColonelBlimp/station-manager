// Package frontend embeds the built Station Manager SPAs into the
// daemon binary. Each SPA's Vite output (logging/dist, logbook/dist, …)
// is compiled in via //go:embed; the daemon serves them at the
// appropriate URL prefixes.
//
// Build pipeline: `task frontend:build` runs `npm run build` in each
// SPA project to populate its dist/ before `go build` reaches the
// embed directive. CI runs the same steps; dist/ is not committed
// (except for a placeholder index.html that ships with the scaffold so
// the embed compiles before the first npm build runs locally).
//
// See docs/v2-design/frontend-spa.md for the full design.
package frontend

import (
	"embed"
	"io/fs"
)

// loggingSPA holds the built logging SPA's static assets. The
// `all:` prefix makes the embed pick up dot-prefixed files (Vite
// emits none today, but a future .well-known/ asset would otherwise
// be silently dropped).
//
//go:embed all:logging/dist
var loggingSPA embed.FS

// LoggingFS returns the logging SPA's filesystem rooted at the dist/
// directory so http.FileServer treats index.html as the root document.
//
// fs.Sub on a hard-coded valid path is infallible at runtime; an
// invalid embed path (or a missing dist/ directory at compile time)
// fails the Go build, not this call. The function therefore returns
// only fs.FS — no error to plumb.
func LoggingFS() fs.FS {
	sub, err := fs.Sub(loggingSPA, "logging/dist")
	if err != nil {
		// Unreachable: the embed directive guarantees this path
		// exists and is well-formed. Panicking here would only
		// fire for a programmer error in the embed directive,
		// which we want surfaced loudly.
		panic("frontend: fs.Sub on embedded loggingSPA failed: " + err.Error())
	}
	return sub
}
