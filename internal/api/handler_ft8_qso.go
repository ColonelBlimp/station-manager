package api

import (
	stderr "errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// validFt8SlotUTC reports whether v is the RFC3339 UTC timestamp the FT8
// sequencer expects (it parses with time.RFC3339 — sequencer.go /
// work_sequencer.go). Validating at the API boundary keeps Go's raw parse-error
// text out of the wire envelope and out of the catch-all error branch, which
// must stay reserved for genuine server faults (review 2026-06-19 M2).
func validFt8SlotUTC(v string) bool {
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}

// validFt8OperatingFreq reports whether mhz is a positive, known-band dial
// frequency. A sequenced QSO is made ON AIR and only logged at completion, so
// committing one with an unloggable frequency means the exchange finishes and
// THEN auto-logging fails after the fact — unrecoverable (review 2026-06-19 M2).
// The SPA gates on freqKnown; the daemon enforces the same invariant for direct
// or stale clients. (At completion the daemon prefers the bridge's live dial;
// this validates the SPA-supplied fallback that's used when the bridge hasn't
// decoded a frequency yet.)
func validFt8OperatingFreq(mhz float64) bool {
	if mhz <= 0 || math.IsNaN(mhz) || math.IsInf(mhz, 0) {
		return false
	}
	return utils.FrequencyToBand(fmt.Sprintf("%.6f", mhz)) != ""
}

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
	// OperatingFreqMHz is the rig's dial frequency (the SPA reads it from the live
	// rig state). The logged QSO frequency IS this dial frequency — the FT8
	// convention (BuildQso logs the dial, not dial+offset; offset_hz is TX
	// placement only, never folded into FREQ). The daemon can't read the dial
	// itself (the bridge is a pure pass-through), so the SPA supplies it.
	OperatingFreqMHz float64 `json:"operating_freq_mhz"`
	// Mode selects the exchange: "" / "standard" is the normal grid+report answer;
	// "fd" answers a CQ FD with the operator's ARRL Field Day exchange (class+section
	// from ft8.field_day config); "type4" answers a NONSTANDARD/compound-call CQ with the
	// reduced bare-calls→RR73→73 ladder (ADR 0048 — no grid/report on the wire). The SPA
	// sets the mode from the shape of the clicked decode.
	Mode string `json:"mode,omitempty"`
	// TheirSnr — our SNR of the clicked decode. Used for modes "fd" and "type4": neither
	// exchanges a report on the air, so we log this measured SNR as RST_SENT (standard
	// answer-a-CQ derives its report from the exchange, so this is ignored there).
	TheirSnr int `json:"their_snr,omitempty"`
}

// handleFt8QsoStart begins a manual answer-a-CQ exchange (ADR 0031, step e3).
// validFt8ExchangeMode reports whether m is an accepted FT8 exchange type. An
// unrecognised value must NOT fall through to the standard on-air exchange
// (review 2026-07-22 #2): this is a TX path, so a typo'd mode would key the rig
// with the wrong exchange. "" and "standard" both select the standard path.
func validFt8ExchangeMode(m string) bool {
	switch m {
	case "", "standard", "fd", "type4":
		return true
	default:
		return false
	}
}

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
	if !validFt8SlotUTC(req.SlotUTC) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"slot_utc must be an RFC3339 UTC timestamp", op)
		return
	}

	// Our identity is daemon-owned (the station config), not client-supplied.
	if !validFt8OperatingFreq(req.OperatingFreqMHz) {
		s.writeError(w, http.StatusBadRequest, "ft8_no_frequency",
			"operating_freq_mhz must be a positive known-band frequency (the rig dial)", op)
		return
	}

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

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if !validFt8ExchangeMode(mode) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			`mode must be one of "", "standard", "fd", or "type4"`, op)
		return
	}
	var err error
	switch mode {
	case "fd":
		// ARRL Field Day: our class+section come from ft8.field_day config (read by
		// the Service), not the client. theirGrid is still logged (bearing/enrichment).
		err = s.ft8.StartQsoFd(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz)
	case "type4":
		// Reduced type-4 (nonstandard/compound call, ADR 0048): no grid/report on the
		// wire, so we log the measured SNR as RST_SENT (like FD).
		err = s.ft8.StartQsoT4(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz)
	default:
		err = s.ft8.StartQso(ourCall, ls.MyGridsquare, req.TheirCall, req.TheirGrid, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz)
	}
	if err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ft8CqStartRequest is the POST /v1/ft8/cq/start body (ADR 0033): call CQ and work
