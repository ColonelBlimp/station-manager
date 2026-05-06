package utils

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestNewUUIDv7_format(t *testing.T) {
	id := NewUUIDv7()
	if len(id) != 36 {
		t.Fatalf("expected 36-char string, got %d: %q", len(id), id)
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("hyphens not in expected positions: %q", id)
	}
	if id[14] != '7' {
		t.Fatalf("version nibble should be '7', got %q in %q", id[14], id)
	}
	if !strings.ContainsAny(string(id[19]), "89ab") {
		t.Fatalf("variant high nibble should be 8/9/a/b, got %q in %q", id[19], id)
	}
}

func TestNewUUIDv7_versionAndVariantBits(t *testing.T) {
	for i := 0; i < 64; i++ {
		id := NewUUIDv7()
		stripped := strings.ReplaceAll(id, "-", "")
		raw, err := hex.DecodeString(stripped)
		if err != nil {
			t.Fatalf("hex decode failed for %q: %v", id, err)
		}
		if (raw[6] & 0xF0) != 0x70 {
			t.Fatalf("version nibble != 7 in %q (byte 6 = %02x)", id, raw[6])
		}
		if (raw[8] & 0xC0) != 0x80 {
			t.Fatalf("variant bits != 10 in %q (byte 8 = %02x)", id, raw[8])
		}
	}
}

func TestNewUUIDv7At_timestampPrefix(t *testing.T) {
	at := time.Date(2025, 6, 15, 12, 30, 45, 123_000_000, time.UTC)
	expectedMs := at.UnixMilli()

	id := NewUUIDv7At(at)
	stripped := strings.ReplaceAll(id, "-", "")
	raw, err := hex.DecodeString(stripped)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}

	gotMs := int64(raw[0])<<40 |
		int64(raw[1])<<32 |
		int64(raw[2])<<24 |
		int64(raw[3])<<16 |
		int64(raw[4])<<8 |
		int64(raw[5])

	if gotMs != expectedMs {
		t.Fatalf("timestamp prefix mismatch: got %d, want %d", gotMs, expectedMs)
	}
}

func TestNewUUIDv7At_orderingPreservesTimestamp(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t1.Add(time.Hour)

	a := NewUUIDv7At(t0)
	b := NewUUIDv7At(t1)
	c := NewUUIDv7At(t2)

	if !(a < b && b < c) {
		t.Fatalf("expected lexicographic ordering by timestamp, got: %s, %s, %s", a, b, c)
	}
}

func TestNewUUIDv7_uniqueness(t *testing.T) {
	const n = 1024
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewUUIDv7()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate UUID at iteration %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewUUIDv7At_negativeTimeCoercedToZero(t *testing.T) {
	id := NewUUIDv7At(time.Unix(-100, 0))
	stripped := strings.ReplaceAll(id, "-", "")
	raw, _ := hex.DecodeString(stripped)
	for i := 0; i < 6; i++ {
		if raw[i] != 0 {
			t.Fatalf("expected zero timestamp prefix for pre-epoch input, got byte %d = %02x in %q", i, raw[i], id)
		}
	}
}

func TestIsValidUUIDv7(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"freshly generated", NewUUIDv7(), true},
		{"empty", "", false},
		{"too short", "deadbeef", false},
		{"missing hyphens", strings.ReplaceAll(NewUUIDv7(), "-", ""), false},
		{"version 4 instead of 7", "01234567-89ab-4def-8123-456789abcdef", false},
		{"bad variant bits", "01234567-89ab-7def-0123-456789abcdef", false},
		{"non-hex chars", "0123456g-89ab-7def-8123-456789abcdef", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidUUIDv7(tt.in)
			if got != tt.want {
				t.Errorf("IsValidUUIDv7(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
