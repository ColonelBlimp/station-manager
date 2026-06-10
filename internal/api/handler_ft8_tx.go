package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// ft8TxArmRequest is the POST /v1/ft8/tx/arm body: {"armed": bool}. Arming is
// the explicit operator gate before any FT8 RF — nothing transmits while
// disarmed. Idempotent.
type ft8TxArmRequest struct {
	Armed bool `json:"armed"`
}

// ft8TxSendRequest is the POST /v1/ft8/tx/send body: the standard FT8 message
// to transmit on the next UTC slot at the chosen audio base offset.
type ft8TxSendRequest struct {
	Message  string  `json:"message"`
	OffsetHz float64 `json:"offset_hz"`
}

// handleFt8TxArm arms/disarms the FT8 transmit path (ADR 0030 step e1).
// Registered only when the FT8 subsystem is enabled. Arming requires a
// connected, identity-verified rig and an available output device; the daemon
// owns the guaranteed stop, so a 202 means "applied" and the resulting state is
// confirmed by the ft8-tx SSE event.
func (s *Server) handleFt8TxArm(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8TxArm"

	var req ft8TxArmRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if err := s.ft8.ArmTx(req.Armed); err != nil {
		s.writeFt8TxError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleFt8TxSend queues one standard FT8 message to transmit on the next UTC
// slot (ADR 0030 step e1). Refused unless TX is armed. A 202 means "queued";
// completion or failure rides the ft8-tx SSE event, since the transmission
// spans the slot boundary + ~12.6 s of audio.
func (s *Server) handleFt8TxSend(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8TxSend"

	var req ft8TxSendRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if err := s.ft8.TransmitNext(req.Message, req.OffsetHz); err != nil {
		s.writeFt8TxError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// writeFt8TxError maps the FT8 TX sentinels to HTTP status + i18n codes. Not-ready
// is a transient "rig isn't up yet" the SPA can retry; not-armed / in-flight are
// conflicts with the current state; bad-message is a client error; anything else
// is an internal failure.
func (s *Server) writeFt8TxError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, ft8.ErrTxUnavailable):
		s.writeError(w, http.StatusServiceUnavailable, "ft8_tx_unavailable",
			"FT8 transmit is unavailable (no rig keyer wired, or this build has no audio output)", op)
	case stderr.Is(err, ft8.ErrTxNotReady):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_ready",
			"rig not connected or identity unverified; try again in a moment", op)
	case stderr.Is(err, ft8.ErrTxNotArmed):
		s.writeError(w, http.StatusConflict, "ft8_tx_not_armed",
			"FT8 transmit is not armed", op)
	case stderr.Is(err, ft8.ErrTxInFlight):
		s.writeError(w, http.StatusConflict, "ft8_tx_in_flight",
			"a transmission is already in flight", op)
	case stderr.Is(err, ft8.ErrTxBadMessage):
		s.writeError(w, http.StatusBadRequest, "ft8_tx_bad_message",
			"not an encodable standard FT8 message", op)
	default:
		s.writeServerError(w, op, err, "ft8_tx_failed", "failed to drive FT8 transmit")
	}
}
