package api

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// sseEventsPath is the daemon-firehose SSE endpoint.
const sseEventsPath = "/v1/events"

// isSSEPath reports whether p is one of the long-lived Server-Sent-Events
// endpoints. ALL of them are exempt from limitConcurrent (the general
// concurrent-request cap): an SSE connection is held for the stream's lifetime,
// so counting it against that cap would let a few EventSource connections
// (e.g. several browser tabs each opening rig + FT8 streams) exhaust the request
// budget and 503 unrelated calls — review internal-api M2. They are bounded
// separately by limitEventSubscribers. Previously only /v1/events was exempted,
// leaving /v1/rig/events + /v1/ft8/events holding ordinary request slots.
func isSSEPath(p string) bool {
	switch p {
	case sseEventsPath, "/v1/rig/events", "/v1/ft8/events":
		return true
	default:
		return false
	}
}

// httpErrorLogWriter adapts net/http's *log.Logger sink onto the structured
// logger, so the server's own diagnostics reach smd.log instead of stderr. One
// Write is one log record: net/http emits a complete line per diagnostic, and
// the trailing newline is stripped so the field holds the message alone.
//
// It never returns an error — reporting a failure back into net/http's error
// path is how a logging fault becomes a serving fault.
type httpErrorLogWriter struct {
	logger *logging.Service
}

func (w *httpErrorLogWriter) Write(p []byte) (int, error) {
	w.logger.WarnWith().
		Str("error", strings.TrimRight(string(p), "\n")).
		Msg("http server error")
	return len(p), nil
}

// recoverPanic wraps an http.Handler with a panic safety net.
//
// Go's net/http has implicit per-request recovery, but it logs via
// the stdlib logger, closes the connection without a response body,
// and is invisible to our structured logging pipeline. This
// middleware catches the panic, logs it through logging.Service
// with a stack trace plus request method and path, and writes a
// proper 500-error envelope so the client gets a response in the
// same shape as every other 4xx/5xx.
//
// Double-response caveat: if the wrapped handler has already called
// w.WriteHeader (or implicitly via w.Write) before panicking, the
// 500 envelope cannot be delivered — the client sees whatever partial
// bytes made it out first. That case is now detected (via the shared
// responseRecorder): the panic line records response_committed=true and
// the doomed envelope is skipped, so a truncated response is
// distinguishable from a clean 500 and the access-log classification is
// tagged (A6). Recovery's primary job remains keeping the daemon alive
// and logging the incident, not guaranteeing a clean response shape.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	const op errors.Op = "api.recoverPanic"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// If the wrapped handler already committed the response (header sent,
			// maybe a partial body), the 500 envelope cannot be delivered — writing it
			// now only appends a JSON body onto the partial bytes (net/http drops the
			// superfluous header). Detect that from the shared responseRecorder so the
			// panic line can distinguish a clean 500 from a truncated response — which
			// the access log's status alone cannot, since a mid-stream panic shows
			// there as whatever was written first, e.g. 200 (A6).
			rr, isRecorder := w.(*responseRecorder)
			committed := isRecorder && rr.wroteHeader
			s.logger.ErrorWith().
				Interface("panic", rec).
				Str("stack", string(debug.Stack())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Bool("response_committed", committed).
				Msg("panic in HTTP handler")
			if committed {
				// Already on the wire — don't garble it with a second envelope. Tag the
				// access-log classification instead, so a request logged there as status
				// 200 is still connected to this panic.
				rr.NoteError("internal_error", "panic after response partially written", string(op))
				return
			}
			// Deliberately generic message — panic values can contain
			// sensitive internals (config paths, stack slices,
			// unredacted inputs). The full detail lives in the log
			// line above, which only operators see.
			s.writeError(w, http.StatusInternalServerError,
				"internal_error", "internal server error", op)
		}()
		next.ServeHTTP(w, r)
	})
}

