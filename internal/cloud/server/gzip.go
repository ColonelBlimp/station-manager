package server

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// gzipMiddleware compresses responses for clients that accept gzip
// (2026-07-19, smcloud stamp-drift follow-up): the reconcile manifest is
// O(logbook-size) JSON — ~650 KB uncompressed at 5.7k rows and growing — and
// highly repetitive, so gzip shrinks it ~10×. That matters on the operator's
// metered Malawi link every time reconcile leaves the hash-only fast path.
//
// Server-side only by design: Go's default http.Transport already sends
// Accept-Encoding: gzip and decompresses transparently, so every existing
// daemon benefits with no client change, and a client that doesn't accept
// gzip gets identity — no lockstep deploy in either direction.
//
// Vary: Accept-Encoding is emitted on EVERY response (compressed or not) so a
// cache never serves one negotiation's body to the other's request.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		switch negotiateEncoding(r.Header.Get("Accept-Encoding")) {
		case encNotAcceptable:
			// The client refused every coding this server can produce (e.g.
			// "gzip;q=0, identity;q=0") — RFC 9110 §12.5.3 says don't send a
			// refused coding; 406 is the honest answer (review round 2 #3).
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotAcceptable)
			_, _ = w.Write([]byte(`{"code":"not_acceptable","message":"no acceptable content-coding; this server offers gzip and identity"}`))
		case encGzip:
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer func() { _ = gz.Close() }()
			next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
		default:
			// encIdentity — and the safe fallback for any future negotiation
			// value: never emit an encoding that wasn't negotiated.
			next.ServeHTTP(w, r)
		}
	})
}

// contentEncoding is the outcome of Accept-Encoding negotiation.
type contentEncoding int

const (
	encIdentity contentEncoding = iota
	encGzip
	encNotAcceptable
)

// negotiateEncoding resolves an Accept-Encoding header value against the two
// codings this server can produce (gzip, identity), honouring q-values,
// case-insensitive codings, and the * wildcard (RFC 9110 §12.5.3). A bare
// substring test would serve gzip to "gzip;q=0" — an encoding the client
// explicitly REFUSED (2026-07-19 review #3). Rules: an explicit gzip/x-gzip
// or identity entry wins over the wildcard; identity is acceptable BY DEFAULT
// unless refused explicitly or via the wildcard; when every producible coding
// is refused the result is encNotAcceptable → 406 (review round 2 #3). An
// absent/empty header negotiates identity.
func negotiateEncoding(header string) contentEncoding {
	gzipQ, gzipSeen := 0.0, false
	idQ, idSeen := 0.0, false
	starQ, starSeen := 0.0, false
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		coding := strings.ToLower(strings.TrimSpace(fields[0]))
		q := 1.0
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if len(param) > 2 && (param[0] == 'q' || param[0] == 'Q') && param[1] == '=' {
				if f, err := strconv.ParseFloat(param[2:], 64); err == nil {
					q = f
				}
			}
		}
		switch coding {
		case "gzip", "x-gzip":
			gzipSeen, gzipQ = true, q
		case "identity":
			idSeen, idQ = true, q
		case "*":
			starSeen, starQ = true, q
		}
	}
	gzipOK := (gzipSeen && gzipQ > 0) || (!gzipSeen && starSeen && starQ > 0)
	identityOK := true // acceptable by default (RFC 9110 §12.5.3)
	if idSeen {
		identityOK = idQ > 0
	} else if starSeen {
		identityOK = starQ > 0
	}
	switch {
	case gzipOK:
		return encGzip
	case identityOK:
		return encIdentity
	default:
		return encNotAcceptable
	}
}

// gzipResponseWriter routes body writes through the gzip stream while headers
// and status pass straight to the underlying writer. Handlers here never set
// Content-Length (json.Encoder streams), so no length rewriting is needed.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// Unwrap exposes the underlying writer to http.ResponseController (2026-07-19
// review #1): without it, handleExport's SetWriteDeadline extension fails and
// the export falls back to the server-wide 2-minute WriteTimeout — every
// default Go client accepts gzip, so a slow-link full-logbook restore would
// be truncated mid-JSON.
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }
