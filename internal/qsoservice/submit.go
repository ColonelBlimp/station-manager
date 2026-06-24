package qsoservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderr "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// dedupeRefetchTimeout bounds the post-insert duplicate refetch on a fresh
// context — the row is already committed in sqlite by then, so this is a
// bounded pure-read; detaching from the request ctx keeps a request-deadline
// expiry from turning a known duplicate into a 500 (review 2026-05-02 M2).
const dedupeRefetchTimeout = 2 * time.Second

// Submit validates an ADIF record, checks for duplicates, and atomically
// stores the QSO and its upload-queue rows. It is the PUBLIC entry point
// (POST /v1/qso): it always mints a fresh UUIDv7 and never honours a
// caller-supplied APP_SM_QSO_ID, so a client cannot choose a QSO's canonical
// identity (identity-spoofing guard — review 2026-06-04 H1).
//
// If force is true, the dedupe check is skipped (contest edge case per
// api.md Section 4.2).
func (s *Service) Submit(ctx context.Context, logbookID int64, rec adif.Record, force bool) (SubmitResult, error) {
	// Stamp the derived MY_RIG for live logging (config.md §10): the active rig's
	// per-rig override, else its rigdef Name. Authoritative — clients no longer
	// set MY_RIG, and the single injection point covers both the phone/CW handler
	// and the FT8 e4 sink. SubmitImport deliberately does NOT stamp (it preserves
	// an imported QSO's own MY_RIG). nil-safe for test wiring without a config.
	if s.Config != nil {
		rec.MyRig = s.Config.Snapshot().ResolveMyRig()
	}
	return s.submit(ctx, logbookID, rec, force, false, nil)
}

// SubmitImport is the restore/import entry point (cmd/smd import). Unlike
// Submit it PRESERVES a valid caller-supplied UUIDv7 — carried onto qso.UUID
// by adif.RecordToQso from the record's APP_SM_QSO_ID — so an exported logbook
// re-imports with its canonical identities intact (ADR 0016; H1). A missing or
// malformed supplied UUID falls back to a freshly minted one. Kept distinct
// from Submit so only the trusted import path, never the public endpoint, can
// set a QSO's identity from the wire.
//
// forwardTo names the forwarders (by config name, case-insensitive) the
// imported QSO should be QUEUED to upload to. It is normally empty: importing
// a historical logbook must never auto-upload to QRZ/ClubLog (retrospective
// backfill is operator-driven, never automatic — ADR 0022). `smd import
// --forward qrz,…` is the explicit opt-in; bulk backfill of an already-stored
// log is the logbook SPA's job, not the importer's.
func (s *Service) SubmitImport(ctx context.Context, logbookID int64, rec adif.Record, force bool, forwardTo []string) (SubmitResult, error) {
	return s.submit(ctx, logbookID, rec, force, true, forwardTo)
}

// resolveSubmitUUID applies the UUID policy (ADR 0016; review 2026-06-04 H1):
// preserve=false (public submit) always mints a fresh UUIDv7; preserve=true
// (import/restore) keeps the supplied UUID when it is a valid UUIDv7, and mints
// otherwise. Pure + free so the policy is unit-testable without the DB path.
func resolveSubmitUUID(supplied string, preserve bool) string {
	if preserve && utils.IsValidUUIDv7(supplied) {
		return supplied
	}
	return utils.NewUUIDv7()
}

