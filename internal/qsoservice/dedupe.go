package qsoservice

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ComputeDedupeKey returns a SHA-256 hex hash of
// CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON (uppercased, pipe-separated). TIME_ON
// is truncated to 4 characters (HHMM) to give minute-granularity
// deduplication per api.md Section 4.2. FREQ is the normalized integer-kHz
// string (Submit produces this via FreqMHzToKHzString) so the input is
// deterministic across "14.074" / "14074" / "14.0740" encodings of the same
// frequency.
//
// Including FREQ distinguishes same-station/same-band/same-mode/same-minute
// contacts that happen on different frequencies (net ops, split, frequency
// hopping) per the ADIF spec's guidance on QSO identity.
//
// All six fields must be non-empty. Panics if any field is empty — this is
// a programming error (Submit validates before calling).
func ComputeDedupeKey(call, band, mode, freq, qsoDate, timeOn string) string {
	if call == "" || band == "" || mode == "" || freq == "" || qsoDate == "" || timeOn == "" {
		panic("ComputeDedupeKey: all fields must be non-empty")
	}

	// Truncate timeOn to minute precision (HHMM).
	if len(timeOn) > 4 {
		timeOn = timeOn[:4]
	}

	input := strings.Join([]string{
		strings.ToUpper(call),
		strings.ToUpper(band),
		strings.ToUpper(mode),
		strings.ToUpper(freq),
		strings.ToUpper(qsoDate),
		strings.ToUpper(timeOn),
	}, "|")

	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}
