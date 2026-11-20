package sqlite

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// The migration sets go one way. There is no downgrade: the command, the
// function and the down files were removed (W-0027, 2026-11-19), because a
// build never has to return a file to an older build's schema.
//
// A down file must not come back by habit. The migration library takes a
// version with no down file for an empty step, so a set in which only some
// versions have one is worse than a set with none: a step down would move the
// version marker and leave the newer tables in place.
func TestMigrationSets_HoldOnlyUpFiles(t *testing.T) {
	for _, set := range allMigrationSets {
		entries, err := fs.ReadDir(migrationFiles, "migrations/"+set)
		if err != nil {
			t.Fatalf("read the %s set: %v", set, err)
		}
		if len(entries) == 0 {
			t.Fatalf("the %s set is empty; the test would prove nothing", set)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".up.sql") {
				t.Errorf("%s set: %s is not an up file", set, e.Name())
			}
		}
	}
}

// A file left by an older build comes up to the head with its rows when it is
// opened. Each file is built UP to its version, as that build left it, and
// holds a logbook and a QSO written in that version's shape.
func TestMigrate_OlderFilesComeUpToTheHead(t *testing.T) {
	for _, version := range []uint{5, 14, 18} {
		t.Run(fmt.Sprintf("a file at %d", version), func(t *testing.T) {
			svc := testServiceAtVersion(t, version)
			if v := schemaVersion(t, svc); v != int(version) {
				t.Fatalf("the fixture is at %d, want %d", v, version)
			}
			seedLogbookAndQsoRow(t, svc.handle)

			if err := svc.Migrate(); err != nil {
				t.Fatalf("migrate up from %d: %v", version, err)
			}
			if v, head := schemaVersion(t, svc), logHead(t); v != head {
				t.Fatalf("schema version = %d after the way up, want the head %d", v, head)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM `+schemaMigrationsTable(MigrationSetLog)+` WHERE dirty = 1`); n != 0 {
				t.Fatal("the way up left the file dirty")
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM logbook WHERE id = 1 AND callsign = 'G4ABC' AND name = 'L'`); n != 1 {
				t.Fatalf("logbook rows kept = %d, want 1", n)
			}
			if n := countT(t, svc, `SELECT COUNT(*) FROM qso WHERE id = 1 AND call = 'G4ABC' AND band = '40m' AND logbook_id = 1`); n != 1 {
				t.Fatalf("qso rows kept = %d, want 1", n)
			}
		})
	}
}