// prepareQso runs all of a submit's validation + normalization for one ADIF
// record and returns an insert-ready Qso (with DedupeKey + UUID set) plus its
// dedupe key. It performs NO database access — the logbook callsign is passed in
// (the caller looks it up: submit once per record, the bulk importer once for
// the whole run, hoisting that query out of its per-record loop). A caller-facing
// validation failure is returned as a *SubmitError. Shared by the live submit
// path and SubmitImportBatch so both validate IDENTICALLY (no drift).
func (s *Service) prepareQso(rec adif.Record, logbookID int64, logbookCallsign string, force, isImport bool) (types.Qso, string, error) {
	const op errors.Op = "qsoservice.prepareQso"

	// ---- Validate required fields ----
	call := strings.ToUpper(strings.TrimSpace(rec.Call))
	if call == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "CALL is required"}
	}
	if !IsValidCallsign(call) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: "CALL must be 3-32 characters and contain at least one digit"}
	}

	band := strings.ToLower(strings.TrimSpace(rec.Band))
	if band == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "BAND is required"}
	}

	mode := strings.ToUpper(strings.TrimSpace(rec.Mode))
	if mode == "" {
		if sub := strings.TrimSpace(rec.Submode); sub != "" {
			if resolved, ok := modes.GetModeBySubmode(sub); ok {
				mode = resolved.String()
			}
		}
		if mode == "" {
			return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "MODE is required"}
		}
	}

	qsoDate := strings.TrimSpace(rec.QsoDate)
	if qsoDate == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "QSO_DATE is required"}
	}

	timeOn := strings.TrimSpace(rec.TimeOn)
	if timeOn == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "TIME_ON is required"}
	}

	stationCallsign := strings.ToUpper(strings.TrimSpace(rec.StationCallsign))
	if stationCallsign == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "STATION_CALLSIGN is required"}
	}
	if !IsValidCallsign(stationCallsign) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: "STATION_CALLSIGN must be 3-32 characters and contain at least one digit"}
	}

	// Logbook-callsign match: live submits require STATION_CALLSIGN == the
	// logbook's callsign (forwarders reject a mismatch); import relaxes it (a
	// historical/mixed log may carry a different callsign). The logbook-exists
	// check lives in the caller (it owns the lookup that yields logbookCallsign).
	if !isImport && !strings.EqualFold(logbookCallsign, stationCallsign) {
		return types.Qso{}, "", &SubmitError{Code: "callsign_mismatch", Message: "STATION_CALLSIGN does not match the logbook's callsign"}
	}

	// ---- Validate field values ----
	if !bands.IsValidBand(band) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("BAND %q is not a recognised band", band)}
	}
	if !modes.IsValidMode(mode) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("MODE %q is not a recognised mode", mode)}
	}

	qsoDate = utils.SanitizeDateToYYYYMMDD(qsoDate)
	if qsoDate == "" || !utils.IsValidDateYYYYMMDD(qsoDate) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: "QSO_DATE is not a valid date (expected YYYYMMDD)"}
	}

	timeOn = utils.SanitizeTimeToADIF(timeOn)
	if timeOn == "" || !utils.IsValidTimeADIF(timeOn) {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: "TIME_ON is not a valid time (expected HHMM or HHMMSS)"}
	}
	timeOn = utils.TimeToHHMM(timeOn)

	timeOff := utils.TimeToHHMM(utils.SanitizeTimeToADIF(strings.TrimSpace(rec.TimeOff)))
	if timeOff == "" {
		timeOff = timeOn
	}

	qsoDateOff := utils.SanitizeDateToYYYYMMDD(strings.TrimSpace(rec.QsoDateOff))

	// ---- Validate time coherence ----
	if timeOn > timeOff {
		if qsoDateOff == "" || qsoDateOff == qsoDate {
			return types.Qso{}, "", &SubmitError{
				Code:    "invalid_time_range",
				Message: "TIME_ON is after TIME_OFF without a QSO_DATE_OFF on the following day",
			}
		}
		onDate, _ := time.Parse("20060102", qsoDate)
		offDate, _ := time.Parse("20060102", qsoDateOff)
		if !offDate.Equal(onDate.AddDate(0, 0, 1)) {
			return types.Qso{}, "", &SubmitError{
				Code:    "invalid_time_range",
				Message: fmt.Sprintf("QSO_DATE_OFF (%s) must be the day after QSO_DATE (%s) when TIME_ON is after TIME_OFF", qsoDateOff, qsoDate),
			}
		}
	}

	// ---- Normalize frequency ----
	freqStr := strings.TrimSpace(rec.Freq)
	if freqStr == "" {
		return types.Qso{}, "", &SubmitError{Code: "missing_required_field", Message: "FREQ is required"}
	}
	freqKHz, err := utils.ParseFreqMHz(freqStr)
	if err != nil {
		return types.Qso{}, "", &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("FREQ %q: %v", freqStr, err)}
	}
	freqMHz := utils.FormatFreqMHz(freqKHz)

	// ---- Build the Qso ----
	qso := adif.RecordToQso(rec, logbookID)
	qso.ContactedStation.Call = call
	qso.QsoDetails.Band = band
	qso.QsoDetails.Mode = mode
	qso.QsoDetails.QsoDate = qsoDate
	qso.QsoDetails.TimeOn = timeOn
	qso.QsoDetails.TimeOff = timeOff
	qso.QsoDetails.QsoDateOff = qsoDateOff
	qso.QsoDetails.Freq = freqMHz
	qso.LoggingStation.StationCallsign = stationCallsign

	if strings.TrimSpace(qso.ContactedStation.Country) == "" {
		qso.ContactedStation.Country = "Unknown"
	}

	// RST defaults (skipped for FT8, whose reports are dB SNR; see review M3).
	if mode != "FT8" {
		if strings.TrimSpace(qso.QsoDetails.RstSent) == "" {
			qso.QsoDetails.RstSent = "59"
		}
		if strings.TrimSpace(qso.QsoDetails.RstRcvd) == "" {
			qso.QsoDetails.RstRcvd = "59"
		}
	}

	// ---- Dedupe key ----
	dedupeKey := ComputeDedupeKey(call, band, mode, strconv.FormatInt(freqKHz, 10), qsoDate, timeOn)
	if force {
		// Force mode: a random key so the UNIQUE index never blocks the insert
		// (this QSO won't be deduplicated).
		nonce := make([]byte, dedupeKeyBytes)
		if _, err = rand.Read(nonce); err != nil {
			return types.Qso{}, "", errors.New(op).WithErr(err).WithMsg("generating force nonce")
		}
		dedupeKey = hex.EncodeToString(nonce)
	}
	qso.DedupeKey = dedupeKey

	// UUID policy (ADR 0016; H1): public Submit always mints; import preserves a
	// valid supplied UUIDv7 (carried onto qso.UUID by RecordToQso).
	qso.UUID = resolveSubmitUUID(qso.UUID, isImport)

	return qso, dedupeKey, nil
}

