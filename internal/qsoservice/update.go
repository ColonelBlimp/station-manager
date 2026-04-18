package qsoservice

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

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
func (s *Service) Update(ctx context.Context, existing types.Qso, body []byte) (types.Qso, error) {
	const op errors.Op = "qsoservice.Update"

	merged := existing
	if err := json.Unmarshal(body, &merged); err != nil {
		return types.Qso{}, &SubmitError{Code: "invalid_json", Message: "failed to parse request body"}
	}

	// ---- Restore immutables ----
	// Structural identity — set at creation, never editable.
	merged.ID = existing.ID
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
	merged.QsoDetails.TimeOn = utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOn))
	merged.QsoDetails.TimeOff = utils.SanitizeTimeToADIF(strings.TrimSpace(merged.QsoDetails.TimeOff))
	merged.QsoDetails.RstSent = strings.TrimSpace(merged.QsoDetails.RstSent)
	merged.QsoDetails.RstRcvd = strings.TrimSpace(merged.QsoDetails.RstRcvd)
	merged.ContactedStation.Country = strings.TrimSpace(merged.ContactedStation.Country)
	if freq := strings.TrimSpace(merged.QsoDetails.Freq); freq != "" {
		kHz, err := utils.ParseFreqMHz(freq)
		if err != nil {
			return types.Qso{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("freq %q: %v", freq, err)}
		}
		merged.QsoDetails.Freq = utils.FormatFreqMHz(kHz)
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
	if merged.QsoDetails.RstSent == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_sent cannot be empty"}
	}
	if merged.QsoDetails.RstRcvd == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "rst_rcvd cannot be empty"}
	}
	if merged.ContactedStation.Country == "" {
		return types.Qso{}, &SubmitError{Code: "missing_required_field", Message: "country cannot be empty"}
	}

	// ---- Time coherence ----
	if merged.QsoDetails.TimeOn > merged.QsoDetails.TimeOff {
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
	dedupeChanged := merged.ContactedStation.Call != existing.ContactedStation.Call ||
		merged.QsoDetails.Band != existing.QsoDetails.Band ||
		merged.QsoDetails.Mode != existing.QsoDetails.Mode ||
		merged.QsoDetails.Freq != existing.QsoDetails.Freq ||
		merged.QsoDetails.QsoDate != existing.QsoDetails.QsoDate ||
		merged.QsoDetails.TimeOn != existing.QsoDetails.TimeOn

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
			merged.QsoDetails.TimeOn,
		)
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

	// ---- Atomic write: QSO + (future) upload-queue rows ----
	// Symmetric with Submit: both ingest paths write under the same
	// one-fails-all-fail contract. No upload-queue rows are produced on
	// edit today (the forwarder lands in a later milestone), but when
	// they are, the Insert loop will slot in here alongside the update
	// inside the existing transaction envelope — no shape change needed.
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	if err = s.DB.UpdateQsoTx(ctx, tx, merged); err != nil {
		_ = tx.Rollback()
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to update QSO")
	}

	// Future forwarder hook: for _, svc := range configuredUploadServices() {
	//     if err = s.DB.InsertQsoUploadTx(ctx, tx, merged.ID, action.Update, svc); err != nil {
	//         _ = tx.Rollback()
	//         return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
	//     }
	// }

	if err = tx.Commit(); err != nil {
		return types.Qso{}, errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", merged.ID).
		Int64("logbook_id", merged.LogbookID).
		Str("call", merged.ContactedStation.Call).
		Str("freq_mhz", merged.QsoDetails.Freq).
		Str("band", merged.QsoDetails.Band).
		Str("mode", merged.QsoDetails.Mode).
		Msg("QSO updated")

	return merged, nil
}
