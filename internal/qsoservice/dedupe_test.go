package qsoservice

import (
	"testing"
)

func TestComputeDedupeKey_Deterministic(t *testing.T) {
	k1 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	k2 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	if k1 != k2 {
		t.Fatalf("same inputs produced different keys: %q vs %q", k1, k2)
	}
}

func TestComputeDedupeKey_Length(t *testing.T) {
	k := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	if len(k) != 64 {
		t.Fatalf("key length = %d, want 64 (SHA-256 hex)", len(k))
	}
}

func TestComputeDedupeKey_CaseInsensitive(t *testing.T) {
	k1 := ComputeDedupeKey("m0cmc", "40M", "ssb", "20250508", "0845")
	k2 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	if k1 != k2 {
		t.Fatalf("case difference produced different keys: %q vs %q", k1, k2)
	}
}

func TestComputeDedupeKey_DifferentInputsDifferentKeys(t *testing.T) {
	k1 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	k2 := ComputeDedupeKey("DL1ABC", "40m", "SSB", "20250508", "0845")
	if k1 == k2 {
		t.Fatal("different calls produced the same key")
	}

	k3 := ComputeDedupeKey("M0CMC", "20m", "SSB", "20250508", "0845")
	if k1 == k3 {
		t.Fatal("different bands produced the same key")
	}

	k4 := ComputeDedupeKey("M0CMC", "40m", "CW", "20250508", "0845")
	if k1 == k4 {
		t.Fatal("different modes produced the same key")
	}

	k5 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250509", "0845")
	if k1 == k5 {
		t.Fatal("different dates produced the same key")
	}

	k6 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0900")
	if k1 == k6 {
		t.Fatal("different times produced the same key")
	}
}

func TestComputeDedupeKey_PanicsOnEmptyField(t *testing.T) {
	cases := []struct {
		name                              string
		call, band, mode, qsoDate, timeOn string
	}{
		{"empty call", "", "40m", "SSB", "20250508", "0845"},
		{"empty band", "M0CMC", "", "SSB", "20250508", "0845"},
		{"empty mode", "M0CMC", "40m", "", "20250508", "0845"},
		{"empty date", "M0CMC", "40m", "SSB", "", "0845"},
		{"empty time", "M0CMC", "40m", "SSB", "20250508", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic for empty field, got none")
				}
			}()
			ComputeDedupeKey(tc.call, tc.band, tc.mode, tc.qsoDate, tc.timeOn)
		})
	}
}

func TestComputeDedupeKey_TimeOnTruncatedToMinute(t *testing.T) {
	// HHMMSS should be truncated to HHMM for minute-granularity dedupe.
	k1 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "084500")
	k2 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "084530")
	if k1 != k2 {
		t.Fatalf("different seconds produced different keys: %q vs %q", k1, k2)
	}

	k3 := ComputeDedupeKey("M0CMC", "40m", "SSB", "20250508", "0845")
	if k1 != k3 {
		t.Fatalf("HHMMSS vs HHMM produced different keys: %q vs %q", k1, k3)
	}
}
