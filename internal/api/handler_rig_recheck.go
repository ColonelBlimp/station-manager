package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rigRecheckResponse reports what the re-probe achieved. `alarm_active` is read
// AFTER the query went out, so on a healthy rig that answers within the request
// it is already false — but it is NOT a safety verdict and the SPA must not
// treat it as one: the answer arrives asynchronously on the read loop, so the
// usual outcome is `asked: true, alarm_active: true` followed a moment later by
// the authoritative tx-alarm SSE clear.
type rigRecheckResponse struct {
	Asked       bool `json:"asked"`
	AlarmActive bool `json:"alarm_active"`
}

// handleRigTxRecheck re-asks the rig whether it is transmitting (2026-07-21
// stuck-TX incident). The alarm latches itself out of every clear path — every
// issuer of the TX-status query is gated by the same txUncertain flag the alarm
// holds — so without this an operator can be left staring at a "CHECK YOUR
// RADIO" banner that no action of theirs can resolve.
//
// This endpoint ONLY puts a read on the wire. It cannot clear the alarm, and
// deliberately offers no way to: retiring the warning without positive evidence
// would either re-enable keying over a possibly-live PTT or hide the only
// standing warning from every tab (the hub caches it for late subscribers).
// Only the rig's own "I am in RX", via observeTxStatus → confirmTxIdle, clears
// it. Operator acknowledgement is a client-side concern — the SPA's banner
// already hides locally without touching daemon state.
func (s *Server) handleRigTxRecheck(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRigTxRecheck"

	if err := s.bridge.RecheckTx(); err != nil {
		s.writeRigRecheckError(w, op, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rigRecheckResponse{
		Asked:       true,
		AlarmActive: s.bridge.TxAlarmActive(),
	})
}

// writeRigRecheckError maps the re-probe's failures to HTTP. A rig that cannot
// be reached or identified is a transient/config condition the operator can act
// on; a rigdef with no TX-status query can never answer, so it is 501 rather
// than something to retry.
func (s *Server) writeRigRecheckError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, bridge.ErrRigNotConnected):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_connected",
			"no rig is currently connected", op)
	case stderr.Is(err, bridge.ErrRigIdentityUnverified):
		s.writeError(w, http.StatusConflict, "rig_identity_unverified",
			"connected rig's identity is unverified; check the configured driver matches the rig", op)
	case stderr.Is(err, bridge.ErrTxRecheckUnsupported):
		s.writeError(w, http.StatusNotImplemented, "rig_tx_recheck_unsupported",
			"this rig cannot be asked for its transmit state", op)
	default:
		s.writeServerError(w, op, err, "rig_tx_recheck_failed", "failed to query the rig's transmit state")
	}
}
