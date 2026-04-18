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
	s.writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		Op:      op,
	})
}
