package sqlite

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The stamp date is the CALLER's date, not a second clock reading (codex
// 2026-08-08 P3): the api handler reports a date to the SPA and this method
// stores one, and when they were computed independently a send crossing UTC
// midnight stored the new day while the response carried the old one — the
// SPA's optimistic "Emailed" column then disagreed with the database until a
// reload. One clock reading, passed in, makes the agreement structural. The
// fixed past date is the differentiator: an implementation that consults its
// own clock stores today and fails here.
func TestMarkSessionEmailed_StoresTheCallersDate(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qso := validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845")
	id, err := svc.InsertQso(qso)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}

	n, err := svc.MarkSessionEmailedWithContext(context.Background(), []int64{id}, "19990101")
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if n != 1 {
		t.Fatalf("stamped %d rows, want 1", n)
	}

	stored, err := svc.FetchQsoByUUIDWithContext(context.Background(), qso.UUID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if stored.SmFwrdByEmailDate != "19990101" {
		t.Errorf("stored date = %q, want the caller's 19990101 — a second clock reading can disagree with the reported date", stored.SmFwrdByEmailDate)
	}
}