// the stations that answer, transmitting on `offset_hz`. Our callsign/grid are
// resolved server-side from the station config (like qso/start); the answerer-
// selection mode is daemon config (ft8.tx.caller_answer_mode). One session at a time.
type ft8CqStartRequest struct {
	OffsetHz float64 `json:"offset_hz"`
	// OperatingFreqMHz is the rig's dial frequency (the SPA reads it from the live
	// rig state); the logged QSO frequency IS this dial frequency (FT8 convention —
	// BuildQso logs the dial, not dial+offset).
	OperatingFreqMHz float64 `json:"operating_freq_mhz"`
	// TxParity is the operator's chosen CQ slot parity (WSJT-X "Tx even/1st"):
	// "even" (:00/:30) or "odd" (:15/:45). Empty (or any other value) = fire on the
	// next slot regardless of parity (the default, fastest first CQ). Operating
	// state, sent per Call-CQ session — not a persisted daemon setting.
	TxParity string `json:"tx_parity,omitempty"`
}

// handleFt8CqStart begins a sequenced Call-CQ session (ADR 0033, step e + caller
// side). Registered only when FT8 is enabled; requires TX already armed. A 202 means
// "the sequencer is now calling CQ and will work answerers"; progress (calling-cq →
// per-contact rungs) rides the ft8-qso SSE.
func (s *Server) handleFt8CqStart(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8CqStart"

	var req ft8CqStartRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	// Our identity is daemon-owned (the station config), not client-supplied.
	if !validFt8OperatingFreq(req.OperatingFreqMHz) {
		s.writeError(w, http.StatusBadRequest, "ft8_no_frequency",
			"operating_freq_mhz must be a positive known-band frequency (the rig dial)", op)
		return
	}

	ls := s.cfg.Snapshot().LoggingStation
	ourCall := strings.TrimSpace(ls.StationCallsign)
	if ourCall == "" {
		ourCall = strings.TrimSpace(ls.Operator)
	}
	if ourCall == "" {
		s.writeError(w, http.StatusBadRequest, "no_station_callsign",
			"set your station callsign in My Station before calling CQ", op)
		return
	}

	if err := s.ft8.StartCallCq(ourCall, ls.MyGridsquare, req.OffsetHz, req.OperatingFreqMHz, req.TxParity); err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ft8QsoWorkRequest is the POST /v1/ft8/qso/work body (ADR 0033 "work a caller"):
// work the station `their_call` (grid `their_grid`) that is calling US, heard in the
// slot at `slot_utc` (which fixes its parity), transmitting on `offset_hz`. `their_snr`
// is the SPA's SNR of the picked decode — the report we send back (RST_SENT). Our own
// callsign/grid are resolved server-side from the station config, like qso/start.
type ft8QsoWorkRequest struct {
	TheirCall string  `json:"their_call"`
	TheirGrid string  `json:"their_grid"`
	TheirSnr  int     `json:"their_snr"`
	SlotUTC   string  `json:"slot_utc"`
	OffsetHz  float64 `json:"offset_hz"`
	// OperatingFreqMHz is the rig's dial frequency (the SPA reads it from the live rig
	// state); the logged QSO frequency IS this dial (FT8 convention — see qso/start).
	OperatingFreqMHz float64 `json:"operating_freq_mhz"`
	// Mode "fd" works a caller who called us with a Field Day exchange (the SPA parsed
	// their class+section from "<ourCall> <theirCall> <class> <section>" and sends them
	// here); "type4" works a NONSTANDARD/compound caller with the reduced RR73 ladder
	// (ADR 0048 — no report, so their_snr is logged as RST_SENT); "" / "standard" is the
	// normal grid/report work. Our own class+section come from ft8.field_day config.
	Mode         string `json:"mode,omitempty"`
	TheirClass   string `json:"their_class,omitempty"`
	TheirSection string `json:"their_section,omitempty"`
}

// handleFt8QsoWork begins working a station that is calling us (ADR 0033). The operator
// picked it from the Band-Activity pile-up. Registered only when FT8 is enabled;
// requires TX already armed and no session in flight. A 202 means "the sequencer is now
// driving this contact"; progress (reporting → rogering) rides the ft8-qso SSE.
func (s *Server) handleFt8QsoWork(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8QsoWork"

	var req ft8QsoWorkRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if strings.TrimSpace(req.TheirCall) == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "their_call is required", op)
		return
	}
	if !validFt8SlotUTC(req.SlotUTC) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"slot_utc must be an RFC3339 UTC timestamp", op)
		return
	}

	// Our identity is daemon-owned (the station config), not client-supplied.
	if !validFt8OperatingFreq(req.OperatingFreqMHz) {
		s.writeError(w, http.StatusBadRequest, "ft8_no_frequency",
			"operating_freq_mhz must be a positive known-band frequency (the rig dial)", op)
		return
	}

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

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if !validFt8ExchangeMode(mode) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			`mode must be one of "", "standard", "fd", or "type4"`, op)
		return
	}
	var err error
	switch mode {
	case "fd":
		// Field Day: their class+section came from the picked call; ours from config.
		err = s.ft8.StartWorkCallerFd(ourCall, req.TheirCall, req.TheirGrid,
			req.TheirClass, req.TheirSection, req.TheirSnr, req.SlotUTC, req.OffsetHz, req.OperatingFreqMHz)
	case "type4":
		// Reduced type-4 (nonstandard/compound caller, ADR 0048): no report on the wire.
		err = s.ft8.StartWorkCallerT4(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz)
	default:
		err = s.ft8.StartWorkCaller(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz)
	}
	if err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ft8QsoPathRequest is the POST /v1/ft8/qso/path body: the operator's antenna-path
