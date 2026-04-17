package qsoservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderr "errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/bands"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Submit validates an ADIF record, checks for duplicates, and atomically
// stores the QSO and its upload-queue rows.
//
// If force is true, the dedupe check is skipped (contest edge case per
// api.md Section 4.2).
func (s *Service) Submit(ctx context.Context, logbookID int64, rec adif.Record, force bool) (SubmitResult, error) {
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
	// ADIF freq is MHz as a string (e.g. "14.074"). The sqlite schema stores
	// freq as integer kHz. The adapter expects an integer string in the Freq
	// field. Convert MHz → kHz integer string.
	freqStr := strings.TrimSpace(rec.Freq)
	if freqStr == "" {
		return SubmitResult{}, &SubmitError{Code: "missing_required_field", Message: "FREQ is required"}
	}
	freqKHz, err := freqMHzToKHzString(freqStr)
	if err != nil {
		return SubmitResult{}, &SubmitError{Code: "invalid_field_value", Message: fmt.Sprintf("FREQ %q: %v", freqStr, err)}
	}

	// ---- Build the Qso ----
	qso := adif.RecordToQso(rec, logbookID)
	qso.ContactedStation.Call = call
	qso.QsoDetails.Band = band
	qso.QsoDetails.Mode = mode
	qso.QsoDetails.QsoDate = qsoDate
	qso.QsoDetails.TimeOn = timeOn
	qso.QsoDetails.TimeOff = timeOff
	qso.QsoDetails.QsoDateOff = qsoDateOff
	qso.QsoDetails.Freq = freqKHz
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
	dedupeKey := ComputeDedupeKey(call, band, mode, qsoDate, timeOn)

	if force {
		// Force mode: generate a unique dedupe key so the UNIQUE index
		// doesn't block the insert. The key is still a valid 64-char hex
		// string but is not reproducible — this QSO won't be deduplicated.
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("generating force nonce")
		}
		dedupeKey = hex.EncodeToString(nonce)
	} else {
		existing, err := s.DB.FetchQsoByDedupeKeyWithContext(ctx, logbookID, dedupeKey)
		if err == nil {
			return SubmitResult{Status: "duplicate", ID: existing.ID}, nil
		}
		if !stderr.Is(err, errors.ErrNotFound) {
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("dedupe check failed")
		}
	}

	qso.DedupeKey = dedupeKey

	// ---- Atomic write: QSO + upload-queue rows ----
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	qsoID, err := s.DB.InsertQsoTx(ctx, tx, qso)
	if err != nil {
		_ = tx.Rollback()
		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to insert QSO")
	}

	// Insert upload-queue rows for each configured upload service.
	// For milestone 1 there are no configured services; the forwarder
	// lands in milestone 1c. When it does, this loop populates the queue.
	for _, svc := range configuredUploadServices() {
		if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, svc); err != nil {
			_ = tx.Rollback()
			return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	if err = tx.Commit(); err != nil {
		return SubmitResult{}, errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", qsoID).
		Int64("logbook_id", logbookID).
		Str("call", call).
		Str("band", band).
		Str("mode", mode).
		Msg("QSO stored")

	return SubmitResult{Status: "stored", ID: qsoID}, nil
}

// configuredUploadServices returns the list of upload services for which
// queue rows should be created on QSO insert. Empty for milestone 1.
func configuredUploadServices() []upload.OnlineService {
	return nil
}

// freqMHzToKHzString converts a frequency string from MHz (e.g. "14.074",
// "7.050") to an integer kHz string (e.g. "14074", "7050"). Also accepts
// plain integer strings that are already in kHz.
func freqMHzToKHzString(s string) (string, error) {
	// If it contains a dot, treat as MHz float.
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return "", fmt.Errorf("cannot parse as MHz: %w", err)
		}
		kHz := int64(math.Round(f * 1000))
		return strconv.FormatInt(kHz, 10), nil
	}
	// No dot — assume already integer and vrify it parses.
	if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		return "", fmt.Errorf("cannot parse as integer frequency: %w", err)
	}

	return s, nil
}
