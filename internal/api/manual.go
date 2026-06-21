package api

import (
	"io/fs"
	"net/http"
)

// manualHandler serves the embedded operator manual — a single self-contained,
// zero-JS static page (ADR 0036) — out of the supplied filesystem.
//
// Unlike spaHandler there is NO client-side-router fallback: the manual is plain
// static files, so a path that doesn't resolve is a genuine 404, not a rewrite
// to index.html. Mounted under /manual/ (a more-specific subtree than the SPA's
// "/" catch-all), so it takes priority for /manual/* and a bare /manual gets the
// ServeMux trailing-slash redirect.
func manualHandler(site fs.FS) http.Handler {
	fileServer := http.FileServerFS(site)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always revalidate: the embedded manual is rebuilt with the daemon, so
		// a stale cached copy after an update would misdescribe the running
		// version. The embed FS carries no ETag, so no-cache means refetch —
		// fine for a localhost-served single-operator app (matches spaHandler).
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
