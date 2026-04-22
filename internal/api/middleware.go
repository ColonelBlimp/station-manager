package api

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// sseEventsPath is the one SSE endpoint; limitConcurrent exempts it
// from the general concurrent-request cap because SSE connections are
// long-lived by design and would otherwise saturate that cap under
// normal use. /v1/events has its own subscriber cap in
// limitEventSubscribers.
const sseEventsPath = "/v1/events"

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
// envelope write below is effectively swallowed — the client sees
// whatever partial bytes made it out first. This mirrors Go stdlib
// behaviour; recovery's primary job here is to keep the daemon
// alive and log the incident, not to guarantee a clean response
// shape in every case.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	const op errors.Op = "api.recoverPanic"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			s.logger.ErrorWith().
				Interface("panic", rec).
				Str("stack", string(debug.Stack())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Msg("panic in HTTP handler")
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
		if r.URL.Path == sseEventsPath {
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
