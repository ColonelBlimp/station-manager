package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The default epoch counts the moves of the file's default pointer (ADR 0100
// §4; W-0029 part 1, group B). A session remembers the epoch it opened at, so
// a change of default is seen even when the pointer has since come back to the
// same logbook. The two setters of the pointer advance it, in the statement
// that moves the pointer, so the two commit together or not at all.

type pointerSetter struct {
	name string
	set  func(*Service, context.Context, int64) error
}

var pointerSetters = []pointerSetter{
	{"the ordinary setter", (*Service).SetArchiveDefaultLogbookWithContext},
	{"the durable setter", (*Service).SetArchiveDefaultLogbookDurablyWithContext},
}

// pointerAndEpoch reads both from the identity row, as stored.
func pointerAndEpoch(t *testing.T, svc *Service) (pointer int64, epoch int) {
	t.Helper()
	var p sql.NullInt64
	if err := svc.handle.QueryRow(`SELECT default_logbook_id, default_epoch FROM archive_metadata WHERE singleton = 1`).
		Scan(&p, &epoch); err != nil {
		t.Fatalf("read the pointer and the epoch: %v", err)
	}
	return p.Int64, epoch
}

// epochBed is a file with an identity row, no pointer, and two live logbooks.
func epochBed(t *testing.T) (svc *Service, x, y int64) {
	t.Helper()
	svc, _ = testFileService(t)
	ctx := context.Background()
	var err error
	if x, err = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "X", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook X: %v", err)
	}
	if y, err = svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Y", Callsign: "G4ABC"}); err != nil {
		t.Fatalf("logbook Y: %v", err)
	}
	if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	if p, e := pointerAndEpoch(t, svc); p != 0 || e != 0 {
		t.Fatalf("the bed starts at pointer %d, epoch %d; want none and 0", p, e)
	}
	return svc, x, y
}

func TestDefaultEpoch_AdvancesWithEveryMoveOfThePointer(t *testing.T) {
	ctx := context.Background()
	for _, s := range pointerSetters {
		t.Run(s.name, func(t *testing.T) {
			svc, x, y := epochBed(t)
			steps := []struct {
				what      string
				to        int64
				wantEpoch int
			}{
				{"none to X", x, 1},
				{"X to Y", y, 2},
				{"Y to Y, a rewrite", y, 2},
				{"Y to X, back again", x, 3},
				{"X to X, a rewrite", x, 3},
			}
			for _, step := range steps {
				if err := s.set(svc, ctx, step.to); err != nil {
					t.Fatalf("%s: %v", step.what, err)
				}
				if p, e := pointerAndEpoch(t, svc); p != step.to || e != step.wantEpoch {
					t.Fatalf("%s: pointer %d, epoch %d; want pointer %d, epoch %d", step.what, p, e, step.to, step.wantEpoch)
				}
			}
		})
	}
}

