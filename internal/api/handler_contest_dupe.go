package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// handleContestDupe answers "have I worked this station on this band
// (and optionally this mode) in this logbook already?". It's a hot-path
// operator UI query during contesting — minimal response, no list.
//
// Required params: logbook, call, band. Optional: mode. Mode is present
// for band+mode contests (CQ WW, etc.) and omitted for band-only
// contests (ARRL DX, etc.) — the client owns the contest rule.
func (s *Server) handleContestDupe(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleContestDupe"

	q := r.URL.Query()

	logbookIDStr := q.Get("logbook")
	if logbookIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing_required_param",
			"logbook query parameter is required", op)
		return
	}
	logbookID, err := strconv.ParseInt(logbookIDStr, 10, 64)
	if err != nil || logbookID < 1 {
		writeError(w, http.StatusBadRequest, "invalid_id",
			"logbook must be a positive integer", op)
		return
	}

	callsign := strings.ToUpper(strings.TrimSpace(q.Get("call")))
	if callsign == "" {
		writeError(w, http.StatusBadRequest, "missing_required_param",
			"call query parameter is required", op)
		return
	}
	if !isValidCallsign(callsign) {
		writeError(w, http.StatusBadRequest, "invalid_field_value",
			"call must be 3-32 characters and contain at least one digit", op)
		return
	}

	band := strings.ToLower(strings.TrimSpace(q.Get("band")))
	if band == "" {
		writeError(w, http.StatusBadRequest, "missing_required_param",
			"band query parameter is required", op)
		return
	}
	if !bands.IsValidBand(band) {
		writeError(w, http.StatusBadRequest, "invalid_field_value",
			"band is not a recognised band", op)
		return
	}

	mode := strings.ToUpper(strings.TrimSpace(q.Get("mode")))
	if mode != "" && !modes.IsValidMode(mode) {
		writeError(w, http.StatusBadRequest, "invalid_field_value",
			"mode is not a recognised mode", op)
		return
	}

	// Verify the logbook exists, same rationale as the list endpoint: a
	// bogus id silently returning "not a dupe" would be misleading.
	// Contest operators hammer this endpoint during pileups, so the
	// lightweight EXISTS probe is important here.
	exists, err := s.db.LogbookExistsByIDWithContext(r.Context(), logbookID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "logbook_not_found",
			"logbook does not exist", op)
		return
	}

	dupe, err := s.db.IsContestDuplicateByLogbookIDWithContext(
		r.Context(), logbookID, callsign, band, mode,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Duplicate bool `json:"duplicate"`
	}{
		Duplicate: dupe,
	})
}
