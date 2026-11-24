package sqlite

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// HasQsoForDxccWithContext matches on the numeric ADIF DXCC code stored in the
// QSO's additional_data blob (via json_extract), which is the key the
// new-entity check uses to avoid the country-name mismatch.
func TestHasQsoForDxcc(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("InsertLogbook: %v", err)
	}

	// A QSO carrying DXCC 230 (Germany). ContactedStation.DXCC serialises to
	// additional_data `$.dxcc` — the path the query reads.
	q := validTestQso(lbID, "DL1ABC", "20m", "FT8", "20260625", "1200")
	q.ContactedStation.Country = "Germany"
	q.ContactedStation.DXCC = "230"
	if _, err := svc.InsertQso(q); err != nil {
		t.Fatalf("InsertQso: %v", err)
	}

	t.Run("worked entity returns true", func(t *testing.T) {
		got, err := svc.HasQsoForDxccWithContext(ctx, "230")
		if err != nil {
			t.Fatalf("HasQsoForDxcc(230): %v", err)
		}
		if !got {
			t.Error("DXCC 230 is in the log, want true")
		}
	})

	t.Run("unworked entity returns false", func(t *testing.T) {
		got, err := svc.HasQsoForDxccWithContext(ctx, "15") // Asiatic Russia, not worked
		if err != nil {
			t.Fatalf("HasQsoForDxcc(15): %v", err)
		}
		if got {
			t.Error("DXCC 15 not in the log, want false")
		}
	})

	t.Run("empty code returns false without error", func(t *testing.T) {
		got, err := svc.HasQsoForDxccWithContext(ctx, "  ")
		if err != nil || got {
			t.Errorf("empty code = %v, %v; want false, nil", got, err)
		}
	})
}

// A contact stored with its country and NO DXCC number is one whose number was
// never known when it was logged (the entity catalogue once held only the
// entities already worked). The new-entity check asks for exactly these when
// the number finds nothing, so that such a contact still counts as its entity
// worked (the clean-room review of 1952d50a).
func TestHasQsoForCountryLackingDxcc(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("InsertLogbook: %v", err)
	}
	insert := func(call, date, country, dxcc string) int64 {
		t.Helper()
		q := validTestQso(lbID, call, "10m", "FT8", date, "1200")
		q.ContactedStation.Country = country
		q.ContactedStation.DXCC = dxcc
		id, err := svc.InsertQso(q)
		if err != nil {
			t.Fatalf("InsertQso %s: %v", call, err)
		}
		return id
	}
	insert("TN8XX", "20261031", "Republic of the Congo", "") // logged without its number
	insert("DL1ABC", "20261101", "Germany", "230")           // logged with it
	gone := insert("VK9XA", "20261102", "Christmas Island", "")
	execT(t, svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, gone)

	for name, c := range map[string]struct {
		country string
		want    bool
	}{
		"stored without a number":                {"Republic of the Congo", true},
		"stored WITH a number is not this case":  {"Germany", false},
		"a deleted contact does not count":       {"Christmas Island", false},
		"a country never logged":                 {"Norfolk Island", false},
		"an empty country matches nothing":       {"  ", false},
		"the name is matched whole, not in part": {"Republic", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := svc.HasQsoForCountryLackingDxccWithContext(ctx, c.country)
			if err != nil || got != c.want {
				t.Fatalf("%q = %v, %v; want %v", c.country, got, err, c.want)
			}
		})
	}
}
