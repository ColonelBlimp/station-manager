package store

import (
	"context"
	"testing"
)

// W-0021 Q7 (ADR 0071 refinement, PQ1): the whole-archive export reads the
// archive's metadata, its logbook inventory, the counts and the rows in ONE
// snapshot.
//
//	AS1 a row moved between the archive's logbooks after the head was read
//	    (the barrier: onHead commits the move and returns only then) is
//	    exported where the head counted it, at its old revision; the head's
//	    counts agree with the rows; a fresh export shows the move.

const (
	asArchive = "01920000-0000-7000-8000-000000000e01"
	asLB1     = "01920000-0000-7000-8000-000000000f01"
	asLB2     = "01920000-0000-7000-8000-000000000f02"
)

type asRow struct {
	logbook  string
	revision int64
}

func asExport(t *testing.T, s *Store, tid int64, onHead func(ArchiveHead)) (ArchiveHead, map[string]asRow) {
	t.Helper()
	var head ArchiveHead
	byID := map[int64]string{}
	rows := map[string]asRow{}
	err := s.ArchiveExportSnapshot(context.Background(), tid, asArchive,
		func(h ArchiveHead) error {
			head = h
			for _, lb := range h.Logbooks {
				if lb.UUID != nil {
					byID[lb.ID] = *lb.UUID
				}
			}
			if onHead != nil {
				onHead(h)
			}
			return nil
		},
		func(r Record) error {
			if _, dup := rows[r.UUID]; dup {
				t.Fatalf("row %s exported twice", r.UUID)
			}
			rows[r.UUID] = asRow{byID[r.LogbookID], r.Revision}
			return nil
		})
	if err != nil {
		t.Fatalf("ArchiveExportSnapshot: %v", err)
	}
	return head, rows
}

func TestArchiveExportSnapshot_AS1_AMoveAfterTheHeadIsNotSeen(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tid, err := s.EnsureTenant(ctx, "7Q5MLV", "")
	if err != nil {
		t.Fatal(err)
	}
	target := func(lb, label string) IdentityTarget {
		return IdentityTarget{ArchiveUUID: asArchive, ArchiveLabel: "Contest", LogbookUUID: lb, LogbookLabel: label, Callsign: "7Q5MLV"}
	}
	r1 := rec(uuidA, tid, 0, baseTime, nil, `{"v":1}`)
	r1.Revision = 1
	r2 := rec(uuidB, tid, 0, baseTime, nil, `{"v":2}`)
	r2.Revision = 1
	if _, err := s.UpsertIdentity(ctx, tid, target(asLB1, "one"), []Record{r1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertIdentity(ctx, tid, target(asLB2, "two"), []Record{r2}); err != nil {
		t.Fatal(err)
	}

	head, rows := asExport(t, s, tid, func(ArchiveHead) {
		moved := rec(uuidA, tid, 0, baseTime.Add(1), nil, `{"v":1,"moved":true}`)
		moved.Revision = 2
		if n, err := s.UpsertIdentity(ctx, tid, target(asLB2, "two"), []Record{moved}); err != nil || n != 1 {
			t.Fatalf("the barrier's move: applied %d, %v", n, err)
		}
	})
	counts := map[string]int{}
	for _, lb := range head.Logbooks {
		if lb.UUID == nil {
			t.Fatalf("a managed archive's logbook with no UUID: %+v", lb)
		}
		if _, dup := counts[*lb.UUID]; dup {
			t.Fatalf("logbook %s in the head twice", *lb.UUID)
		}
		counts[*lb.UUID] = lb.Count
	}
	if head.ArchiveUUID != asArchive || head.Legacy || len(head.Logbooks) != 2 || counts[asLB1] != 1 || counts[asLB2] != 1 {
		t.Fatalf("head = %+v; want Contest, not legacy, one row in each logbook", head)
	}
	if got := rows[uuidA]; got != (asRow{asLB1, 1}) {
		t.Fatalf("the moved row exported as %+v; want it where the head counted it, %s at revision 1", got, asLB1)
	}
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.logbook]++
	}
	if seen[asLB1] != counts[asLB1] || seen[asLB2] != counts[asLB2] || len(rows) != 2 {
		t.Fatalf("rows %+v disagree with the head's counts %v", rows, counts)
	}

	_, after := asExport(t, s, tid, nil)
	if got := after[uuidA]; got != (asRow{asLB2, 2}) {
		t.Fatalf("a fresh export shows the moved row as %+v; want %s at revision 2 (the barrier's move happened)", got, asLB2)
	}
}
