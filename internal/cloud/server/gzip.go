package server

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipMiddleware compresses responses for clients that advertise gzip support
// (2026-07-19, smcloud stamp-drift follow-up): the reconcile manifest is
// O(logbook-size) JSON — ~650 KB uncompressed at 5.7k rows and growing — and
// highly repetitive, so gzip shrinks it ~10×. That matters on the operator's
// metered Malawi link every time reconcile leaves the hash-only fast path.
//
// Server-side only by design: Go's default http.Transport already sends
// Accept-Encoding: gzip and decompresses transparently, so every existing
// daemon benefits with no client change, and a client that doesn't advertise
// gzip gets identity — no lockstep deploy in either direction.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer func() { _ = gz.Close() }()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

// gzipResponseWriter routes body writes through the gzip stream while headers
// and status pass straight to the underlying writer. Handlers here never set
// Content-Length (json.Encoder streams), so no length rewriting is needed.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }
