package api

import (
	"context"
	stderr "errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// currentStationIdentity resolves, from ONE config snapshot, the FT8 station
// identity to pin at arm (ADR 0055): the callsign FT8 transmits + logs under
// (the CURRENT logbook's callsign) AND the logbook_id that call came from. Both
// are pinned into the FT8 session so the QSO logs to the book it started under,
// even if the shell's current-logbook selection changes mid-exchange.
//
// The callsign fails CLOSED on a transient DB error: falling back to the config
// call there could transmit the wrong call while qsoservice logs the logbook's
// once the DB recovers (codex review of c93da89b, #1). The config fallback
// applies only when the logbook is genuinely absent (pre-setup) or empty; any
// other error returns callsign "" so no session is started and no PTT is keyed.
// logbookID is always the current default. It is needed both when the callsign
// resolves (pinned into the session at arm) and when the datastore fails, where it
// names the logbook that could not be read on the diagnostic record.
// The returned error is non-nil ONLY for an unexpected datastore failure, and
// that is the whole point of it: an empty callsign with a nil error means the
// station is genuinely unconfigured, which is a different fault demanding a
// different action from the operator. Collapsing the two — as this did until
// 2026-08-01 — reported a broken database as unset configuration and sent the
// operator to a Settings screen to fix a field that was already correct
// (internal/api logging audit, finding A7).
//
// Fail-closed is unchanged: a datastore error still yields no callsign, so no
// session is started and no PTT is keyed on one. Note what this does NOT do — these
// routes require TX to be armed already, and refusing here does not disarm it.
func (s *Server) currentStationIdentity(ctx context.Context) (callsign string, logbookID int64, err error) {
	snap := s.cfg.Snapshot()
	logbookID = snap.DefaultLogbookID
	lbCall, dbErr := s.db.LogbookCallsignByIDWithContext(ctx, logbookID)
	if dbErr == nil {
		if c := strings.TrimSpace(lbCall); c != "" {
			return c, logbookID, nil
		}
	}
	if dbErr == nil || stderr.Is(dbErr, errors.ErrNotFound) {
		return strings.TrimSpace(snap.LoggingStation.StationCallsign), logbookID, nil
	}
	return "", logbookID, dbErr
}

// writeStationIdentityUnavailable records why the datastore could not be read and
// refuses the request with 503.
//
// NOT writeServerError, which hardcodes 500 (httpkit.go:99). This is a 503: the
// daemon is healthy and its datastore is not, which is the distinction
// handler_health.go already draws with the same `db_unavailable` code — so the
// operator meets one name for one condition rather than two.
//
// The cause, the logbook that could not be read and the operation all go on the
// record. Without them the log says only that something failed, which is where
// finding A7 found things.
func (s *Server) writeStationIdentityUnavailable(
	w http.ResponseWriter, op errors.Op, err error, logbookID int64,
) {
	s.logger.ErrorWith().Err(err).Str("op", string(op)).Int64("logbook_id", logbookID).
		Str("code", "db_unavailable").
		Msg("station identity unreadable; refusing to start an ft8 session")
	s.writeError(w, http.StatusServiceUnavailable, "db_unavailable",
		"database is not reachable", op)
}

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
	// AllowDuplicate is the operator's EXPLICIT "work this station again" intent. SM
	// deduplicates on call+band+mode+freq+date+HH:MM, so without it a deliberate second
	// contact inside one minute is folded into the first and never stored — the
	// operator transmits a full exchange and sees no row. Reachable on the short
	// ladders (work-a-caller, single-rung type-4). Absent/false keeps the duplicate
	// protection; the SPA sets it only when the operator acted on a station it already
	// shows as worked this session.
	AllowDuplicate bool `json:"allow_duplicate,omitempty"`
	// AutoWork is the operator's per-click auto-work intent (ADR 0065): true arms an
	// auto-work-callers run alongside this contact (the ctrl+shift gesture or the
	// Auto-work toggle), gated daemon-side on ft8.tx.auto_work_callers. Absent/false
	// works this station only — and clears any run a previous session armed.
	// Standard exchange mode only; FD and type-4 sessions never arm a run.
	AutoWork bool `json:"auto_work,omitempty"`
	// AnswerMode is the SESSION's answerer-selection mode (ADR 0066) an armed
	// auto-work run would select the next caller with. Empty = the config
	// default; junk is a 400. Only meaningful alongside auto_work — a plain
	// start ignores it.
	AnswerMode string `json:"answer_mode,omitempty"`
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

	if !validFt8AnswerMode(req.AnswerMode) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"answer_mode must be auto_first, auto_strongest or operator_pick", op)
		return
	}
	ls := s.cfg.Snapshot().LoggingStation
	ourCall, logbookID, idErr := s.currentStationIdentity(r.Context())
	if idErr != nil {
		s.writeStationIdentityUnavailable(w, op, idErr, logbookID)
		return
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
			req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate)
	case "type4":
		// Reduced type-4 (nonstandard/compound call, ADR 0048): no grid/report on the
		// wire, so we log the measured SNR as RST_SENT (like FD).
		err = s.ft8.StartQsoT4(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate)
	default:
		err = s.ft8.StartQso(ourCall, ls.MyGridsquare, req.TheirCall, req.TheirGrid, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate, req.AutoWork, req.AnswerMode)
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
	// AnswerMode is the SESSION's answerer-selection mode (ADR 0066): one of the
	// three ft8.tx.caller_answer_mode literals, chosen in the TX control bar's
	// Answer selector per run. Empty = the config default (old clients keep the
	// pre-0066 behaviour); any other junk is a 400. Operating state, like
	// TxParity — config.json holds only the default that seeds the selector.
	AnswerMode string `json:"answer_mode,omitempty"`
}

