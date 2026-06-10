package api

import (
	stderr "errors"
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// ft8QsoStartRequest is the POST /v1/ft8/qso/start body (ADR 0031): answer the
// CQ from `their_call` (with `their_grid`), heard in the slot at `slot_utc`
// (which fixes the worked station's parity), transmitting on `offset_hz`. Our
// own callsign/grid are resolved server-side from the station config, not sent
// by the client.
type ft8QsoStartRequest struct {
	TheirCall string  `json:"their_call"`
	TheirGrid string  `json:"their_grid"`
	SlotUTC   string  `json:"slot_utc"`
	OffsetHz  float64 `json:"offset_hz"`
}

// handleFt8QsoStart begins a manual answer-a-CQ exchange (ADR 0031, step e3).
// Registered only when FT8 is enabled. Requires TX already armed. A 202 means
// "the sequencer is now driving this contact"; progress rides the ft8-qso SSE.
func (s *Server) handleFt8QsoStart(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8QsoStart"

	var req ft8QsoStartRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if strings.TrimSpace(req.TheirCall) == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "their_call is required", op)
		return
	}

	// Our identity is daemon-owned (the station config), not client-supplied.
	ls := s.cfg.Snapshot().LoggingStation
	ourCall := strings.TrimSpace(ls.StationCallsign)
	if ourCall == "" {
		ourCall = strings.TrimSpace(ls.Operator)
	}
	if ourCall == "" {
		s.writeError(w, http.StatusBadRequest, "no_station_callsign",
			"set your station callsign in My Station before transmitting", op)
		return
	}

	err := s.ft8.StartQso(ourCall, ls.MyGridsquare, req.TheirCall, req.TheirGrid, req.SlotUTC, req.OffsetHz)
	if err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleFt8QsoAbandon drops any active sequenced QSO. Idempotent — abandoning
// when idle is a 202 no-op.
func (s *Server) handleFt8QsoAbandon(w http.ResponseWriter, _ *http.Request) {
	s.ft8.AbandonQso()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) writeFt8QsoError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, ft8.ErrTxNotArmed):
		s.writeError(w, http.StatusConflict, "ft8_tx_not_armed",
			"arm FT8 transmit before starting a QSO", op)
	case stderr.Is(err, ft8.ErrQsoInProgress):
		s.writeError(w, http.StatusConflict, "ft8_qso_in_progress",
			"a QSO is already in progress; abandon it first", op)
	case stderr.Is(err, ft8.ErrNoOffset):
		s.writeError(w, http.StatusBadRequest, "ft8_no_offset",
			"pick a clear TX offset before starting a QSO", op)
	default:
		// A bad slot_utc (time.Parse failure) and anything else map to 400 —
		// the request is malformed, not a server fault.
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"could not start the QSO: "+err.Error(), op)
	}
}
