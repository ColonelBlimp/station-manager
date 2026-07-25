package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rigRecheckResponse reports what the evidence attempt achieved. `alarm_active`
// is read afterwards: a CI-V tx_off ACK is synchronous and may already have
// cleared it, while a TX-status query normally answers asynchronously on the
// read loop. The authoritative state remains the tx-alarm SSE event.
type rigRecheckResponse struct {
	Asked       bool `json:"asked"`
	AlarmActive bool `json:"alarm_active"`
}

// handleRigTxRecheck asks for fresh protocol evidence (2026-07-21 stuck-TX
// incident). Rigs with read_tx_status are queried; CI-V safely re-asserts
// tx_off and awaits the addressed rig's ACK.
//
// This endpoint offers no operator-asserted clear. Only the rig's own
// TXSTATUS=RX answer or an accepted CI-V tx_off ACK retires the alarm. That
// preserves the guarantee that the warning and TX gate cannot disappear merely
// because the operator pressed a button.
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
// on; a rigdef with neither a status query nor ACK-confirmed safe unkey is 501.
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
			"this rig has no supported transmit-state recovery check", op)
	default:
		s.writeServerError(w, op, err, "rig_tx_recheck_failed", "failed to obtain transmit-state evidence")
	}
}
