package api

import (
	stderr "errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// handleContactHistory answers "have I worked this callsign before, and
// where?". Drives the recent-contacts panel in the logging app's draft
// flow. Results are newest-first (by created_at) and capped by
// Server.MaxContactHistoryResults; this is a "recent contacts" view, not
// a paginated log browser.
//
// Scope: by default, all logbooks are searched — operators usually want
// "have I ever worked them". An optional ?logbook=<id> restricts the
// search to a single logbook (useful inside a contest session).
func (s *Server) handleContactHistory(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleContactHistory"

	q := r.URL.Query()

	callsign := strings.ToUpper(strings.TrimSpace(q.Get("call")))
	if callsign == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param",
			"call query parameter is required", op)
		return
	}
	if !isValidCallsign(callsign) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"call must be 3-32 characters and contain at least one digit", op)
		return
	}

	// Optional logbook filter.
	var logbookID int64
	if raw := q.Get("logbook"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			s.writeError(w, http.StatusBadRequest, "invalid_id",
				"logbook must be a positive integer", op)
			return
		}
		// Verify it exists so a typo in the logbook id doesn't silently
		// produce an empty result that looks identical to "never worked".
		exists, err := s.db.LogbookExistsByIDWithContext(r.Context(), id)
		if err != nil {
			s.writeServerError(w, op, err, "db_error", "database operation failed")
			return
		}
		if !exists {
			s.writeError(w, http.StatusNotFound, "logbook_not_found",
				"logbook does not exist", op)
			return
		}
		logbookID = id
	}

	history, err := s.db.FetchQsoSliceByCallsignWithContext(
		r.Context(), callsign, logbookID, s.maxContactHistoryResults,
	)
	if err != nil {
		// "No prior contacts" is not a 404 — it's an empty list.
		if stderr.Is(err, errors.ErrNotFound) {
			history = nil
		} else {
			s.writeServerError(w, op, err, "db_error", "database operation failed")
			return
		}
	}

	if history == nil {
		history = []types.ContactHistory{}
	}

	s.writeJSON(w, http.StatusOK, struct {
		Items []types.ContactHistory `json:"items"`
	}{
		Items: history,
	})
}
