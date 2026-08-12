package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Request-ID + application access log (C4). Direct-LAN smcloud deployments have no
// access log at all, and even behind Caddy an access entry cannot be tied to the
// application error or tenant it produced. This middleware — OUTERMOST in the chain —
// gives every request a bounded correlation id and emits one access-log line.
//
// The id is an UNTRUSTED correlation LABEL only, NEVER an input to auth or any
// security decision. A caller MAY present X-Request-Id, but it is accepted only in a
// bounded format and replaced with a crypto-random id when absent or invalid. In the
// public deployment Caddy overwrites client-supplied X-Request-Id at the boundary, so
// a spoofed value never reaches here anyway (see docs/smcloud-deploy.md). The resolved
// id is put in the request context (for handler error lines), echoed in the response
// X-Request-Id header, and carried on the access line.

// requestIDPattern bounds an accepted incoming X-Request-Id: 16–64 ASCII
// alphanumeric / '-' / '_'. Anything else is discarded and a fresh id minted.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type reqInfoKey struct{}

// reqInfo is the per-request correlation state: the resolved id, and the tenant once
// auth resolves it. A POINTER in the context, so auth's tenant write is visible to the
// outer access-log middleware after the handler returns (one goroutine, no lock).
type reqInfo struct {
	id     string
	tenant int64
}

func reqInfoFrom(ctx context.Context) *reqInfo {
	info, _ := ctx.Value(reqInfoKey{}).(*reqInfo)
	return info
}

// requestID returns the correlation id for r, or "" if the access-log middleware did
// not run (e.g. a handler invoked directly in a test).
func requestID(r *http.Request) string {
	if info := reqInfoFrom(r.Context()); info != nil {
		return info.id
	}
	return ""
}

func resolveRequestID(r *http.Request) string {
	if v := r.Header.Get("X-Request-Id"); requestIDPattern.MatchString(v) {
		return v
	}
	var b [16]byte
	// crypto/rand.Read effectively never fails on the target platforms; a zero id on
	// the impossible error is still bounded and does no harm (correlation only).
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// accessRecorder records the status + byte count for the access line. Flush + Unwrap
// keep the export handler's http.ResponseController deadline extension working through
// the wrapper (mirrors gzipResponseWriter.Unwrap).
type accessRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func newAccessRecorder(w http.ResponseWriter) *accessRecorder {
	return &accessRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (a *accessRecorder) WriteHeader(code int) {
	if a.wroteHeader {
		return
	}
	a.status, a.wroteHeader = code, true
	a.ResponseWriter.WriteHeader(code)
}

func (a *accessRecorder) Write(b []byte) (int, error) {
	a.wroteHeader = true
	n, err := a.ResponseWriter.Write(b)
	a.bytes += int64(n)
	return n, err
}

func (a *accessRecorder) Flush() {
	if f, ok := a.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *accessRecorder) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// accessLog is the OUTERMOST middleware: it observes every request's final status
// (including the 503 from limitMiddleware and the 401 from auth). /v1/health and
// /v1/version are unauthenticated and polled frequently, so their line stays at Debug
// to keep the default-level access log signal-heavy.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &reqInfo{id: resolveRequestID(r)}
		w.Header().Set("X-Request-Id", info.id) // echo for client/proxy correlation
		rec := newAccessRecorder(w)
		start := time.Now()

		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, info)))

		logAt := s.log.Info
		if r.URL.Path == "/v1/health" || r.URL.Path == "/v1/version" {
			logAt = s.log.Debug
		}
		logAt("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.bytes,
			"request_id", info.id,
			"tenant_id", info.tenant, // 0 for an unauthenticated request
			"remote", clientIP(r),
		)
	})
}

// clientIP returns the caller's address for the access line: the first X-Forwarded-For
// hop when present (Caddy sets it on the VPS), else RemoteAddr with the port stripped.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