// validFt8AnswerMode accepts an ABSENT session answer mode (empty — the config
// default applies, ADR 0066) or one of the three caller_answer_mode literals.
// Junk is a client bug and 400s rather than silently resolving to a default the
// operator never chose.
func validFt8AnswerMode(m string) bool {
	return m == "" || types.Ft8CallerAnswerModeValid(m)
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
	if !validFt8AnswerMode(req.AnswerMode) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"answer_mode must be auto_first, auto_strongest or operator_pick", op)
		return
	}

	ls := s.cfg.Snapshot().LoggingStation
	ourCall, logbookID, idErr := s.currentStationIdentity(r.Context())
	if idErr != nil {
		s.writeStationIdentityUnavailable(w, op, idErr, logbookID)
		return
	}
	if ourCall == "" {
		s.writeError(w, http.StatusBadRequest, "no_station_callsign",
			"set your station callsign in My Station before calling CQ", op)
		return
	}

	if err := s.ft8.StartCallCq(ourCall, ls.MyGridsquare, req.OffsetHz, req.OperatingFreqMHz, req.AnswerMode, req.TxParity, logbookID); err != nil {
		s.writeFt8QsoError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ft8CqPickRequest is the POST /v1/ft8/cq/pick body (ADR 0065 decision 3): commit
// the listed operator_pick answerer `call` into the live Call-CQ run. Everything
// else about the contact (grid, SNR, offset, dial) is daemon-owned — the candidate
// list the SPA renders came from the daemon's ft8-qso frames in the first place.
type ft8CqPickRequest struct {
	Call string `json:"call"`
}

// handleFt8CqPick commits a listed answerer into an operator_pick Call-CQ run
// (sequencing rules in internal/ft8/operatorpick_test.go). A 202 means the run
// will transmit our report to that station at its next slot evaluation; the
// commit itself is confirmed by push (the ft8-qso SSE frame published by the
// pop). Refusals: 409 ft8_no_cq_pick_run / 404 ft8_answerer_not_listed /
// 409 ft8_cq_contact_in_flight.
func (s *Server) handleFt8CqPick(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8CqPick"

	var req ft8CqPickRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	if strings.TrimSpace(req.Call) == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "call is required", op)
		return
	}
	if err := s.ft8.PickAnswerer(req.Call); err != nil {
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
	// AllowDuplicate is the operator's EXPLICIT "work this station again" intent. SM
	// deduplicates on call+band+mode+freq+date+HH:MM, so without it a deliberate second
	// contact inside one minute is folded into the first and never stored — the
	// operator transmits a full exchange and sees no row. Reachable on the short
	// ladders (work-a-caller, single-rung type-4). Absent/false keeps the duplicate
	// protection; the SPA sets it only when the operator acted on a station it already
	// shows as worked this session.
	AllowDuplicate bool `json:"allow_duplicate,omitempty"`
	// AutoWork is the operator's per-click auto-work intent (ADR 0065): true arms an
	// auto-work-callers run alongside this contact (the ctrl+shift gesture or the
	// Auto-work toggle), gated daemon-side on ft8.tx.auto_work_callers. Absent/false
	// works this station only — and clears any run a previous session armed.
	// Standard exchange mode only; FD and type-4 sessions never arm a run.
	AutoWork bool `json:"auto_work,omitempty"`
	// AnswerMode is the SESSION's answerer-selection mode (ADR 0066) an armed
	// auto-work run would select the next caller with. Empty = the config
	// default; junk is a 400. Only meaningful alongside auto_work — a plain
	// start ignores it.
	AnswerMode string `json:"answer_mode,omitempty"`
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

	if !validFt8AnswerMode(req.AnswerMode) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"answer_mode must be auto_first, auto_strongest or operator_pick", op)
		return
	}
	// TX identity is the CURRENT logbook's callsign (ADR 0055) — never OPERATOR
	// (a club station's operator differs from its station call). Empty means the
	// logbook can't be resolved (pre-setup, or a fail-closed transient DB error)
	// → refuse to transmit rather than key the wrong call (codex review of
	// 23907ffd, #1).
	ourCall, logbookID, idErr := s.currentStationIdentity(r.Context())
	if idErr != nil {
		s.writeStationIdentityUnavailable(w, op, idErr, logbookID)
		return
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
			req.TheirClass, req.TheirSection, req.TheirSnr, req.SlotUTC, req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate)
	case "type4":
		// Reduced type-4 (nonstandard/compound caller, ADR 0048): no report on the wire.
		err = s.ft8.StartWorkCallerT4(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate)
	default:
		err = s.ft8.StartWorkCaller(ourCall, req.TheirCall, req.TheirGrid, req.TheirSnr, req.SlotUTC,
			req.OffsetHz, req.OperatingFreqMHz, logbookID, req.AllowDuplicate, req.AutoWork, req.AnswerMode)
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
// {"armed": bool}. Arming is refused when the CURRENT RUNG has no skip path:
// 409 ft8_no_active_qso when nothing is running, 409 ft8_rung_not_skippable when
// a session is but is on a terminal rung (or a Call-CQ run, whose Next is an
// immediate takeover). Disarm is always accepted. The armed state comes back via
// the ft8-qso SSE (skip_armed).
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

