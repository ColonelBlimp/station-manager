package api

import (
	"net/http"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/stationevents"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// handleListStationEvents returns the newest operator events across categories,
// newest first — the Station Events page's read (W-0020, ADR 0076). It replaced
// GET /v1/notifications (ruling 4): the same OperatorEvent DTO, now spanning the
// notification and alarm categories, narrowed by ?category= and ?severity=.
//
// An unknown category or severity is a 400, never an empty list: on the page an
// empty filtered list must read as a filter, not as a fault or a quiet history.
// ?limit=N (default 50) is bounded by the store's whole ring
// (sqlite.OperatorEventFetchLimitMax); out of range is rejected, not clamped.
func (s *Server) handleListStationEvents(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleListStationEvents"
	q := r.URL.Query()

	var f sqlite.OperatorEventFilter
	if c := q.Get("category"); c != "" {
		if _, known := stationevents.KindsByCategory()[c]; !known {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value", "unknown category", op)
			return
		}
		f.Category = c
	}
	switch sev := q.Get("severity"); sev {
	case "", stationevents.SeverityInfo, stationevents.SeverityWarn, stationevents.SeverityError:
		f.Severity = sev
	default:
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "unknown severity", op)
		return
	}
	limit := 50
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > sqlite.OperatorEventFetchLimitMax {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value",
				"limit must be an integer in [1, "+strconv.Itoa(sqlite.OperatorEventFetchLimitMax)+"]", op)
			return
		}
		limit = n
	}

	events, err := s.db.FetchOperatorEventsWithContext(r.Context(), f, limit)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}
	if events == nil {
		events = []types.OperatorEvent{}
	}
	s.writeJSON(w, http.StatusOK, struct {
		Items []types.OperatorEvent `json:"items"`
	}{Items: events})
}