// A write a setter refuses today changes neither value. What each refuses is
// not changed by the epoch: the ordinary setter refuses a logbook that does
// not exist, and the durable one also refuses a deleted logbook.
func TestDefaultEpoch_ARefusedWriteChangesNeitherValue(t *testing.T) {
	ctx := context.Background()
	for _, s := range pointerSetters {
		t.Run(s.name+", a logbook that does not exist", func(t *testing.T) {
			svc, x, _ := epochBed(t)
			if err := s.set(svc, ctx, x); err != nil {
				t.Fatalf("set X: %v", err)
			}
			if err := s.set(svc, ctx, 999); err == nil {
				t.Fatal("a pointer to no logbook was accepted")
			}
			if p, e := pointerAndEpoch(t, svc); p != x || e != 1 {
				t.Fatalf("after the refusal: pointer %d, epoch %d; want %d and 1", p, e, x)
			}
		})
	}
	t.Run("the durable setter, a deleted logbook", func(t *testing.T) {
		svc, x, y := epochBed(t)
		if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, x); err != nil {
			t.Fatalf("set X: %v", err)
		}
		execT(t, svc, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = ?`, y)
		if err := svc.SetArchiveDefaultLogbookDurablyWithContext(ctx, y); err == nil {
			t.Fatal("the durable setter accepted a deleted logbook")
		}
		if p, e := pointerAndEpoch(t, svc); p != x || e != 1 {
			t.Fatalf("after the refusal: pointer %d, epoch %d; want %d and 1", p, e, x)
		}
	})
	t.Run("the ordinary setter still accepts a deleted logbook, and that move counts", func(t *testing.T) {
		svc, x, y := epochBed(t)
		if err := svc.SetArchiveDefaultLogbookWithContext(ctx, x); err != nil {
			t.Fatalf("set X: %v", err)
		}
		execT(t, svc, `UPDATE logbook SET deleted_at = datetime('now') WHERE id = ?`, y)
		if err := svc.SetArchiveDefaultLogbookWithContext(ctx, y); err != nil {
			t.Fatalf("the ordinary setter's existing behaviour changed: %v", err)
		}
		if p, e := pointerAndEpoch(t, svc); p != y || e != 2 {
			t.Fatalf("pointer %d, epoch %d; want %d and 2", p, e, y)
		}
	})
	for _, s := range pointerSetters {
		t.Run(s.name+", no identity row", func(t *testing.T) {
			svc, _ := testFileService(t)
			id, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "X", Callsign: "G4ABC"})
			if err != nil {
				t.Fatalf("logbook: %v", err)
			}
			if err := s.set(svc, ctx, id); err == nil {
				t.Fatal("a pointer was set on a file with no identity row")
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM archive_metadata`); n != 0 {
				t.Fatalf("the refusal left %d identity row(s)", n)
			}
		})
	}
}

// The logbook the archive file names as its default cannot be deleted, by the
// same statement that deletes: the file's pointer is the authority for the
// default (ADR 0071), and the configuration's id is only its projection. After
// a setup save that failed before its configuration commit, the configuration
// does not yet name the logbook the file points at, so the caller's own check
// of the configured id does not protect it (the clean-room review of 3f2f08aa).
func TestDeleteLogbook_RefusesTheArchivesDefault(t *testing.T) {
	ctx := context.Background()
	svc, x, y := epochBed(t)
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, x); err != nil {
		t.Fatalf("set the default: %v", err)
	}
	live := func(id int64) bool {
		t.Helper()
		return countT(t, svc, `SELECT COUNT(*) FROM logbook WHERE id = ? AND deleted_at IS NULL`, id) == 1
	}

	if err := svc.DeleteLogbookByIDWithContext(ctx, x); !stderr.Is(err, errors.ErrLogbookIsDefault) {
		t.Fatalf("deleting the file's default: %v; want ErrLogbookIsDefault", err)
	}
	if !live(x) {
		t.Fatal("the file's default logbook was deleted")
	}
	if p, e := pointerAndEpoch(t, svc); p != x || e != 1 {
		t.Fatalf("the refusal changed the pointer or the epoch: %d, %d", p, e)
	}

	// Once the default is another logbook, the first can go; the new default cannot.
	if err := svc.SetArchiveDefaultLogbookWithContext(ctx, y); err != nil {
		t.Fatalf("move the default: %v", err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, x); err != nil || live(x) {
		t.Fatalf("deleting the former default: %v, still live %v", err, live(x))
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, y); !stderr.Is(err, errors.ErrLogbookIsDefault) || !live(y) {
		t.Fatalf("deleting the new default: %v, live %v; want it refused", err, live(y))
	}
}

// A file with no identity row has no default: its logbooks are deleted as before.
func TestDeleteLogbook_WithNoArchiveIdentityIsAsBefore(t *testing.T) {
	ctx := context.Background()
	svc, _ := testFileService(t)
	id, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "X", Callsign: "G4ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, id); err != nil {
		t.Fatalf("delete with no identity row: %v", err)
	}
	if err := svc.DeleteLogbookByIDWithContext(ctx, id); !stderr.Is(err, errors.ErrNotFound) {
		t.Fatalf("a second delete: %v; want ErrNotFound", err)
	}
}
