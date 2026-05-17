package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

func (s *Server) handleLogbookCount(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleLogbookCount"

	logbookID, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	exists, err := s.db.LogbookExistsByIDWithContext(r.Context(), logbookID)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}
	if !exists {
		s.writeError(w, http.StatusNotFound, "logbook_not_found", "logbook does not exist", op)
		return
	}

	count, err := s.db.FetchQsoCountByLogbookIdWithContext(r.Context(), logbookID)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, struct {
		LogbookID int64 `json:"logbook_id"`
		Count     int64 `json:"count"`
	}{
		LogbookID: logbookID,
		Count:     count,
	})
}
