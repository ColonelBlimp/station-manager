package api

import (
	"net/http"
	"runtime/debug"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// recoverPanic wraps an http.Handler with a panic safety net.
//
// Go's net/http has implicit per-request recovery, but it logs via
// the stdlib logger, closes the connection without a response body,
// and is invisible to our structured logging pipeline. This
// middleware catches the panic, logs it through logging.Service
// with a stack trace plus request method and path, and writes a
// proper 500 error envelope so the client gets a response in the
// same shape as every other 4xx/5xx.
//
// Double-response caveat: if the wrapped handler already called
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
