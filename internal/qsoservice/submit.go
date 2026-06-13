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
	return s.submit(ctx, logbookID, rec, force, false)
}

// SubmitImport is the restore/import entry point (cmd/smd import). Unlike
// Submit it PRESERVES a valid caller-supplied UUIDv7 — carried onto qso.UUID
// by adif.RecordToQso from the record's APP_SM_QSO_ID — so an exported logbook
// re-imports with its canonical identities intact (ADR 0016; H1). A missing or
// malformed supplied UUID falls back to a freshly minted one. Kept distinct
// from Submit so only the trusted import path, never the public endpoint, can
// set a QSO's identity from the wire.
func (s *Service) SubmitImport(ctx context.Context, logbookID int64, rec adif.Record, force bool) (SubmitResult, error) {
	return s.submit(ctx, logbookID, rec, force, true)
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

// submit is the shared implementation. preserveUUID selects the UUID policy:
// false (public) always mints; true (import) keeps a valid supplied UUIDv7.
func (s *Service) submit(ctx context.Context, logbookID int64, rec adif.Record, force, preserveUUID bool) (SubmitResult, error) {
	const op errors.Op = "qsoservice.Submit"

	// ---- Validate required fields ----
	call := strings.ToUpper(strings.TrimSpace(rec.Call))
	if call == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "CALL is required"}
	}
	if !IsValidCallsign(call) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: "CALL must be 3-32 characters and contain at least one digit"}
	}

	band := strings.ToLower(strings.TrimSpace(rec.Band))
	if band == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "BAND is required"}
	}

	mode := strings.ToUpper(strings.TrimSpace(rec.Mode))
	if mode == "" {
		// Try to resolve from submode
		if sub := strings.TrimSpace(rec.Submode); sub != "" {
			if resolved, ok := modes.GetModeBySubmode(sub); ok {
				mode = resolved.String()
			}
		}
		if mode == "" {
			return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "MODE is required"}
		}
	}

	qsoDate := strings.TrimSpace(rec.QsoDate)
	if qsoDate == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "QSO_DATE is required"}
	}

	timeOn := strings.TrimSpace(rec.TimeOn)
	if timeOn == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "TIME_ON is required"}
	}

	stationCallsign := strings.ToUpper(strings.TrimSpace(rec.StationCallsign))
	if stationCallsign == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "STATION_CALLSIGN is required"}
	}
	if !IsValidCallsign(stationCallsign) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: "STATION_CALLSIGN must be 3-32 characters and contain at least one digit"}
	}

	// ---- Validate field values ----
	if !bands.IsValidBand(band) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("BAND %q is not a recognised band", band)}
	}

	if !modes.IsValidMode(mode) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("MODE %q is not a recognised mode", mode)}
	}

	qsoDate = utils.SanitizeDateToYYYYMMDD(qsoDate)
	if qsoDate == "" || !utils.IsValidDateYYYYMMDD(qsoDate) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: "QSO_DATE is not a valid date (expected YYYYMMDD)"}
	}

	timeOn = utils.SanitizeTimeToADIF(timeOn)
	if timeOn == "" || !utils.IsValidTimeADIF(timeOn) {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: "TIME_ON is not a valid time (expected HHMM or HHMMSS)"}
	}

	// Sanitize timeOff if present
	timeOff := utils.SanitizeTimeToADIF(strings.TrimSpace(rec.TimeOff))
	if timeOff == "" {
		timeOff = timeOn // Default to timeOn if not provided
	}

	// Sanitize qsoDateOff if present
	qsoDateOff := utils.SanitizeDateToYYYYMMDD(strings.TrimSpace(rec.QsoDateOff))

	// ---- Validate time coherence ----
	// If TIME_ON > TIME_OFF the QSO crossed midnight, which is only valid
	// when QSO_DATE_OFF is the following day.
	if timeOn > timeOff {
		if qsoDateOff == "" || qsoDateOff == qsoDate {
			return SubmitResult{}, &SubmitError{
				Code:    "invalid_time_range",
				Message: "TIME_ON is after TIME_OFF without a QSO_DATE_OFF on the following day",
			}
		}
		onDate, _ := time.Parse("20060102", qsoDate)
		offDate, _ := time.Parse("20060102", qsoDateOff)
		if !offDate.Equal(onDate.AddDate(0, 0, 1)) {
			return SubmitResult{}, &SubmitError{
				Code:    "invalid_time_range",
				Message: fmt.Sprintf("QSO_DATE_OFF (%s) must be the day after QSO_DATE (%s) when TIME_ON is after TIME_OFF", qsoDateOff, qsoDate),
			}
		}
	}

	// ---- Normalize frequency ----
	// ADIF freq is MHz as a string (e.g. "14.074"). types.Qso.Freq follows
	// the ADIF spec and stores MHz too — the kHz integer is only the DB
	// storage unit and lives below the adapter. We keep the int-kHz form
	// around locally for the dedupe hash (deterministic numeric
	// representation) and format the canonical MHz string for the Qso.
	freqStr := strings.TrimSpace(rec.Freq)
	if freqStr == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "FREQ is required"}
	}
	freqKHz, err := utils.ParseFreqMHz(freqStr)
	if err != nil {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("FREQ %q: %v", freqStr, err)}
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

	// Country: use whatever was in the record, or empty string as fallback.
	// The schema requires country NOT NULL, so provide a default.
	if strings.TrimSpace(qso.ContactedStation.Country) == "" {
		qso.ContactedStation.Country = "Unknown"
	}

	// RST defaults
	if strings.TrimSpace(qso.QsoDetails.RstSent) == "" {
		qso.QsoDetails.RstSent = "59"
	}
	if strings.TrimSpace(qso.QsoDetails.RstRcvd) == "" {
		qso.QsoDetails.RstRcvd = "59"
	}

	// ---- Dedupe ----
	// Hash input uses the int-kHz string, so the hash is deterministic
	// regardless of how the caller wrote the MHz decimal ("7.050" /
	// "7.0500" / "7050" all collapse to the same integer).
	dedupeKey := ComputeDedupeKey(call, band, mode, strconv.FormatInt(freqKHz, 10), qsoDate, timeOn)

	if force {
		// Force mode: generate a unique dedupe key so the UNIQUE index
		// doesn't block the insert. The key is still a valid 64-char hex
		// string but is not reproducible — this QSO won't be deduplicated.
		nonce := make([]byte, dedupeKeyBytes)
		if _, err = rand.Read(nonce); err != nil {
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("generating force nonce")
		}
		dedupeKey = hex.EncodeToString(nonce)
	} else {
		existing, err := s.DB.FetchQsoByDedupeKeyWithContext(ctx, logbookID, dedupeKey)
		if err == nil {
			return SubmitResult{Status: "duplicate", UUID: existing.UUID, ID: existing.ID}, nil
		}
		if !stderr.Is(err, errors.ErrNotFound) {
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("dedupe check failed")
		}
	}

	qso.DedupeKey = dedupeKey
	// UUID policy (ADR 0016; review 2026-06-04 H1): public Submit always mints;
	// SubmitImport preserves a valid supplied UUIDv7 (carried onto qso.UUID by
	// RecordToQso) so canonical identity round-trips on restore/import. Dedupe
	// is by content key (above), not UUID, so re-importing identical QSOs still
	// dedupes before reaching here; a preserved UUID that collides with a
	// different existing row is an out-of-scope edge surfaced by the insert.
	qso.UUID = resolveSubmitUUID(qso.UUID, preserveUUID)

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
		Str("qso_date", qsoDate).
		Str("time_on", timeOn).
		Str("freq_mhz", freqMHz).
		Str("band", band).
		Str("mode", mode).
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
