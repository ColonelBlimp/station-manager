package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// readBody / readJSONBody delegate to the Server's httpkit.Kit (ADR 0043).
// Kept as Server methods so handler call sites are untouched by the extraction.

func (s *Server) readBody(w http.ResponseWriter, r *http.Request, op errors.Op) ([]byte, bool) {
	return s.kit.ReadBody(w, r, op)
}

func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, op errors.Op, dst any) bool {
	return s.kit.ReadJSONBody(w, r, op, dst)
}
