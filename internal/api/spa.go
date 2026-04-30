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
// that match nothing else."
//
// See docs/v2-design/frontend-spa.md for the full design.
func spaHandler(spa fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(spa))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// http.FS opens paths relative to the FS root. The leading
		// "/" is stripped so e.g. "/assets/main.js" → "assets/main.js".
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
			_ = f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}
