package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// logbookDestination is one destination binding of a logbook as the logbook
// view needs it (ADR 0082): the binding's durable name — what the backfill and
// `missing_from` address — its type, the station account's display label and
// whether the route is on. No credentials, ever.
type logbookDestination struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Label   string `json:"label,omitempty"`
	Enabled bool   `json:"enabled"`
}

// handleLogbookDestinations serves GET /v1/logbook/{id}/destinations — the
// bindings of one logbook from the daemon's start-time snapshot, in snapshot
// order. The logbook view's backfill picker and upload-status colour read
// this rather than the station's config entries: an additional logbook's
// binding is named `<type>.<logbook uuid>` and only that name routes its QSOs
// (Codex P2 on 7990011b). An unknown id lists nothing.
func (s *Server) handleLogbookDestinations(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleLogbookDestinations"
	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}
	labels := map[string]string{}
	for _, fc := range s.cfg.Forwarders() {
		labels[fc.Type] = fc.Label
	}
	out := make([]logbookDestination, 0)
	for _, route := range s.qso.DestinationRoutes() {
		if route.LogbookID != id {
			continue
		}
		out = append(out, logbookDestination{
			Name: route.Config.Name, Type: route.Config.Type, Label: labels[route.Config.Type], Enabled: route.Config.Enabled,
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"destinations": out})
}