// submit is the shared implementation. isImport marks the trusted import/restore
// path (SubmitImport) and drives every divergence from a live/public submit:
// it preserves a valid supplied UUIDv7 (vs. always minting), relaxes the
// logbook-callsign match (a historical/mixed log may carry a different
// callsign), and enqueues upload rows only for forwarders named in forwardTo
// (default none) — importing a historical logbook must never auto-upload
// (retrospective backfill is operator-driven, never automatic; ADR 0022).
func (s *Service) submit(ctx context.Context, logbookID int64, rec adif.Record, force, isImport bool, forwardTo []string) (SubmitResult, error) {
	const op errors.Op = "qsoservice.Submit"

	// Logbook must exist; the callsign-match check (live submits only) lives in
	// prepareQso, which takes the callsign as a param. Looked up once here per
	// submit (SubmitImportBatch hoists this same lookup out of its per-record loop).
	logbookCallsign, lberr := s.DB.LogbookCallsignByIDWithContext(ctx, logbookID)
	if lberr != nil {
		if stderr.Is(lberr, errors.ErrNotFound) {
			return SubmitResult{}, &SubmitError{Code: "logbook_not_found", Message: "logbook does not exist"}
		}
		return SubmitResult{}, errors.New(op).WithErr(lberr).WithMsg("looking up logbook callsign")
	}

	qso, dedupeKey, err := s.prepareQso(rec, logbookID, logbookCallsign, force, isImport)
	if err != nil {
		return SubmitResult{}, err
	}
	call := qso.ContactedStation.Call

	// ---- Dedupe ----
	if !force {
		existing, derr := s.DB.FetchQsoByDedupeKeyWithContext(ctx, logbookID, dedupeKey)
		if derr == nil {
			return SubmitResult{Status: "duplicate", UUID: existing.UUID, ID: existing.ID}, nil
		}
		if !stderr.Is(derr, errors.ErrNotFound) {
			return SubmitResult{}, errors.New(op).WithErr(derr).WithMsg("dedupe check failed")
		}
	}

	// ---- Atomic write: QSO + upload-queue rows ----
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	qsoID, err := s.DB.InsertQsoTx(ctx, tx, qso)
	if err != nil {
		_ = tx.Rollback()

		// Race window: two submits with identical dedupe-key inputs
		// can both pass the pre-transaction FetchQsoByDedupeKey check
		// above and both try to insert. The second to hit the UNIQUE
		// index on (logbook_id, dedupe_key) WHERE deleted_at IS NULL
		// fails here. From the client's point of view this is a
		// duplicate, not a daemon error — the submit path is
		// advertised as idempotent (see api.md §4.2; the text-file
		// fallback relies on this). Translate the constraint violation
		// into a duplicate outcome.
		//
		// Refetch uses a fresh context detached from the request: by
		// this point the duplicate row is committed in sqlite, so the
		// lookup is bounded and pure-read. Inheriting `ctx` would let
		// a request-deadline expiry turn a known-duplicate into a
		// generic 500 — the M2 finding from the 2026-05-02 review.
		if sqlite.IsUniqueConstraintError(err) && !force {
			refetchCtx, refetchCancel := context.WithTimeout(context.Background(), dedupeRefetchTimeout)
			existing, ferr := s.DB.FetchQsoByDedupeKeyWithContext(refetchCtx, logbookID, dedupeKey)
			refetchCancel()
			if ferr == nil {
				return SubmitResult{Status: "duplicate", UUID: existing.UUID, ID: existing.ID}, nil
			}
			// If the row isn't there on the follow-up query, something
			// stranger is going on — fall through to surface the
			// original insert error.
		}

		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to insert QSO")
	}

	// Insert upload-queue rows for each CONFIGURED forwarder whose
	// action_filter includes 'insert' (ADR 0022: enqueue is gated by config
	// presence + action_filter, NOT the Enabled flag — Enabled gates only
	// worker lifecycle, so a disabled forwarder still accrues pending rows).
	// Inside the same transaction as the QSO insert per the one-fails-all-fail
	// invariant (see docs/v2-design/forwarding.md §1). Zero forwarders
	// configured → the loop is a no-op and only the QSO row is committed.
	for _, fwd := range s.Config.Forwarders() {
		if !shouldEnqueue(fwd, action.Insert) {
			continue
		}
		// Import enqueues only for forwarders the operator explicitly named via
		// `smd import --forward`; default import (empty forwardTo) uploads
		// nothing. The live/public path passes nil forwardTo and isImport=false,
		// so this gate never trips for it.
		if isImport && !forwarderNamed(forwardTo, fwd.Name) {
			continue
		}
		if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, fwd.Name, fwd.Type); err != nil {
			_ = tx.Rollback()
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	if err = tx.Commit(); err != nil {
		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", qsoID).
		Str("uuid", qso.UUID).
		Int64("logbook_id", logbookID).
		Str("call", call).
		Str("qso_date", qso.QsoDetails.QsoDate).
		Str("time_on", qso.QsoDetails.TimeOn).
		Str("freq_mhz", qso.QsoDetails.Freq).
		Str("band", qso.QsoDetails.Band).
		Str("mode", qso.QsoDetails.Mode).
		Msg("QSO stored")

	s.Hub.Publish(events.NameQsoStored, events.QsoStoredPayload{
		QsoID:     qsoID,
		LogbookID: logbookID,
	})

	// Best-effort contacted_station upsert — the second write path
	// per ADR 0017 #10. Outside the QSO transaction (cache writes
	// are best-effort per the one-fails-all-fail invariant): the QSO
	// is already committed, so a cache-write failure here logs a
	// warning but doesn't fail the submit response.
	//
	// Subsequent enrichment Tabs may overwrite the country fields
	// with hamnut's truth per ADR 0017 #11; the operator's QSO row
	// preserves what they typed regardless.
	if uerr := s.DB.UpsertContactedStationWithContext(ctx, qso.ContactedStation); uerr != nil {
		s.Logger.WarnWith().
			Err(uerr).
			Str("call", call).
			Msg("contacted_station upsert failed (best-effort, QSO already stored)")
	}

	return SubmitResult{Status: "stored", UUID: qso.UUID, ID: qsoID}, nil
}

// (isUniqueConstraintError moved to sqlite.IsUniqueConstraintError —
// see docs/decisions/0018-sqlite-driver-modernc.md. The previous
// local copy detected the wrong driver's typed error type — daemon
// uses modernc.org/sqlite, not mattn/go-sqlite3 — and fell through
// to the substring fallback at runtime.)
