package qsoservice

import (
	"context"
	"crypto/sha256"
	stderr "errors"
	"fmt"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"strconv"
	"strings"
)

// dedupeKeyBytes is the raw byte length of a dedupe key — a SHA-256 digest,
// so 32 bytes / 64 hex chars. The force-mode random nonce uses the same width
// so its hex form is indistinguishable in shape from a real key.
const dedupeKeyBytes = sha256.Size

// timeOnKeyLen is the minute-granularity prefix (HHMM) of TIME_ON used in the
// key, per api.md §4.2 — seconds are dropped so same-minute logs dedupe.
const timeOnKeyLen = 4

// ComputeDedupeKey returns a SHA-256 hex hash of
// CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON (uppercased, pipe-separated). TIME_ON
// is truncated to 4 characters (HHMM) to give minute-granularity
// deduplication per api.md Section 4.2. FREQ is the normalized integer-kHz
// string (callers parse MHz input via utils.ParseFreqMHz, then pass the
// int-kHz as a string), so the input is deterministic across
// "14.074" / "14074" / "14.0740" encodings of the same frequency.
//
// Including FREQ distinguishes same-station/same-band/same-mode/same-minute
// contacts that happen on different frequencies (net ops, split, frequency
// hopping) per the ADIF spec's guidance on QSO identity.
//
// All six fields must be non-empty. Panics if any field is empty — this is
// a programming error (Submit validates before calling).
// legacyDedupeKey is the key a prepared record would have hashed to under the
// bare name of its submode, when that submode is one the catalogue listed as a
// main mode until 2026-09-11 (FT4, FST4, FST4W, JS8, Q65): a row stored as
// MODE=FT4 back then carries that key until an edit recomputes it. Derived
// from the CANONICAL pair, never from what was submitted — the bulk import's
// CLI has already converted its records to the pair before the service sees
// them, and a key that needed the bare form would miss exactly that path
// (operator review 2026-09-11). Empty for every other pair.
func legacyDedupeKey(qso types.Qso) string {
	sub := qso.QsoDetails.Submode
	if !modes.WasListedAsMainMode(sub) {
		return ""
	}
	if parent, ok := modes.GetModeBySubmode(sub); !ok || parent.String() != qso.QsoDetails.Mode {
		return ""
	}
	kHz, err := utils.ParseFreqMHz(qso.QsoDetails.Freq) // canonical MHz: cannot fail
	if err != nil {
		return ""
	}
	return ComputeDedupeKey(qso.ContactedStation.Call, qso.QsoDetails.Band, sub,
		strconv.FormatInt(kHz, 10), qso.QsoDetails.QsoDate, utils.TimeToHHMM(qso.QsoDetails.TimeOn))
}

// dedupeKeysFor is every key under which a prepared record may already be
// stored: its own, then the legacy one when it has one. The single submit and
// the bulk import both look up all of them — the one place that decides what
// "already stored" means, so the two paths cannot drift (codex 40238d47 P2).
func dedupeKeysFor(qso types.Qso) []string {
	keys := []string{qso.DedupeKey}
	if legacy := legacyDedupeKey(qso); legacy != "" {
		keys = append(keys, legacy)
	}
	return keys
}

// findStored looks a prepared record up under each of its dedupe keys in turn.
// found is false with a nil error when no key matches; any other lookup error
// is the caller's to classify (an infrastructure fault, not a duplicate).
func (s *Service) findStored(ctx context.Context, logbookID int64, qso types.Qso) (existing types.Qso, found bool, err error) {
	for _, key := range dedupeKeysFor(qso) {
		existing, err = s.DB.FetchQsoByDedupeKeyWithContext(ctx, logbookID, key)
		if err == nil {
			return existing, true, nil
		}
		if !stderr.Is(err, errors.ErrNotFound) {
			return types.Qso{}, false, err
		}
	}
	return types.Qso{}, false, nil
}

func ComputeDedupeKey(call, band, mode, freq, qsoDate, timeOn string) string {
	if call == "" || band == "" || mode == "" || freq == "" || qsoDate == "" || timeOn == "" {
		panic("ComputeDedupeKey: all fields must be non-empty")
	}

	// Truncate timeOn to minute precision (HHMM).
	if len(timeOn) > timeOnKeyLen {
		timeOn = timeOn[:timeOnKeyLen]
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
