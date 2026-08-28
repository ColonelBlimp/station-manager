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

	// A fresh insert is at revision 0, which the revision-guarded stamp matches.
	stamped, err := svc.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: id, Revision: 0}}, "19990101")
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if len(stamped) != 1 || stamped[0] != id {
		t.Fatalf("stamped = %v, want [%d]", stamped, id)
	}

	stored, err := svc.FetchQsoByUUIDWithContext(context.Background(), qso.UUID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if stored.SmFwrdByEmailDate != "19990101" {
		t.Errorf("stored date = %q, want the caller's 19990101 — a second clock reading can disagree with the reported date", stored.SmFwrdByEmailDate)
	}
}

// The session-email handler caps a request at 10,000 UUIDs, so the stamp must match
// that many (id, revision) targets in ONE statement. A generated OR-chain would blow
// SQLITE_MAX_EXPR_DEPTH (1000) and a multi-row VALUES risks SQLITE_MAX_COMPOUND_SELECT
// (500); the json_each relation must carry the whole set and still return exactly the
// rows whose id AND revision match.
func TestMarkSessionEmailedAtRevision_ScalesToTenThousandTargets(t *testing.T) {
	svc := testService(t)
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	insert := func(call string) int64 {
		id, ierr := svc.InsertQso(validTestQso(lbID, call, "40m", "SSB", "20250508", "0845"))
		if ierr != nil {
			t.Fatalf("insert %s: %v", call, ierr)
		}
		return id
	}
	idA := insert("M0AAA")
	idB := insert("M0BBB")
	idC := insert("M0CCC") // revision 0

	// 10,000 targets: A and B at their real revision (match), C at a WRONG revision
	// (must not match — the PT-3 guard at scale), and the rest non-existent ids.
	const total = 10000
	targets := []SessionEmailTarget{
		{ID: idA, Revision: 0},
		{ID: idB, Revision: 0},
		{ID: idC, Revision: 7}, // C is at revision 0, so this never matches
	}
	for i := len(targets); i < total; i++ {
		targets = append(targets, SessionEmailTarget{ID: int64(1_000_000 + i), Revision: 0})
	}
	if len(targets) != total {
		t.Fatalf("built %d targets, want %d", len(targets), total)
	}

	stamped, err := svc.MarkSessionEmailedAtRevisionWithContext(context.Background(), targets, "20260101")
	if err != nil {
		t.Fatalf("stamp %d targets: %v", total, err)
	}

	// Exactly A and B. RETURNING order is unspecified, so compare as a set.
	if len(stamped) != 2 {
		t.Fatalf("stamped %d rows, want 2 (A and B only); got %v", len(stamped), stamped)
	}
	got := map[int64]bool{stamped[0]: true, stamped[1]: true}
	if !got[idA] || !got[idB] {
		t.Fatalf("stamped = %v, want exactly {A=%d, B=%d} (C wrong revision, rest absent)", stamped, idA, idB)
	}
}
