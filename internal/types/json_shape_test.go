package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The LastRefreshedAt fields are `json:"-"` cache metadata (review 2026-06-19
// M1): they must never reach the marshalled JSON — not in the durable
// additional_data blob, not in country_details, and not on the API wire. A
// zero time.Time would otherwise serialize as "0001-01-01T00:00:00Z", noise
// that contradicts ADR 0015's operator-set/enriched-only blob contract. These
// tests pin the absence for both the zero and the populated case.

func TestContactedStation_LastRefreshedAtNeverMarshaled(t *testing.T) {
	// Zero value: no key, no zero-time string.
	zero, err := json.Marshal(ContactedStation{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(zero), "last_refreshed_at") {
		t.Errorf("zero ContactedStation JSON contains last_refreshed_at: %s", zero)
	}
	if strings.Contains(string(zero), "0001-01-01") {
		t.Errorf("zero ContactedStation JSON contains a zero timestamp: %s", zero)
	}

	// Populated value: still absent — `json:"-"` is unconditional, the column
	// is the authoritative store.
	set, err := json.Marshal(ContactedStation{
		Call:            "DL1ABC",
		LastRefreshedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(set), "last_refreshed_at") {
		t.Errorf("populated ContactedStation JSON leaks last_refreshed_at: %s", set)
	}
}

func TestCountry_LastRefreshedAtNeverMarshaled(t *testing.T) {
	zero, err := json.Marshal(Country{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(zero), "last_refreshed_at") {
		t.Errorf("zero Country JSON contains last_refreshed_at: %s", zero)
	}
	if strings.Contains(string(zero), "0001-01-01") {
		t.Errorf("zero Country JSON contains a zero timestamp: %s", zero)
	}

	set, err := json.Marshal(Country{
		Name:            "Germany",
		LastRefreshedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(set), "last_refreshed_at") {
		t.Errorf("populated Country JSON leaks last_refreshed_at: %s", set)
	}
}

// A minimal Qso (as written into the additional_data blob via json.Marshal of
// the whole struct) must carry neither the embedded ContactedStation's
// last_refreshed_at nor a zero timestamp inside country_details.
func TestQso_BlobShapeHasNoZeroRefreshTimestamp(t *testing.T) {
	blob, err := json.Marshal(Qso{
		LogbookID:        1,
		ContactedStation: ContactedStation{Call: "DL1ABC"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), "last_refreshed_at") {
		t.Errorf("Qso blob leaks last_refreshed_at: %s", blob)
	}
	if strings.Contains(string(blob), "0001-01-01") {
		t.Errorf("Qso blob contains a zero timestamp: %s", blob)
	}
}
