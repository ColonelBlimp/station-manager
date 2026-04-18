package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleHealthz"

	if err := s.db.Ping(); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable", op)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