// choice for the active exchange — "S"/"short" or "L"/"long". Logging-only — it
// annotates the QSO the exchange logs (ADIF ANT_PATH + the matching bearing/distance)
// and never touches the on-air signal. Mirrors the Phone/CW short/long radio, but
// FT8 QSOs are built daemon-side, so the choice is sent here rather than at submit.
type ft8QsoPathRequest struct {
	Path string `json:"path"`
}

// handleFt8QsoPath records the antenna-path choice for the active FT8 exchange.
// Registered only when FT8 is enabled. Lenient: any value other than long ("L"/
// "long") is treated as short, so a bad value can't produce an invalid ADIF code.
// 202 — the choice is applied when the exchange logs (it resets to short per QSO).
func (s *Server) handleFt8QsoPath(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8QsoPath"

	var req ft8QsoPathRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	s.ft8.SetExchangePath(req.Path)
	w.WriteHeader(http.StatusAccepted)
}

// handleFt8QsoSkip arms/disarms skip-if-silent on the active session (the
// deferred Next moved daemon-side, 2026-07-13): armed, a silent cycle on an
// already-sent rung ends the session INSTEAD of keying the repeat — so a skip
// never puts RF at a station the operator has decided to drop. Body
// {"armed": bool}. 409 ft8_no_active_qso when arming with nothing skippable
// (idle, or a Call-CQ run — its Next is an immediate takeover); disarm is
// idempotent. The armed state comes back via the ft8-qso SSE (skip_armed).
func (s *Server) handleFt8QsoSkip(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8QsoSkip"
	var req struct {
		Armed bool `json:"armed"`
	}
	if !s.readJSONBody(w, r, op, &req) {
		return // readJSONBody already wrote the error envelope
	}
	if err := s.ft8.SetQsoSkip(req.Armed); err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleFt8QsoAbandon drops any active sequenced session — answer-a-CQ or Call-CQ.
// Idempotent — abandoning when idle is a 202 no-op.
func (s *Server) handleFt8QsoAbandon(w http.ResponseWriter, _ *http.Request) {
	s.ft8.AbandonQso()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) writeFt8QsoError(w http.ResponseWriter, op errors.Op, err error) {
	switch {
	case stderr.Is(err, ft8.ErrTxNotArmed):
		s.writeError(w, http.StatusConflict, "ft8_tx_not_armed",
			"arm FT8 transmit before starting a QSO", op)
	case stderr.Is(err, ft8.ErrTxNotReady):
		s.writeError(w, http.StatusServiceUnavailable, "rig_not_ready",
			"rig not connected or identity unverified; try again in a moment", op)
	case stderr.Is(err, ft8.ErrNoActiveQso):
		s.writeError(w, http.StatusConflict, "ft8_no_active_qso",
			"no active QSO to arm a skip on", op)
	case stderr.Is(err, ft8.ErrQsoInProgress):
		s.writeError(w, http.StatusConflict, "ft8_qso_in_progress",
			"a QSO is already in progress; abandon it first", op)
	case stderr.Is(err, ft8.ErrNoOffset):
		s.writeError(w, http.StatusBadRequest, "ft8_no_offset",
			"pick a clear TX offset before starting a QSO", op)
	case stderr.Is(err, ft8.ErrTxBadOffset):
		s.writeError(w, http.StatusBadRequest, "ft8_bad_offset",
			"TX offset is outside the usable passband", op)
	case stderr.Is(err, ft8.ErrTxBadMessage):
		s.writeError(w, http.StatusBadRequest, "ft8_tx_bad_message",
			"that station can't be worked on FT8 — only standard messages transmit "+
				"(compound/portable calls and free text can't be encoded)", op)
	case stderr.Is(err, ft8.ErrCallerAnswerModeUnsupported):
		s.writeError(w, http.StatusNotImplemented, "ft8_caller_mode_unsupported",
			"operator_pick answerer selection is not yet implemented; "+
				"set ft8.tx.caller_answer_mode to auto_first", op)
	case stderr.Is(err, ft8.ErrFdIdentityUnset):
		s.writeError(w, http.StatusBadRequest, "ft8_field_day_unset",
			"set your Field Day class and section (ft8.field_day) before answering a CQ FD", op)
	default:
		// Client-input faults (bad slot_utc, missing call) are validated and
		// mapped to 400 in the handlers BEFORE the service is called, and every
		// known service condition has a sentinel above. Anything reaching here
		// is an unexpected service/hardware fault, not malformed input: log the
		// detail and return a generic 500 — never leak err.Error() to the wire
		// or mislabel a server fault as a 400 (review 2026-06-19 M2).
		s.writeServerError(w, op, err, "internal_error", "could not start the QSO; check daemon logs")
	}
}
