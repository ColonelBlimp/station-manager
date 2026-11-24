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

// Has this entity been worked? One question, asked in ONE statement: a contact
// carrying the entity's number, or a contact with its country and NO number.
//
// The second half is for a contact whose number was never known when it was
// logged (the entity catalogue once held only the entities already worked): it
// cannot be found by number. Where a number was stored, the number rules.
//
// One statement, because it was two (the clean-room review of fbab155f): asked
// one after the other, an edit that gave such a contact its number between them
// was missed by both, and the entity read as new with the contact in the log
// before and after.
func TestHasQsoForEntity(t *testing.T) {
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
	insert("EA8XX", "20261103", "Spain", "29")               // its number says Canary Islands
	gone := insert("VK9XA", "20261102", "Christmas Island", "")
	execT(t, svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, gone)

	for name, c := range map[string]struct {
		dxcc, country string
		want          bool
	}{
		"by its number":                                       {"230", "Fed. Rep. of Germany", true},
		"by its number, whatever the name":                    {"230", "", true},
		"by its country, stored without a number":             {"412", "Republic of the Congo", true},
		"a stored number rules over a matching name":          {"281", "Spain", false},
		"the stored number finds it under another name":       {"29", "Canary Islands", true},
		"a deleted contact does not count":                    {"35", "Christmas Island", false},
		"never logged":                                        {"189", "Norfolk Island", false},
		"the name is matched whole, not in part":              {"412", "Republic", false},
		"no number and no country matches nothing":            {"  ", "  ", false},
		"no number asked for: the country half still answers": {"", "Republic of the Congo", true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := svc.HasQsoForEntityWithContext(ctx, c.dxcc, c.country)
			if err != nil || got != c.want {
				t.Fatalf("(%q, %q) = %v, %v; want %v", c.dxcc, c.country, got, err, c.want)
			}
		})
	}
}
