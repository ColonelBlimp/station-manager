package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rigTuneRequest is the POST /v1/rig/tune body (ADR 0027): {"active": bool}.
// active=true keys the tune carrier; active=false drops it and restores the
// pre-tune mode + power. Both are idempotent — a redundant start while tuning,
// or a stop while idle, is a no-op.
type rigTuneRequest struct {
	Active bool `json:"active"`
}

// handleRigTune drives the daemon-owned tune controller (ADR 0027) — the first
// SM endpoint that transmits. Registered only when the bridge is enabled. The
// daemon owns the guaranteed stop (hard auto-off timer + release-on-disconnect),
// so a 202 means "the tune carrier was keyed / dropped"; the resulting state is
// confirmed out-of-band by the tune-state SSE event, not by this response.
func (s *Server) handleRigTune(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRigTune"

	var req rigTuneRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	var err error
	if req.Active {
		err = s.bridge.StartTune(r.Context())
	} else {
		err = s.bridge.StopTune(r.Context())
	}
	if err != nil {
		s.writeRigTuneError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeRigTuneError maps the tune controller's errors to HTTP. Not-connected
// and state-unknown are both transient "the rig isn't ready yet" conditions
// (rig off / reconnecting / no state observed yet) the SPA can retry; anything
// else is an internal failure.
func (s *Server) writeRigTuneError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, bridge.ErrRigNotConnected):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_connected",
			"no rig is currently connected", op)
	case stderr.Is(err, bridge.ErrTuneStateUnknown):
		s.writeError(w, http.StatusServiceUnavailable, "rig_state_unknown",
			"rig mode/power not yet known; try again in a moment", op)
	default:
		s.writeServerError(w, op, err, "rig_tune_failed", "failed to drive rig tune")
	}
}
