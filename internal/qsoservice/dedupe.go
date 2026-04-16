package qsoservice

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// ComputeDedupeKey returns a SHA-256 hex hash of CALL|BAND|MODE|QSO_DATE|TIME_ON
// (uppercased, pipe-separated). TIME_ON is truncated to 4 characters (HHMM)
// to give minute-granularity deduplication per api.md Section 4.2.
func ComputeDedupeKey(call, band, mode, qsoDate, timeOn string) string {
	// Truncate timeOn to minute precision (HHMM).
	if len(timeOn) > 4 {
		timeOn = timeOn[:4]
	}

	input := strings.Join([]string{
		strings.ToUpper(call),
		strings.ToUpper(band),
		strings.ToUpper(mode),
		strings.ToUpper(qsoDate),
		strings.ToUpper(timeOn),
	}, "|")

	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}
