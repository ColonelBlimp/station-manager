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
// Safe co-existence with /v1/* handlers: Go 1.22+'s http.ServeMux gives
// pattern-matched routes priority over the bare "/" catch-all, so any
// request matching a /v1/* pattern is dispatched there before reaching
// this handler. The catch-all is therefore naturally bounded to "paths
// that match nothing else." A /v1/* path that matches NO registered
// pattern (a disabled subsystem like bridge/FT8, or a typo) DOES reach
// here — and gets an honest 404, not an SPA-fallback 200, so API misses
// stay truthful for curl/script/EventSource/fetch consumers.
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
		// A /v1/* path reaching this catch-all matched no API route — a disabled
		// subsystem (bridge/FT8 off) or a typo. Return a real 404 rather than
		// SPA-falling-through to a 200 index.html: /v1/* is the API namespace,
		// never an SPA client route, and a 200 HTML page misleads every
		// curl/script/EventSource/fetch consumer (a disabled GET /v1/rig/events
		// would otherwise 200-HTML; POST /v1/rig/command would 405 vs GET /).
		if strings.HasPrefix(r.URL.Path, "/v1/") {
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
