package qsoservice

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// preserveSeconds protects stored seconds on an edit: when an incoming time
// arrives at coarse HHMM precision but the stored value has HHMMSS for the SAME
// minute, the stored (seconds-bearing) value is kept — editing an unrelated
// field must never silently drop precision the QSO already had. An incoming
// value that changes the minute, or that itself carries seconds, wins. Empty
// incoming falls through to the caller's required-field validation.
func preserveSeconds(incoming, existing string) string {
	if len(incoming) == 4 && len(existing) == 6 && existing[:4] == incoming {
		return existing
	}
	return incoming
}

// Update overlays a JSON patch body onto existing, validates the merged
// QSO, recomputes the dedupe key if any of its inputs changed (and rejects
// on collision), then persists. Returns the updated QSO.
//
// JSON keys not present in the body leave the corresponding field
// unchanged. Immutable fields — QSO identity, logbook, station callsign,
// dedupe key, forwarding state, enrichment — are always restored from
// existing, so clients cannot rewrite them via PATCH.
//
// Validation errors come back as *SubmitError. A dedupe collision is
// reported as *SubmitError with Code "duplicate_key" so the handler maps
// it to 409.
//
// src identifies which subsystem of the daemon initiated the edit; it
// is recorded on the qso_history audit row written inside the same
// transaction as the QSO update (ADR 0016 prep #2). The audit row's
// before_image is json.Marshal(existing) — the pre-edit snapshot, not
// the merged result — so replaying audit gives the row's history of
// states.
func (s *Service) Update(ctx context.Context, existing types.Qso, body []byte, src source.Source) (types.Qso, error) {
	const op errors.Op = "qsoservice.Update"

	merged := existing
	if err := json.Unmarshal(body, &merged); err != nil {
		return types.Qso{}, &SubmitError{Code: "invalid_json", Message: "failed to parse request body"}
	}

	// ---- Restore immutables ----
	// Structural identity — set at creation, never editable.
	merged.ID = existing.ID
	merged.UUID = existing.UUID
	merged.LogbookID = existing.LogbookID
	merged.DedupeKey = existing.DedupeKey
	merged.LoggingStation.StationCallsign = existing.LoggingStation.StationCallsign
	// Forwarding state — owned by the forwarder, not the client.
	merged.SmQsoUploadDate = existing.SmQsoUploadDate
	merged.SmQsoUploadStatus = existing.SmQsoUploadStatus
	merged.SmFwrdByEmailDate = existing.SmFwrdByEmailDate
	merged.SmFwrdByEmailStatus = existing.SmFwrdByEmailStatus
	merged.QrzComUploadDate = existing.QrzComUploadDate
	merged.QrzComUploadStatus = existing.QrzComUploadStatus
	// Enrichment — populated by services, not user input.
	merged.CountryDetails = existing.CountryDetails
	merged.ContactHistory = existing.ContactHistory

	// ---- Normalize ----
	// Idempotent on canonical form: existing stored data is already in
	// canonical form (Submit does the same normalizations), so this only
	// has effect on fields the patch actually touched.
	merged.ContactedStation.Call = strings.ToUpper(strings.TrimSpace(merged.ContactedStation.Call))
	merged.QsoDetails.Band = strings.ToLower(strings.TrimSpace(merged.QsoDetails.Band))
	merged.QsoDetails.Mode = strings.ToUpper(strings.TrimSpace(merged.QsoDetails.Mode))
	merged.QsoDetails.Submode = strings.TrimSpace(merged.QsoDetails.Submode)
	merged.QsoDetails.QsoDate = utils.SanitizeDateToYYYYMMDD(strings.TrimSpace(merged.QsoDetails.QsoDate))
	if raw := strings.TrimSpace(merged.QsoDetails.QsoDateOff); raw != "" {
		merged.QsoDetails.QsoDateOff = utils.SanitizeDateToYYYYMMDD(raw)
	} else {
		merged.QsoDetails.QsoDateOff = ""
	}
	// Store time at native precision (HHMM or HHMMSS) — no longer truncated. An
	// edit that arrives at coarser HHMM for the SAME minute keeps the stored
	// seconds (preserveSeconds), so editing an unrelated field on an FT8 QSO can't
	// silently drop its seconds. A changed minute, or supplied seconds, wins.
	merged.QsoDetails.TimeOn = preserveSeconds(
		utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOn)), existing.QsoDetails.TimeOn)
	merged.QsoDetails.TimeOff = preserveSeconds(
		utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOff)), existing.QsoDetails.TimeOff)
	merged.QsoDetails.RstSent = strings.TrimSpace(merged.QsoDetails.RstSent)
	merged.QsoDetails.RstRcvd = strings.TrimSpace(merged.QsoDetails.RstRcvd)
	merged.ContactedStation.Country = strings.TrimSpace(merged.ContactedStation.Country)
	if freq := strings.TrimSpace(merged.QsoDetails.Freq); freq != "" {
		kHz, err := utils.ParseFreqMHz(freq)
		if err != nil {
			return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("freq %q: %v", freq, err)}
		}
		merged.QsoDetails.Freq = utils.FormatFreqMHz(kHz)
		// Band is a function of frequency, so derive it from the canonical freq —
		// the edit overlay sends the OLD band on a VFO freq change, which would
		// otherwise persist an impossible BAND/FREQ pair (and a wrong dedupe key,
		// and contradictory ADIF to forwarders). Review 2026-06-19 M2. Only
		// overwrite when the freq maps to a known band; an out-of-band freq keeps
		// the supplied band so the band validation below still catches it.
		if derived := utils.FrequencyToBand(merged.QsoDetails.Freq); derived != "" {
			merged.QsoDetails.Band = strings.ToLower(derived)
		}
	}

	// ---- Validate required-field invariants on the merged result ----
	if merged.ContactedStation.Call == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "call cannot be empty"}
	}
	if !IsValidCallsign(merged.ContactedStation.Call) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "call must be 3-32 characters and contain at least one digit"}
	}
	if merged.QsoDetails.Band == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "band cannot be empty"}
	}
	if !bands.IsValidBand(merged.QsoDetails.Band) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("band %q is not a recognised band", merged.QsoDetails.Band)}
	}
	if merged.QsoDetails.Mode == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "mode cannot be empty"}
	}
	if !modes.IsValidMode(merged.QsoDetails.Mode) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("mode %q is not a recognised mode", merged.QsoDetails.Mode)}
	}
	if merged.QsoDetails.QsoDate == "" || !utils.IsValidDateYYYYMMDD(merged.QsoDetails.QsoDate) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "qso_date is not a valid date (expected YYYYMMDD)"}
	}
	if merged.QsoDetails.QsoDateOff != "" && !utils.IsValidDateYYYYMMDD(merged.QsoDetails.QsoDateOff) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "qso_date_off is not a valid date (expected YYYYMMDD)"}
	}
	if merged.QsoDetails.TimeOn == "" || !utils.IsValidTimeADIF(merged.QsoDetails.TimeOn) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "time_on is not a valid time (expected HHMM or HHMMSS)"}
	}
	if merged.QsoDetails.TimeOff == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "time_off cannot be empty"}
	}
	if !utils.IsValidTimeADIF(merged.QsoDetails.TimeOff) {
		return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: "time_off is not a valid time (expected HHMM or HHMMSS)"}
	}
	// RST is mode-aware (review 2026-06-19 M1). FT8 reports are signed SNR in dB
	// and a caller-side bare-roger QSO can legitimately have no RST_RCVD — Submit
	// leaves FT8 reports empty rather than fabricating "59", so Update must not
	// reject an empty FT8 report either (otherwise a no-op or comment-only edit of
	// a valid bare-roger FT8 QSO fails with a 400). Phone/CW keep the requirement.
	if merged.QsoDetails.Mode != "FT8" {
		if merged.QsoDetails.RstSent == "" {
			return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_sent cannot be empty"}
		}
		if merged.QsoDetails.RstRcvd == "" {
			return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_rcvd cannot be empty"}
		}
	}
	if merged.ContactedStation.Country == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "country cannot be empty"}
	}

	// ---- Time coherence ----
	// Minute precision so a mixed HHMM/HHMMSS pair can't trip a false overnight
	// error via lexical comparison of unequal widths.
	if utils.TimeToHHMM(merged.QsoDetails.TimeOn) > utils.TimeToHHMM(merged.QsoDetails.TimeOff) {
		if merged.QsoDetails.QsoDateOff == "" || merged.QsoDetails.QsoDateOff == merged.QsoDetails.QsoDate {
			return types.Qso{}, &SubmitError{
				Code:    "invalid_time_range",
				Message: "time_on is after time_off without a qso_date_off on the following day",
			}
		}
		onDate, _ := time.Parse("20060102", merged.QsoDetails.QsoDate)
		offDate, _ := time.Parse("20060102", merged.QsoDetails.QsoDateOff)
		if !offDate.Equal(onDate.AddDate(0, 0, 1)) {
			return types.Qso{}, &SubmitError{
				Code: "invalid_time_range",
				Message: fmt.Sprintf("qso_date_off (%s) must be the day after qso_date (%s) when time_on is after time_off",
					merged.QsoDetails.QsoDateOff, merged.QsoDetails.QsoDate),
			}
		}
	}

	// ---- Dedupe recompute + collision check ----
	// TimeOn compared at minute precision — the dedupe key is minute-based, so a
	// seconds-only edit doesn't change the key and needn't trigger a recompute.
	dedupeChanged := merged.ContactedStation.Call != existing.ContactedStation.Call ||
		merged.QsoDetails.Band != existing.QsoDetails.Band ||
		merged.QsoDetails.Mode != existing.QsoDetails.Mode ||
		merged.QsoDetails.Freq != existing.QsoDetails.Freq ||
		merged.QsoDetails.QsoDate != existing.QsoDetails.QsoDate ||
		utils.TimeToHHMM(merged.QsoDetails.TimeOn) != utils.TimeToHHMM(existing.QsoDetails.TimeOn)

	if dedupeChanged {
		// Hash input uses the int-kHz string for determinism — the same
		// contract as Submit. merged.Freq is canonical MHz (set above),
		// so this parse cannot fail.
		kHz, _ := utils.ParseFreqMHz(merged.QsoDetails.Freq)
		newKey := ComputeDedupeKey(
			merged.ContactedStation.Call,
			merged.QsoDetails.Band,
			merged.QsoDetails.Mode,
			strconv.FormatInt(kHz, 10),
			merged.QsoDetails.QsoDate,
			utils.TimeToHHMM(merged.QsoDetails.TimeOn),
		)
		// Pre-tx collision check is a fast-path optimization: it avoids
		// the BeginTx + UpdateQsoTx + rollback round-trip in the common
		// case where the operator's edit obviously collides with an
		// existing row. The cross-handler race window (a concurrent
		// Submit/Update commits between this check and the UPDATE
		// below) is intentionally left open here — the H2 backstop
		// in the UpdateQsoTx error branch translates the
		// UNIQUE-constraint violation into the same duplicate_key
		// outcome, so the race resolves correctly without holding a
		// pre-tx read lock that sqlite would have to serialize against
		// every concurrent writer.
		collision, err := s.DB.FetchQsoByDedupeKeyWithContext(ctx, merged.LogbookID, newKey)
		if err == nil && collision.ID != merged.ID {
			return types.Qso{}, &SubmitError{
				Code:    "duplicate_key",
				Message: "edit would collide with another QSO in this logbook",
			}
		}
		if err != nil && !stderr.Is(err, errors.ErrNotFound) {
			return types.Qso{}, errors.New(op).WithErr(err).WithMsg("dedupe collision check failed")
		}
		merged.DedupeKey = newKey
	}

	// ---- Atomic write: QSO update + update-action upload rows + qso_history ----
	// Symmetric with Submit: both ingest paths write under the same
	// one-fails-all-fail contract. A single transaction carries the QSO update,
	// the action.Update upload-queue rows for configured forwarders, and the
	// qso_history audit row.
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	if err = s.DB.UpdateQsoTx(ctx, tx, merged); err != nil {
		_ = tx.Rollback()

		// Race window symmetric with Submit (submit.go:202–224): the
		// pre-tx FetchQsoByDedupeKey at line 169 above can return
		// ErrNotFound while a concurrent Submit/Update is committing
		// the same dedupe key, then this UPDATE hits the UNIQUE index
		// on (logbook_id, dedupe_key) WHERE deleted_at IS NULL. From
		// the client's point of view this is a duplicate-key conflict,
		// not a daemon error — translate it so the handler maps to 409
		// the same way the pre-check path does.
		if sqlite.IsUniqueConstraintError(err) {
			return types.Qso{}, &SubmitError{
				Code:    "duplicate_key",
				Message: "edit would collide with another QSO in this logbook",
			}
		}
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to update QSO")
	}

	// Enqueue one qso_upload row per ENABLED forwarder whose action_filter
	// includes 'update' (ADR 0039: `enabled` gates enqueue). Same tx as the QSO
	// update per the one-fails-all-fail
	// invariant. If the destination's filter is ["insert"] (LoTW-style write-
	// once), no row is inserted for it and the edit simply doesn't propagate
	// there — which matches the operator's declared intent.
	for _, fwd := range s.Config.Forwarders() {
		if !shouldEnqueue(fwd, action.Update) {
			continue
		}
		if err = s.DB.InsertQsoUploadTx(ctx, tx, merged.ID, action.Update, fwd.Name, fwd.Type); err != nil {
			_ = tx.Rollback()
			return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	// Append the audit row inside the same tx (ADR 0016 prep #2). The
	// snapshot is the pre-edit state — json.Marshal(existing), not
	// json.Marshal(merged) — so replaying audit reconstructs each
	// state the row passed through.
	beforeImage, err := json.Marshal(existing)
	if err != nil {
		_ = tx.Rollback()
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to marshal pre-edit snapshot")
	}
	if err = s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, action.Update, src, beforeImage); err != nil {
		_ = tx.Rollback()
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to insert qso_history row")
	}

	if err = tx.Commit(); err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", merged.ID).
		Int64("logbook_id", merged.LogbookID).
		Str("call", merged.ContactedStation.Call).
		Str("qso_date", merged.QsoDetails.QsoDate).
		Str("time_on", merged.QsoDetails.TimeOn).
		Str("freq_mhz", merged.QsoDetails.Freq).
		Str("band", merged.QsoDetails.Band).
		Str("mode", merged.QsoDetails.Mode).
		Msg("QSO updated")

	s.Hub.Publish(events.NameQsoUpdated, events.QsoUpdatedPayload{
		QsoID:     merged.ID,
		LogbookID: merged.LogbookID,
	})

	return merged, nil
}
