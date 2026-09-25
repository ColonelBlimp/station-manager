package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// forwarderQueueCount is one forwarder's entry in the GET /v1/forwarder-queues
// response: Waiting is the pending backlog still to be sent and Failed the
// terminal rows a retry would re-arm — carried apart (W-0010 outcome 9) so a
// failure never reads as a live backlog; Clearable is their sum, the count a
// clear would remove (W-0005); InFlight is the in_progress batch a live worker
// is processing and never clears.
type forwarderQueueCount struct {
	Name      string `json:"name"`
	Waiting   int64  `json:"waiting"`
	Failed    int64  `json:"failed"`
	Clearable int64  `json:"clearable"`
	InFlight  int64  `json:"in_flight"`
}

// forwarderQueuesResponse lists the counts per configured forwarder name. The
// interim forwarding gate of W-0021 slice 2B is gone (ADR 0082): an archive
// forwards exactly what its enabled bindings say, so there is no gate to state.
type forwarderQueuesResponse struct {
	Forwarders []forwarderQueueCount `json:"forwarders"`
}

// clearForwarderQueueResponse is the POST /v1/forwarder/{name}/queue/clear result.
type clearForwarderQueueResponse struct {
	Discarded int64 `json:"discarded"`
}

// retryForwarderQueueResponse is the POST /v1/forwarder/{name}/queue/retry result.
type retryForwarderQueueResponse struct {
	Rearmed int64 `json:"rearmed"`
}

// handleForwarderQueues serves GET /v1/forwarder-queues — the Settings →
// Forwarding queue readout. Every BINDING of the active archive appears (in
// listing order, enabled or not — ADR 0082), each with its clearable/in-flight
// counts; a name with no queued rows reads {0,0}. Merged in the handler so the
// SPA renders a count next to every destination it lists.
func (s *Server) handleForwarderQueues(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleForwarderQueues"

	counts, err := s.db.ForwarderQueueCountsWithContext(r.Context())
	if err != nil {
		s.writeServerError(w, op, err, "queue_counts_failed", "read forwarder queue counts failed")
		return
	}

	names := s.queueNames
	out := forwarderQueuesResponse{Forwarders: make([]forwarderQueueCount, 0, len(names))}
	for _, name := range names {
		c := counts[name] // zero value {0,0} when the binding has no rows
		out.Forwarders = append(out.Forwarders, forwarderQueueCount{
			Name:      name,
			Waiting:   c.Waiting,
			Failed:    c.Failed,
			Clearable: c.Clearable(),
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
//   - 404 unknown_forwarder  name is not a binding of the active archive
//   - 200 {discarded}
func (s *Server) handleClearForwarderQueue(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleClearForwarderQueue"

	// Use the EXACT path value, not a trimmed copy: forwarder names are stored and
	// listed (GET /v1/forwarder-queues) verbatim, and config validation only
	// rejects an empty name — it neither trims nor forbids surrounding whitespace.
	// Trimming here would make a legitimately-configured name like " qrz " show up
	// in the readout yet be unclearable (lookup misses → 404). Round-trip the name
	// the GET returns.
	name := r.PathValue("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_forwarder", "forwarder name is required", op)
		return
	}
	if !s.isQueueName(name) {
		s.writeError(w, http.StatusNotFound, "unknown_forwarder", "no such destination binding in the active archive", op)
		return
	}

	n, err := s.db.DiscardClearableUploadsForForwarderWithContext(r.Context(), name)
	if err != nil {
		s.writeServerError(w, op, err, "clear_failed", "clear forwarder queue failed")
		return
	}
	s.writeJSON(w, http.StatusOK, clearForwarderQueueResponse{Discarded: n})
}

// handleRetryForwarderQueue serves POST /v1/forwarder/{name}/queue/retry — the
// operator's "Retry failed" (W-0010 outcome 9, ruling (a)). It returns EVERY
// failed row of the named forwarder to pending, whatever its failure class; the
// forwarder's worker then drains them like any other backlog. A row that is
// still invalid fails once more, terminally (one more forward.failed event); an
// accepted upload cannot be duplicated because uploaded rows are never touched.
//
// Unlike clear, retry requires a worker created at daemon startup: a forwarder
// disabled at startup has none, so re-arming would show a "waiting" count that
// never moves. The startup snapshot matters because config saves change the
// live config service immediately while workers change only on restart.
//
// Status codes:
//   - 400 invalid_forwarder   empty name
//   - 404 unknown_forwarder   name is not a binding of the active archive
//   - 400 forwarder_disabled  no running worker of that name
//   - 200 {rearmed}
func (s *Server) handleRetryForwarderQueue(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRetryForwarderQueue"

	// Exact path value, for the same round-trip reason as clear.
	name := r.PathValue("name")
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_forwarder", "forwarder name is required", op)
		return
	}
	if !s.isQueueName(name) {
		s.writeError(w, http.StatusNotFound, "unknown_forwarder", "no such destination binding in the active archive", op)
		return
	}
	if _, running := s.startupForwarders[name]; !running {
		s.writeError(w, http.StatusBadRequest, "forwarder_disabled",
			"forwarder has no running worker; restart the daemon with it enabled before retrying", op)
		return
	}

	n, err := s.db.RearmFailedUploadsForForwarderWithContext(r.Context(), name)
	if err != nil {
		s.writeServerError(w, op, err, "retry_failed", "retry forwarder queue failed")
		return
	}
	s.writeJSON(w, http.StatusOK, retryForwarderQueueResponse{Rearmed: n})
}

// isQueueName reports whether name (exact match) is one the queue endpoints
// know — a binding of the active archive.
func (s *Server) isQueueName(name string) bool {
	for _, n := range s.queueNames {
		if n == name {
			return true
		}
	}
	return false
}
