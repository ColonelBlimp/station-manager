package api

import (
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// forwarderQueueCount is one forwarder's entry in the GET /v1/forwarder-queues
// response: Clearable is the operator-clearable backlog (pending + failed) a
// clear would remove; InFlight is the in_progress batch a live worker is
// processing and never clears. See W-0005.
type forwarderQueueCount struct {
	Name      string `json:"name"`
	Clearable int64  `json:"clearable"`
	InFlight  int64  `json:"in_flight"`
}

type forwarderQueuesResponse struct {
	Forwarders []forwarderQueueCount `json:"forwarders"`
}

// clearForwarderQueueResponse is the POST /v1/forwarder/{name}/queue/clear result.
type clearForwarderQueueResponse struct {
	Discarded int64 `json:"discarded"`
}

// handleForwarderQueues serves GET /v1/forwarder-queues — the Settings →
// Forwarding queue readout. Every CONFIGURED forwarder appears (in config order,
// enabled or not), each with its clearable/in-flight counts; a forwarder with no
// queued rows reads {0,0}. Merged in the handler so the SPA renders a count next
// to every forwarder it already lists.
func (s *Server) handleForwarderQueues(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleForwarderQueues"

	counts, err := s.db.ForwarderQueueCountsWithContext(r.Context())
	if err != nil {
		s.writeServerError(w, op, err, "queue_counts_failed", "read forwarder queue counts failed")
		return
	}

	fwds := s.cfg.Forwarders()
	out := forwarderQueuesResponse{Forwarders: make([]forwarderQueueCount, 0, len(fwds))}
	for _, f := range fwds {
		c := counts[f.Name] // zero value {0,0} when the forwarder has no rows
		out.Forwarders = append(out.Forwarders, forwarderQueueCount{
			Name:      f.Name,
			Clearable: c.Clearable,
			InFlight:  c.InFlight,
		})
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleClearForwarderQueue serves POST /v1/forwarder/{name}/queue/clear — the
// operator-triggered "drop the backlog, finish the currently claimed batch"
// clear (W-0005). It removes only the named forwarder's pending + failed rows,
// leaving in_progress (the claimed batch) and uploaded (history) untouched, and
// is independent of enable/disable (either may be cleared).
//
// Status codes:
//   - 400 invalid_forwarder  empty name
//   - 404 unknown_forwarder  name is not a configured forwarder
//   - 200 {discarded}
func (s *Server) handleClearForwarderQueue(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleClearForwarderQueue"

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_forwarder", "forwarder name is required", op)
		return
	}
	if !s.isConfiguredForwarder(name) {
		s.writeError(w, http.StatusNotFound, "unknown_forwarder", "no such forwarder", op)
		return
	}

	n, err := s.db.DiscardClearableUploadsForForwarderWithContext(r.Context(), name)
	if err != nil {
		s.writeServerError(w, op, err, "clear_failed", "clear forwarder queue failed")
		return
	}
	s.writeJSON(w, http.StatusOK, clearForwarderQueueResponse{Discarded: n})
}

// isConfiguredForwarder reports whether name matches a configured forwarder.
func (s *Server) isConfiguredForwarder(name string) bool {
	for _, f := range s.cfg.Forwarders() {
		if f.Name == name {
			return true
		}
	}
	return false
}
