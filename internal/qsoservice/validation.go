package qsoservice

import (
	"fmt"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// validateSubmodeMatchesMode rejects a SUBMODE that is KNOWN to belong to a
// different main mode: an inconsistent pair (MODE=CW, SUBMODE=USB) would
// otherwise be stored and forwarded to QRZ/ClubLog as contradictory ADIF. An
// UNKNOWN submode passes — the catalogue is operator-extendable, so an
// unlisted-but-valid ADIF submode must not block an import.
//
// Both create and edit call this, because the invariant is only as strong as its
// weaker path: a PATCH naming just MODE leaves the stored SUBMODE untouched and
// re-forms at edit time exactly the pair refused at creation. Keeping it in one
// function is what stops the two paths drifting apart again.
//
// Inputs should already be trimmed and uppercased (same contract as
// IsValidCallsign) — the lookup normalizes its own argument, but the parent it
// returns is canonical uppercase and is compared against mode directly.
func validateSubmodeMatchesMode(mode, submode string) error {
	if mode == "" || submode == "" {
		return nil
	}
	parent, ok := modes.GetModeBySubmode(submode)
	if !ok || parent.String() == mode {
		return nil
	}
	return &SubmitError{
		Code:    "invalid_field_value",
		Message: fmt.Sprintf("SUBMODE %q belongs to mode %q, not %q", submode, parent.String(), mode),
	}
}

// Callsign length bounds (inclusive). The error messages in submit.go /
// update.go quote "3-32" in prose; keep them in step with these if changed.
const (
	minCallsignLen = 3
	maxCallsignLen = 32
)

// Schema-mirroring length caps (review 2026-08-07 #3). These MUST match the
// qso table's CHECK constraints (migrations 0002_relax_rst_length /
// 0006_widen_mode_call: rst_sent/rst_rcvd ≤ 10, trimmed country ≤ 50): a
// value the CHECK would refuse has to fail VALIDATION instead, because a
// CHECK failure surfaces as a server fault on the live path and — worse — as
// a non-SubmitError in the batch import's per-record fallback, which aborts
// the whole remaining import for one overlong field.
const (
	maxRstLen     = 10
	maxCountryLen = 50
)

// validateSchemaLengths applies the caps above to the three free-text columns
// no earlier check bounds (every other CHECKed column is covered upstream:
// call/band/mode by their validators, dates/times by format checks, freq by
// the band table). Shared by prepareQso and Update so the paths cannot drift;
// adifNames selects the caller's field vocabulary for the message.
func validateSchemaLengths(rstSent, rstRcvd, country string, adifNames bool) error {
	name := func(adif, js string) string {
		if adifNames {
			return adif
		}
		return js
	}
	if len(rstSent) > maxRstLen {
		return &SubmitError{Code: "invalid_field_value",
			Message: fmt.Sprintf("%s must be at most %d characters", name("RST_SENT", "rst_sent"), maxRstLen)}
	}
	if len(rstRcvd) > maxRstLen {
		return &SubmitError{Code: "invalid_field_value",
			Message: fmt.Sprintf("%s must be at most %d characters", name("RST_RCVD", "rst_rcvd"), maxRstLen)}
	}
	if len(strings.TrimSpace(country)) > maxCountryLen {
		return &SubmitError{Code: "invalid_field_value",
			Message: fmt.Sprintf("%s must be at most %d characters", name("COUNTRY", "country"), maxCountryLen)}
	}
	return nil
}

// timeCmpADIF compares two validated ADIF times at their finest COMMON
// precision: both HHMMSS compare on full seconds; a mixed pair compares on
// the minute, because HHMM names the minute — not second :00 — so a mixed
// pair in the same minute is "the same time", never a negative interval
// (the preserveSeconds reading; refusing it would reject QRZ-style minute
// logs of seconds-bearing QSOs). Fixed-width digit strings order correctly
// under lexical comparison.
func timeCmpADIF(a, b string) int {
	if len(a) == 6 && len(b) == 6 {
		return strings.Compare(a, b)
	}
	return strings.Compare(utils.TimeToHHMM(a), utils.TimeToHHMM(b))
}

// validateTimeCoherence rejects impossible start/end pairs (review 2026-08-07
// #4). Direction one — the end earlier than the start on the clock — is only
// explained by the midnight wrap, so QSO_DATE_OFF must be exactly the
// following day (the long-standing rule, now seconds-aware via timeCmpADIF).
// Direction two — the end at or after the start — was previously unchecked
// entirely, which let QSO_DATE_OFF sit years from QSO_DATE: a present
// QSO_DATE_OFF must equal QSO_DATE, because a same-direction pair spanning a
// calendar day would describe a ≥24-hour QSO. One function for both ingest
// paths so they cannot drift; adifNames selects the caller's field vocabulary
// (ADIF tags for submit/import, json keys for PATCH).
//
// Inputs are already validated (dates YYYYMMDD, times HHMM/HHMMSS), so the
// date parses cannot fail.
func validateTimeCoherence(timeOn, timeOff, qsoDate, qsoDateOff string, adifNames bool) error {
	name := func(adif, js string) string {
		if adifNames {
			return adif
		}
		return js
	}
	if timeCmpADIF(timeOn, timeOff) > 0 {
		if qsoDateOff == "" || qsoDateOff == qsoDate {
			return &SubmitError{
				Code: "invalid_time_range",
				Message: fmt.Sprintf("%s is after %s without a %s on the following day",
					name("TIME_ON", "time_on"), name("TIME_OFF", "time_off"), name("QSO_DATE_OFF", "qso_date_off")),
			}
		}
		onDate, _ := time.Parse("20060102", qsoDate)
		offDate, _ := time.Parse("20060102", qsoDateOff)
		if !offDate.Equal(onDate.AddDate(0, 0, 1)) {
			return &SubmitError{
				Code: "invalid_time_range",
				Message: fmt.Sprintf("%s (%s) must be the day after %s (%s) when %s is after %s",
					name("QSO_DATE_OFF", "qso_date_off"), qsoDateOff, name("QSO_DATE", "qso_date"), qsoDate,
					name("TIME_ON", "time_on"), name("TIME_OFF", "time_off")),
			}
		}
		return nil
	}
	if qsoDateOff != "" && qsoDateOff != qsoDate {
		return &SubmitError{
			Code: "invalid_time_range",
			Message: fmt.Sprintf("%s (%s) must equal %s (%s) when %s is not after %s",
				name("QSO_DATE_OFF", "qso_date_off"), qsoDateOff, name("QSO_DATE", "qso_date"), qsoDate,
				name("TIME_ON", "time_on"), name("TIME_OFF", "time_off")),
		}
	}
	return nil
}

// IsValidCallsign checks that a callsign:
//   - is 3 to 32 characters long;
//   - contains only ASCII letters, ASCII digits, and the recognised
//     separators '/' and '-' (slash for portable/mobile/maritime
//     suffixes like G4ABC/P, dash occasionally seen in special-event
//     calls);
//   - contains at least one ASCII digit.
//
// The character-set restriction (review m6) defends downstream LIKE-
// based queries that interpolate the validated callsign without their
// own ESCAPE clause — notably FetchQsoSliceByCallsignWithContext, which
// builds `<callsign>/%` to match suffix-decorated variants. A typo'd
// callsign containing '%' or '_' would otherwise silently over-match.
//
// The input should already be trimmed and uppercased.
func IsValidCallsign(callsign string) bool {
	if len(callsign) < minCallsignLen || len(callsign) > maxCallsignLen {
		return false
	}
	hasDigit := false
	for i := 0; i < len(callsign); i++ {
		c := callsign[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '/' || c == '-':
		default:
			return false
		}
	}
	return hasDigit
}
