package utils

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrFrequencyTooShort = errors.New("frequency string too short (minimum 9 characters)")
	ErrFrequencyTooLong  = errors.New("frequency string too long (maximum 10 characters)")
	ErrFrequencySyntax   = errors.New("invalid frequency string (must have 2 periods)")
	ErrFrequencyInvalid  = errors.New("invalid frequency string (must have 9 characters)")
)

var reFrequencyMHz = regexp.MustCompile(`^\d{7,8}$`)

// bandRange is one amateur band's MHz allocation. The table is range-based
// (low/high), NOT prefix-based: an earlier prefix scheme (e.g. "14." → 20m)
// classified in-prefix-out-of-band values as valid — 14.999 → 20m even though
// 20m ends at 14.350 — so values between bands or past a band edge were
// mislabelled (review 2026-06-19 M1). Mirrors the app SPA's table
// (frontend/app/src/lib/utils/frequency.ts) so daemon and SPA agree on
// what counts as in-band. Ranges follow the ADIF 3.1.5 band enumeration — notably
// 60m = 5.06–5.45 MHz and 4m = 70–71 MHz — so band derivation accepts every in-band
// ADIF frequency; a truly out-of-band freq (empty result) is rejected at submit,
// which relies on this table being no narrower than ADIF (2026-07-21 review #2).
type bandRange struct {
	low  float64 // MHz, inclusive
	high float64 // MHz, inclusive
	name string  // ADIF BAND literal
}

var bandRanges = []bandRange{
	{1.800, 2.000, "160m"},
	{3.500, 4.000, "80m"},
	{5.060, 5.450, "60m"},
	{7.000, 7.300, "40m"},
	{10.100, 10.150, "30m"},
	{14.000, 14.350, "20m"},
	{18.068, 18.168, "17m"},
	{21.000, 21.450, "15m"},
	{24.890, 24.990, "12m"},
	{28.000, 29.700, "10m"},
	{50.000, 54.000, "6m"},
	{70.000, 71.000, "4m"},
	{144.000, 148.000, "2m"},
	{222.000, 225.000, "1.25m"},
	{420.000, 450.000, "70cm"},
	{902.000, 928.000, "33cm"},
	{1240.000, 1300.000, "23cm"},
}

// bandRangeFor parses a decimal-MHz frequency string and returns the band whose
// allocation contains it, or nil when the value is unparseable or out of band.
func bandRangeFor(freq string) *bandRange {
	mhz, err := strconv.ParseFloat(strings.TrimSpace(freq), 64)
	if err != nil {
		return nil
	}
	for i := range bandRanges {
		if mhz >= bandRanges[i].low && mhz <= bandRanges[i].high {
			return &bandRanges[i]
		}
	}
	return nil
}

// FormatFrequencyToKhz converts a 9-character raw frequency string into a formatted frequency string in kHz format.
// Returns an error if the input string length is invalid.
func FormatFrequencyToKhz(rawFreq string) (string, error) {
	if len(rawFreq) != 9 {
		return "0.000.000", ErrFrequencyInvalid
	}
	mhz := strings.TrimLeft(rawFreq[1:3], "0")
	khz := rawFreq[3:6]
	hz := rawFreq[6:]
	return mhz + dotString + khz + dotString + hz, nil
}

// GetFrequencyRange returns the min and max MHz of the band allocation
// containing freq (a decimal-MHz string), or 0, 0 when freq is out of band or
// unparseable. Range-checked against the actual allocations, not a MHz prefix.
func GetFrequencyRange(freq string) (float64, float64) {
	if br := bandRangeFor(freq); br != nil {
		return br.low, br.high
	}
	return 0, 0
}

// FrequencyToBand returns the ADIF band literal for freq (a decimal-MHz
// string), or an empty string when freq falls between bands, past a band edge,
// or cannot be parsed. Callers (FT8 dial validation, QSO band derivation) rely
// on the empty string to mean "not a valid in-band dial frequency".
func FrequencyToBand(freq string) string {
	if br := bandRangeFor(freq); br != nil {
		return br.name
	}
	return emptyString
}

// FormatFrequencyToMhz formats a raw frequency string (e.g., "014.074.000" or "14.074") into MHz format "14.074".
// It is lenient about length and focuses on dot-separated parts; returns an error if structure is clearly invalid.
func FormatFrequencyToMhz(rawFreq string) (string, error) {
	if rawFreq == emptyString {
		return emptyString, nil
	}
	parts := strings.Split(rawFreq, dotString)
	switch len(parts) {
	case 1:
		// No dot present, cannot infer MHz with decimals reliably
		return emptyString, ErrFrequencySyntax
	case 2:
		// Already MHz with decimals (e.g., "14.074" or "144.390")
		mhz := strings.TrimLeft(parts[0], "0")
		return mhz + dotString + parts[1], nil
	default:
		// Three or more parts: drop the last part (Hz) and keep MHz.KHz
		mhz := strings.TrimLeft(parts[0], "0")
		return mhz + dotString + parts[1], nil
	}
}

// ParseFreqMHz parses a frequency string and returns the equivalent integer
// kHz value — the unit the sqlite schema stores. Accepts either decimal MHz
// ("14.074", the ADIF-native form) or plain integer kHz ("14074"). The
// tolerance for bare integers is there for legacy clients and tools that
// already speak kHz; ADIF-compliant callers send MHz.
//
// A non-positive result (zero or negative) is rejected: FREQ is a required QSO
// field and no caller uses zero as a sentinel, so letting 0/negative through
// would either store meaningless data (the sqlite freq check allows >= 0) or
// surface as a late insert/update failure instead of a clean 400. Catching it
// here means every caller's existing error path turns it into invalid_field_value
// (review 2026-06-19 M3).
func ParseFreqMHz(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == emptyString {
		return 0, fmt.Errorf("empty frequency")
	}
	var kHz int64
	if strings.Contains(s, dotString) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse as MHz: %w", err)
		}
		kHz = int64(math.Round(f * 1000))
	} else {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse as integer frequency: %w", err)
		}
		kHz = n
	}
	if kHz <= 0 {
		return 0, fmt.Errorf("frequency must be positive, got %q", s)
	}
	return kHz, nil
}

// FormatFreqMHz formats an integer kHz value as a canonical decimal MHz
// string with exactly three decimal places (the DB's kHz granularity):
// 14074 → "14.074", 7050 → "7.050", 7000 → "7.000". Always three decimals
// so round-tripping is stable and the canonical form is unambiguous.
func FormatFreqMHz(kHz int64) string {
	return fmt.Sprintf("%d.%03d", kHz/1000, kHz%1000)
}

func IsValidFrequencyMHz(s string) bool {
	s = strings.TrimSpace(s)
	if s == emptyString {
		return false
	}
	if !reFrequencyMHz.MatchString(s) {
		return false
	}
	f, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return false
	}
	return f > 0
}
