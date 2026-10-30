package sqlite

import (
	"context"
	"testing"
)

// W-0023 weekend slice: the durable contest serial counter. Starting a contest
// QSO consumes the next serial for good; a retry for the same draft gets the
// same number; nothing ever hands a serial out twice.

func allocT(t *testing.T, svc *Service, logbookID int64, contest, draft string) int64 {
	t.Helper()
	n, err := svc.AllocateContestSerialWithContext(context.Background(), logbookID, contest, "20261031", draft)
	if err != nil {
		t.Fatalf("allocate(%d, %q, %q): %v", logbookID, contest, draft, err)
	}
	return n
}

func TestAllocateContestSerial_StartsAtOneAndNeverReuses(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)

	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-a"); n != 1 {
		t.Fatalf("first serial = %d, want 1", n)
	}
	// draft-a abandoned (never logged): its number stays consumed.
	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-b"); n != 2 {
		t.Fatalf("next draft = %d, want 2 (an abandoned serial is a gap, not reused)", n)
	}
	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-c"); n != 3 {
		t.Fatalf("third draft = %d, want 3", n)
	}
}

func TestAllocateContestSerial_RetryForTheSameDraftKeepsItsNumber(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)

	first := allocT(t, svc, 1, "UKEIDXSSB", "draft-a")
	if again := allocT(t, svc, 1, "UKEIDXSSB", "draft-a"); again != first {
		t.Fatalf("retry for the same draft = %d, want %d (a lost response must not consume another)", again, first)
	}
	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-b"); n != first+1 {
		t.Fatalf("next draft after a retry = %d, want %d", n, first+1)
	}
	// Another tab takes a number, then the first draft retries (its response
	// was lost): it still gets ITS number, never a fresh one.
	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-a"); n != first {
		t.Fatalf("draft-a after another draft = %d, want its own %d", n, first)
	}
	if n := allocT(t, svc, 1, "UKEIDXSSB", "draft-c"); n != first+2 {
		t.Fatalf("next new draft = %d, want %d", n, first+2)
	}
}

func TestAllocateContestSerial_ScopedByLogbookAndContest(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)

	allocT(t, svc, 1, "UKEIDXSSB", "a")
	allocT(t, svc, 1, "UKEIDXSSB", "b")
	if n := allocT(t, svc, 2, "UKEIDXSSB", "c"); n != 1 {
		t.Fatalf("another logbook's first serial = %d, want 1", n)
	}
	if n := allocT(t, svc, 1, "UKEIDXCW", "d"); n != 1 {
		t.Fatalf("another contest's first serial = %d, want 1", n)
	}
	if n := allocT(t, svc, 1, "UKEIDXSSB", "e"); n != 3 {
		t.Fatalf("original sequence = %d, want 3", n)
	}
}

// The occurrence (the event's start date) is part of the sequence: next
// year's edition, or a dry run under another date, starts again at 1.
func TestAllocateContestSerial_ScopedByOccurrence(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	ctx := context.Background()
	alloc := func(occ, draft string) int64 {
		n, err := svc.AllocateContestSerialWithContext(ctx, 1, "UKEIDXSSB", occ, draft)
		if err != nil {
			t.Fatalf("allocate(%s, %s): %v", occ, draft, err)
		}
		return n
	}
	alloc("20261030", "dry-1")
	alloc("20261030", "dry-2")
	if n := alloc("20261031", "real-1"); n != 1 {
		t.Fatalf("the real event's first serial = %d, want 1 (a dry run under another date is its own sequence)", n)
	}
	if n := alloc("20271023", "next-year"); n != 1 {
		t.Fatalf("next year's first serial = %d, want 1", n)
	}
	if n := alloc("20261031", "real-2"); n != 2 {
		t.Fatalf("the real event's second serial = %d, want 2", n)
	}
}

func TestAllocateContestSerial_RefusesBlankContestOrDraft(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	ctx := context.Background()
	if _, err := svc.AllocateContestSerialWithContext(ctx, 1, "UKEIDXSSB", "", "a"); err == nil {
		t.Fatal("blank occurrence: want an error")
	}
	if _, err := svc.AllocateContestSerialWithContext(ctx, 1, "", "20261031", "a"); err == nil {
		t.Fatal("blank contest id: want an error")
	}
	if _, err := svc.AllocateContestSerialWithContext(ctx, 1, "UKEIDXSSB", "20261031", ""); err == nil {
		t.Fatal("blank draft: want an error")
	}
	if n := countT(t, svc, `SELECT count(*) FROM contest_serial_allocation`); n != 0 {
		t.Fatalf("rows after refusals = %d, want 0", n)
	}
}

func TestMigrate0019_DownDropsTheCounter(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	allocT(t, svc, 1, "UKEIDXSSB", "a")
	if _, err := svc.DowngradeLogSchemaTo(18); err != nil {
		t.Fatalf("downgrade to 18: %v", err)
	}
	if n := countT(t, svc, `SELECT count(*) FROM sqlite_master WHERE name = 'contest_serial_allocation'`); n != 0 {
		t.Fatal("contest_serial survived the down step")
	}
}

// A Sent # typed by hand (an override, or after a failed allocation) and saved
// is a serial on air too: the next allocation goes above it, never repeats it.
// Only this logbook's QSOs of this contest occurrence count.
func TestAllocateContestSerial_GoesAboveSavedManualSerials(t *testing.T) {
	svc := testService(t)
	seedTwoLogbookFile(t, svc, 1, lbUUID2)
	save := func(logbookID int64, call, contest, occurrence, stx string) {
		t.Helper()
		q := validTestQso(logbookID, call, "20m", "SSB", "20261031", "1300")
		q.QsoDetails.ContestId, q.QsoDetails.STX, q.AppSmContestStart = contest, stx, occurrence
		if _, err := svc.InsertQso(q); err != nil {
			t.Fatalf("insert %s: %v", call, err)
		}
	}
	if n := allocT(t, svc, 1, "UKEIDXSSB", "a"); n != 1 {
		t.Fatalf("first = %d, want 1", n)
	}
	save(1, "G4MAN", "UKEIDXSSB", "20261031", "2")  // allocated 1, sent and saved 2 by hand
	save(1, "G4DRY", "UKEIDXSSB", "20261030", "50") // another occurrence
	save(2, "G4LB2", "UKEIDXSSB", "20261031", "60") // another logbook
	save(1, "G4CW", "UKEIDXCW", "20261031", "70")   // another contest
	save(1, "G4TXT", "UKEIDXSSB", "20261031", "x9") // not a number: ignored
	if n := allocT(t, svc, 1, "UKEIDXSSB", "b"); n != 3 {
		t.Fatalf("after a saved manual 2 = %d, want 3", n)
	}
	// With no allocation yet, a saved manual serial still sets the floor.
	save(1, "G4FIRST", "UKEIDXSSB", "20261101", "5")
	if n := allocT(t, svc, 1, "UKEIDXSSB", "c"); n != 4 {
		t.Fatalf("20261031 sequence = %d, want 4", n)
	}
	n, err := svc.AllocateContestSerialWithContext(context.Background(), 1, "UKEIDXSSB", "20261101", "d")
	if err != nil || n != 6 {
		t.Fatalf("20261101 first allocation = %d (%v), want 6 (above the saved manual 5)", n, err)
	}
}
