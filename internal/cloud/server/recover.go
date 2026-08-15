package server

import (
	"net/http"
	"runtime/debug"
)

// recoverPanic (L6): a handler panic must not drop the connection with no trace.
// This middleware — mounted just outside the mux, inside gzip/limit/accessLog so
// the access line still records the resulting 500 and shares its request_id —
// recovers the panic, keeps the server alive, and logs ONE structured
// application-panic line carrying the correlation id + tenant + the recovered
// value + stack. It returns a GENERIC 500 (never the panic detail: panic values
// can hold config paths, unredacted inputs, or a bearer token pulled from a stack
// frame), so nothing sensitive reaches the client; the detail stays in the
// operator-only log.
//
// A panic is distinguishable from a transport disconnect: a client drop does not
// panic (it surfaces as a write error on the normal path — e.g. the export
// mid-stream "aborted" line), so it produces no panic line here.
//
// If the handler already began the response (header or body on the wire), the 500
// envelope cannot be delivered; response_committed is recorded and the envelope
// skipped, so a truncated response is distinguishable in the log from a clean 500.

// committedResponse is implemented by the response writers in the chain that track
// whether the handler has begun the response. Each reports for itself because gzip
// buffers: a handler write still in the gzip buffer is invisible to the outer
// accessRecorder, so only the gzipResponseWriter can see it.
type committedResponse interface{ committed() bool }

func responseCommitted(w http.ResponseWriter) bool {
	if cw, ok := w.(committedResponse); ok {
		return cw.committed()
	}
	return false
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			committed := responseCommitted(w)
			s.log.Error("panic in HTTP handler",
				"panic", rec,
				"stack", string(debug.Stack()),
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", requestID(r),
				"tenant_id", tenantID(r),
				"response_committed", committed,
			)
			if committed {
				// Bytes already on the wire. Returning normally would let net/http
				// FINISH the response (gzip footer + terminating chunk), handing the
				// client a syntactically complete but TRUNCATED body — which a syncing
				// client of /v1/export could mistake for a full snapshot. Abort instead
				// so the client detects the truncation. ErrAbortHandler is net/http's
				// silent-abort signal (we already logged the incident above).
				panic(http.ErrAbortHandler)
			}
			// Generic message only: the recovered value + stack are in the log line
			// above (operators), never the client body.
			s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}