// rejectWhenDraining short-circuits NEW requests with 503 once graceful
// shutdown has begun (s.draining, set by StopAccepting). StopAccepting closes
// the listener and disables keep-alives, but a client holding an existing
// keep-alive connection can still send a request during the multi-second
// subsystem teardown that runs before http.Server.Shutdown drains — and that
// request would otherwise reach a stopping/stopped subsystem (bridge, ft8,
// forwarder workers). In-flight requests are unaffected; only new ones are
// turned away. Sits outside limitConcurrent so a drained request never consumes
// a concurrency slot (review 2026-07-22 #3).
func (s *Server) rejectWhenDraining(next http.Handler) http.Handler {
	const op errors.Op = "api.rejectWhenDraining"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			s.writeError(w, http.StatusServiceUnavailable, "shutting_down",
				"the daemon is shutting down", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitConcurrent caps the number of simultaneous non-SSE requests
// in flight. Over-budget requests get 503 server_busy; the daemon
// does not queue. SSE (/v1/events) is exempt and counted separately
// by limitEventSubscribers — see docs/v2-design/api.md §6 for the
// threat model. Non-blocking acquire is deliberate: piling up
// goroutines waiting for a slot is exactly the failure mode this
// middleware is meant to prevent.
func (s *Server) limitConcurrent(next http.Handler) http.Handler {
	const op errors.Op = "api.limitConcurrent"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSSEPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ok, release := s.limits.acquireConcurrent()
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable,
				"server_busy", "daemon is at its concurrent-request limit; retry shortly", op)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

// limitEventSubscribers caps simultaneous SSE subscribers on the
// /v1/events endpoint. When full, the handler returns 503 rather
// than accepting the connection; a subscriber slot is released when
// the handler returns.
func (s *Server) limitEventSubscribers(next http.Handler) http.Handler {
	const op errors.Op = "api.limitEventSubscribers"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, release := s.limits.acquireSubscriber()
		if !ok {
			s.writeError(w, http.StatusServiceUnavailable,
				"server_busy", "event subscriber limit reached; retry shortly", op)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

// limitSubmitRate applies a token-bucket rate limit to the 'submit'
// endpoint (POST /v1/qso). Over-budget requests get 429 rate_limited.
// This is a per-endpoint cap on top of limitConcurrent because the
// 'submit' path is the single hottest thing a buggy client can stampede.
func (s *Server) limitSubmitRate(next http.Handler) http.Handler {
	const op errors.Op = "api.limitSubmitRate"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limits.allowSubmit(time.Now()) {
			s.writeError(w, http.StatusTooManyRequests,
				"rate_limited", "submit rate exceeded; slow down", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// responseRecorder wraps http.ResponseWriter so the outer access-log
// middleware can observe the status code and bytes written. Status
// defaults to 200 to mirror net/http's implicit-WriteHeader behaviour
// for handlers that call w.Write without an explicit WriteHeader.
//
// errCode / errMessage / errOp carry the 4xx/5xx envelope fields up
// to logRequests so the access-log line can name *what* the failure
// was, not just its HTTP status. writeError / writeServerError stash
// them via noteError before writing the JSON body. For 5xx, the
// detailed wrapped-error chain is also logged at ERR level by
// writeServerError itself; the access-log line is the request-level
// summary.
//
// Flush is implemented so SSE handlers (which assert http.Flusher on
// the writer) keep working through the wrapper. Other writer
// interfaces (Hijacker, Pusher) are not in use today; if they're
// added later, mirror the Flush pattern.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool

	errCode    string
	errMessage string
	errOp      string
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

// NoteError records the error envelope's classification. First call
// wins (handlers should only call writeError once per request; the
// guard is defence-in-depth). Exported to satisfy httpkit.ErrorNoter so
// httpkit.WriteError can stash the classification without importing this
// package (ADR 0043).
func (r *responseRecorder) NoteError(code, message, op string) {
	if r.errCode == "" {
		r.errCode = code
		r.errMessage = message
		r.errOp = op
	}
}

func (r *responseRecorder) WriteHeader(code int) {
	// First WriteHeader wins, mirroring net/http: a later call is a no-op (and
	// forwarding it would trigger net/http's "superfluous WriteHeader" warning),
	// so return before touching the wrapped writer again.
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// Mirror net/http: an implicit 200 is committed on first write.
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap is required by http.ResponseController so that SetWriteDeadline /
// SetReadDeadline / hijack-style operations can traverse the wrapper and
// reach the underlying connection. Without it the controller returns
// ErrNotSupported and the SSE handlers' per-write deadline arming
// (handler_events.go) fails — long-lived streams then silently inherit the
// server-wide WriteTimeout, which manifests as "SSE connection closes every
// WriteTimeoutSec seconds and the SPA misses any pushes during the
// reconnect window."
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// clientIP returns a best-effort representation of the request's
// originating address. Strips the port off RemoteAddr; honours
// X-Forwarded-For's first hop when present (LAN reverse-proxy
// scenarios). Defence-in-depth: the daemon doesn't trust XFF for any
// security decision today, so this is purely for log breadcrumbs.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma >= 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	addr := r.RemoteAddr
	if colon := strings.LastIndexByte(addr, ':'); colon >= 0 {
		return addr[:colon]
	}
	return addr
}

// logRequests is the outermost middleware: it observes every HTTP
// request the daemon receives and emits one structured INF line on
// completion. Mounted outside recoverPanic and limitConcurrent so it
// captures all four shapes of completion uniformly:
//
//   - 2xx / 3xx / 4xx from a normal handler return
//   - 5xx from writeServerError (the wrapped err is also logged at
//     ERR by writeServerError itself; the access-log line here is
//     the request-level summary)
//   - 503 from limitConcurrent or limitEventSubscribers
//   - 500 from recoverPanic catching a panic
//
// The line shape is the canonical web-access-log set: method, path,
// status, duration_ms, remote_addr, plus bytes_written for body-size
// observability. Operators grep `status:5` for any 5xx, `status:4`
// for any 4xx — the level stays Info uniformly so a structured
// extractor can pivot on the field rather than the level.
//
// SSE caveat: a long-lived /v1/events connection is logged once at
// disconnect with a large duration (the connection lifetime). That's
// the right behaviour: the access log records what happened, and
// "happened" for an SSE connection means "the connection ended."
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseRecorder(w)
		// Debug-level breadcrumb at handler entry. Pairs with the
		// info-level completion line below so a debug-configured
		// daemon shows request-in / request-out, while info-only
		// stays at completion-only. No-op when level >= info because
		// zerolog drops the event before any field methods run.
		s.logger.DebugWith().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", clientIP(r)).
			Msg("http request received")
		start := time.Now()
		next.ServeHTTP(rec, r)
		duration := time.Since(start)

		evt := s.logger.InfoWith().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Int64("duration_ms", duration.Milliseconds()).
			Int64("bytes", rec.bytes).
			Str("remote", clientIP(r))
		// Surface the 4xx/5xx error classification when present, so
		// the access-log line answers "what went wrong" without an
		// extra log-stream join. 5xx detail (the wrapped error
		// chain) still lives on writeServerError's ERR line.
		if rec.errCode != "" {
			evt = evt.
				Str("code", rec.errCode).
				Str("error", rec.errMessage).
				Str("op", rec.errOp)
		}
		evt.Msg("http request")
	})
}
