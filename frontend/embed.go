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

// configSPA holds the built config SPA's static assets — the
// station-configuration client, served by the daemon under the
// /config/ sub-path while the logging SPA owns the root. Same `all:`
// rationale as loggingSPA.
//
//go:embed all:config/dist
var configSPA embed.FS

// logbookSPA holds the built logbook SPA's static assets — the
// logbook-management client (QSL-awaiting view, edit-history viewer,
// logbook search), served by the daemon under the /logbook/ sub-path
// while the logging SPA owns the root. Same `all:` rationale as
// loggingSPA.
//
//go:embed all:logbook/dist
var logbookSPA embed.FS

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

// ConfigFS returns the config SPA's filesystem rooted at its dist/
// directory. Same infallible-at-runtime contract as LoggingFS — the
// embed directive guarantees the path at compile time.
func ConfigFS() fs.FS {
	sub, err := fs.Sub(configSPA, "config/dist")
	if err != nil {
		panic("frontend: fs.Sub on embedded configSPA failed: " + err.Error())
	}
	return sub
}

// LogbookFS returns the logbook SPA's filesystem rooted at its dist/
// directory. Same infallible-at-runtime contract as LoggingFS — the
// embed directive guarantees the path at compile time.
func LogbookFS() fs.FS {
	sub, err := fs.Sub(logbookSPA, "logbook/dist")
	if err != nil {
		panic("frontend: fs.Sub on embedded logbookSPA failed: " + err.Error())
	}
	return sub
}