// handleFt8QsoNext short-circuits the repeat cap on a stuck Call-CQ contact: park
// this answerer at the next slot evaluation, then work another live answerer from
// that slot or resume calling CQ. The run continues — ending it is Abandon's job.
//
// Deliberately NOT the skip route. Skip fires on a SILENT cycle; the case this exists
// for is a station that keeps transmitting the same rung and never advances, so a
// skip-shaped trigger would never fire. No body. 202 on success; 409 ft8_no_answerer
// when no answerer is being worked (idle, merely calling CQ, or an answer/work session
// whose Next is skip). The pending state comes back via the ft8-qso SSE (next_armed).
func (s *Server) handleFt8QsoNext(w http.ResponseWriter, _ *http.Request) {
	const op errors.Op = "api.handleFt8QsoNext"
	if err := s.ft8.NextAnswerer(); err != nil {
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

// handleFt8AutoWorkStop disarms an armed auto-work run WITHOUT ending any active
// contact — the Auto-work pill's click action (ADR 0065). Distinct from abandon,
// which stops both. Idempotent — stopping a stopped run is a 202 no-op.
func (s *Server) handleFt8AutoWorkStop(w http.ResponseWriter, _ *http.Request) {
	s.ft8.StopAutoWorkRun()
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
	case stderr.Is(err, ft8.ErrTxDialUnknown):
		// Distinct from rig_not_ready: the rig IS connected and armable, but the
		// daemon cannot read the frequency it is on, so it will not key or log a
		// contact it cannot place. Clears once the bridge decodes the VFO.
		s.writeError(w, http.StatusServiceUnavailable, "rig_dial_unknown",
			"rig frequency not known yet; try again in a moment", op)
	case stderr.Is(err, ft8.ErrTxInFlight):
		// A transmission (a manual send, or a just-finished session's draining tail)
		// is in flight and no session is active — a new session can't start atop live
		// RF or its opening rung would collide and drop. (A duplicate start atop an
		// ACTIVE session is ErrQsoInProgress, handled above, not this.)
		s.writeError(w, http.StatusConflict, "ft8_tx_in_flight",
			"a transmission is in flight; wait for it to finish or abandon it", op)
	case stderr.Is(err, ft8.ErrNoActiveQso):
		s.writeError(w, http.StatusConflict, "ft8_no_active_qso",
			"no active QSO to arm a skip on", op)
	case stderr.Is(err, ft8.ErrNoAnswerer):
		// A third distinct refusal alongside ft8_no_active_qso and
		// ft8_rung_not_skippable: something IS running (or nothing is), but no answerer
		// is being worked, so there is nobody to move on from.
		s.writeError(w, http.StatusConflict, "ft8_no_answerer",
			"no station is being worked; Next moves on from a contact, and there isn't one", op)
	case stderr.Is(err, ft8.ErrRungNotSkippable):
		// Distinct from ft8_no_active_qso: a session IS running, but it is on a rung
		// with no skip path (a terminal RR73/73), so an arm could never fire. Naming
		// Abandon matters — the operator reaching for skip wants the radio to stop,
		// and this is the control that does it here.
		s.writeError(w, http.StatusConflict, "ft8_rung_not_skippable",
			"this rung cannot be skipped — it is the closing message; use Abandon to end the contact", op)
	case stderr.Is(err, ft8.ErrQsoInProgress):
		s.writeError(w, http.StatusConflict, "ft8_qso_in_progress",
			"a QSO is already in progress; abandon it first", op)
	case stderr.Is(err, ft8.ErrNoOffset):
		s.writeError(w, http.StatusBadRequest, "ft8_no_offset",
			"pick a clear TX offset before starting a QSO", op)
	case stderr.Is(err, ft8.ErrStaleDecode):
		// CONFLICT, not bad input: the request was well formed and was valid when
		// the row was rendered — the world moved on. A 400 would tell the operator
		// they sent something wrong and send them looking at the SPA for a fault
		// that is not there. Band Activity retains decodes by count, not age, so on
		// a quiet band a station that left minutes ago still has a clickable row
		// (dogfood 2026-07-31: six rungs transmitted at a station last heard 5m31s
		// earlier). The SPA greys those rows; this is the guarantee behind it.
		s.writeError(w, http.StatusConflict, "ft8_stale_decode",
			"that decode is too old to work — the station may have left the air; "+
				"wait for a fresh decode", op)
	case stderr.Is(err, ft8.ErrSlotInFuture):
		// BAD INPUT, not a conflict: unlike a stale decode this was never valid —
		// no decode can be reported from a slot that has not happened. Kept apart
		// from ft8_stale_decode because the operator's action differs: fix a clock,
		// rather than wait for a fresh decode from a station that is fine.
		s.writeError(w, http.StatusBadRequest, "ft8_slot_in_future",
			"that decode's slot time is in the future — check the clock on this "+
				"machine and the daemon's", op)
	case stderr.Is(err, ft8.ErrTxBadOffset):
		s.writeError(w, http.StatusBadRequest, "ft8_bad_offset",
			"TX offset is outside the usable passband", op)
	case stderr.Is(err, ft8.ErrTxBadMessage):
		s.writeError(w, http.StatusBadRequest, "ft8_tx_bad_message",
			"that station can't be worked on FT8 — only standard messages transmit "+
				"(compound/portable calls and free text can't be encoded)", op)
	// The pop's three refusals (ADR 0065; internal/ft8/operatorpick_test.go rule 5)
	// stay distinct on the wire because the operator's next action differs: nothing
	// to do / wait for a fresh answer / finish the contact or press Next first.
	// Not-listed is a 404 — the named resource is gone (the station stopped
	// calling or was never heard), which is a fact about the world, not a conflict
	// with the run's state.
	case stderr.Is(err, ft8.ErrNoCqPickRun):
		s.writeError(w, http.StatusConflict, "ft8_no_cq_pick_run",
			"no operator-pick Call-CQ run is live", op)
	case stderr.Is(err, ft8.ErrAnswererNotListed):
		s.writeError(w, http.StatusNotFound, "ft8_answerer_not_listed",
			"that station is no longer answering your CQ — pick a listed one", op)
	case stderr.Is(err, ft8.ErrCqContactInFlight):
		s.writeError(w, http.StatusConflict, "ft8_cq_contact_in_flight",
			"the run is already working a contact — finish it or press Next first", op)
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
