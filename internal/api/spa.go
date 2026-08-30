package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves static SPA assets out of the supplied filesystem,
// with the SPA-fallback behaviour required for client-side routing:
// when the requested path doesn't resolve to a real file, the request
// URL is rewritten to "/" so http.FileServer returns index.html, and
// the SPA's router decides what to render. Without this, a refresh on
// /log or /logbook would 404 because no logging-app SPA file at those
// names exists in dist/.
//
// Safe co-existence with server namespaces: Go 1.22+'s http.ServeMux gives
// pattern-matched routes priority over the bare "/" catch-all, so a request
// matching a /v1/* or /debug/pprof/* pattern is dispatched there before
// reaching this handler. At the canonical root, though, EVERY unmatched path
// reaches this catch-all — so a server-namespace path that matched no route (a
// disabled subsystem like bridge/FT8/profiling, or a typo, in bare or subtree
// form) is explicitly 404ed below rather than SPA-falling-through to a 200
// index.html, so API misses stay truthful for curl/script/EventSource/fetch
// consumers.
//
// See docs/v2-design/frontend-spa.md for the full design.
func spaHandler(spa fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(spa))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force revalidation on every SPA asset. The entry bundle has a
		// stable, hash-free name (assets/index.js + index.css — see
		// vite.config.ts), so the filename no longer changes when the SPA is
		// rebuilt; without this header a browser could serve a stale cached
		// bundle after a daemon update. The embed FS carries no modtime/ETag
		// for conditional revalidation, so `no-cache` effectively means
		// "always refetch" — fine for a localhost-served single-operator app.
		w.Header().Set("Cache-Control", "no-cache")
		// http.FS opens paths relative to the FS root. The leading
		// "/" is stripped so e.g. "/assets/main.js" → "assets/main.js".
		// A server-namespace path reaching this catch-all matched no route — a
		// disabled subsystem (bridge/FT8 off, or profiling off) or a typo. Return a
		// real 404 rather than SPA-falling-through to a 200 index.html: /debug/pprof*
		// is a server namespace, never a SPA client route, and a 200 HTML page misleads
		// every curl/script/pprof consumer. This is load-bearing at the canonical root,
		// where there is no /app prefix to quarantine the fallthrough — profiling-off
		// /debug/pprof* would otherwise become SPA HTML instead of the 404 the
		// full-server pprof tests require.
		//
		// /v1/* no longer reaches here: apiRouter (see route_fallback.go) dispatches the
		// whole /v1/ namespace to its own mux and returns the JSON error envelope for an
		// unmatched route (AW-4). The /v1 arm below is kept as a cheap defensive net in
		// case a future root mount is reached directly.
		p := r.URL.Path
		if p == "/v1" || strings.HasPrefix(p, "/v1/") ||
			p == "/debug/pprof" || strings.HasPrefix(p, "/debug/pprof/") {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		f, err := spa.Open(clean)
		if err != nil {
			// Path doesn't resolve to a real file — serve index.html
			// and let the SPA router handle it. (Mutating r.URL.Path
			// is safe here: the request is about to terminate inside
			// fileServer; nothing downstream observes the rewrite.)
			r.URL.Path = "/"
		} else {
			// A real DIRECTORY (e.g. /assets/) has no index.html, so
			// http.FileServer would render a directory listing — a minor
			// disclosure once served over LAN TCP. Treat it as an SPA route too;
			// only a real FILE is served directly.
			if info, serr := f.Stat(); serr == nil && info.IsDir() {
				r.URL.Path = "/"
			}
			_ = f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

// redirectAppToRoot 301-redirects a legacy /app or /app/{path} URL to its
// canonical-root equivalent, preserving the query string, so bookmarks saved
// during the /app/ transition survive the move to the root (W-0003). /app and
// /app/ both go to "/"; /app/config → "/config". Permanent (301): the /app/
// mount is gone for good, so caches and bookmarks should update.
func redirectAppToRoot(w http.ResponseWriter, r *http.Request) {
	// Normalize the suffix to a single-slash LOCAL path. A crafted URL whose path
	// decodes to a leading double slash or backslash (e.g. /app/%2Fevil.example →
	// "/app//evil.example") would otherwise trim to "//evil.example", which
	// http.Redirect emits as a scheme-relative EXTERNAL target — an open redirect,
	// made worse by the permanent (cacheable) 301. Collapsing every leading "/"
	// and "\" to exactly one "/" keeps the destination on this origin.
	suffix := strings.TrimPrefix(r.URL.Path, "/app")
	target := "/" + strings.TrimLeft(suffix, "/\\")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
