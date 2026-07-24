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

	// missing_from (ADR 0039): count only QSOs not yet uploaded to this
	// destination, so the SPA's "of N" matches the filtered page.
	var missingPrefix string
	if raw := r.URL.Query().Get("missing_from"); raw != "" {
		p, ok := s.resolveMissingFromPrefix(raw)
		if !ok {
			s.writeError(w, http.StatusBadRequest, "invalid_missing_from",
				"missing_from must name a configured forwarder with an upload-status stamp", op)
			return
		}
		missingPrefix = p
	}

	// not_emailed: count only QSOs not yet forwarded by email, so the SPA's
	// "of N" matches the "Not emailed only" filtered page.
	notEmailed, err := parseBoolQuery(r, "not_emailed")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_not_emailed",
			"not_emailed must be a boolean", op)
		return
	}

	count, err := s.db.FetchQsoCountByLogbookIdWithContext(r.Context(), logbookID, missingPrefix, notEmailed)
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
