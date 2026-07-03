package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/api/httpkit"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// ErrorResponse is the JSON error envelope. Aliased from httpkit so existing
// callers and tests keep referring to api.ErrorResponse while the definition
// lives in one place (ADR 0043).
type ErrorResponse = httpkit.ErrorResponse

// The write helpers delegate to the Server's httpkit.Kit. They stay as Server
// methods so the ~180 handler call sites (s.writeJSON / s.writeError / …) are
// untouched by the extraction; the per-surface split (ADR 0043) will later
// replace these with direct kit use inside each surface package.

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	s.kit.WriteJSON(w, status, v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string, op errors.Op) {
	s.kit.WriteError(w, status, code, message, op)
}

func (s *Server) writeServerError(w http.ResponseWriter, op errors.Op, err error, code, clientMsg string) {
	s.kit.WriteServerError(w, op, err, code, clientMsg)
}
