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
