package api

import (
	"encoding/json"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// ErrorResponse is the JSON error envelope per api.md Section 4.6.
type ErrorResponse struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Op      errors.Op `json:"op,omitempty"`
}

// writeJSON marshals v as JSON and writes it as the response with the given
// status. Encode errors are logged at warn — the status is already on the
// wire by the time a write can fail, so the error cannot be signalled to
// the client, but losing the diagnostic signal entirely (silent drop)
// makes mid-response failures invisible server-side. A warn line restores
// the breadcrumb without pretending we can recover.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.WarnWith().Err(err).Int("status", status).Msg("failed to encode JSON response")
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string, op errors.Op) {
	// Stash the envelope classification on the responseRecorder (when
	// present — the writer is wrapped by logRequests in production)
	// so the access-log line can name what the failure was. The
	// type assertion is a no-op fallback in tests that exercise
	// writeError without the middleware chain.
	if rec, ok := w.(*responseRecorder); ok {
		rec.noteError(code, message, string(op))
	}
	s.writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		Op:      op,
	})
}

// writeServerError handles 5xx outcomes consistently: logs the underlying
// error with full op-tagged context for the operator, then emits a
// deliberately generic envelope to the client. Raw err.Error() is never
// returned over the wire because internal error chains can carry SQL
// constraint snippets, file paths, or driver text that the client has no
// business seeing — the same defence-in-depth that recoverPanic applies
// to panic values.
//
// `code` and `clientMsg` are the wire-visible classification; pass
// stable, low-cardinality values ("db_error", "internal_error") so
// clients can switch on them. The full err goes to the log line.
func (s *Server) writeServerError(w http.ResponseWriter, op errors.Op, err error, code, clientMsg string) {
	s.logger.ErrorWith().Err(err).Str("op", string(op)).Str("code", code).Msg("server error")
	s.writeError(w, http.StatusInternalServerError, code, clientMsg, op)
}
