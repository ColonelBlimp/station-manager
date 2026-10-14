package sqlite

import (
	"context"
	"fmt"
	"testing"
)

// W-0021 5F.5 (ruling K4): `smd restore` refuses a legacy restore into a
// binding born on identity, enabled or not, in any archive, reading the closed
// file read-only. The peek must carry each binding's identity_since.
func TestPeekDestinationBindings_CarriesIdentitySince(t *testing.T) {
	svc, path := testFileService(t)
	ctx := context.Background()
	if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO logbook (name, callsign) VALUES ('On', 'M0TEST'), ('Off', 'M0TEST'), ('Legacy', 'M0TEST')`,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, identity_since)
			VALUES ((SELECT id FROM logbook WHERE name = 'On'), 'smcloud', 'stamped-on', 1, datetime('now')),
			       ((SELECT id FROM logbook WHERE name = 'Off'), 'smcloud', 'stamped-off', 0, datetime('now')),
			       ((SELECT id FROM logbook WHERE name = 'Legacy'), 'smcloud', 'legacy', 1, NULL)`,
	} {
		if _, err := svc.handle.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	peek, err := PeekDestinationBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	stamped := map[string]bool{}
	for _, b := range peek.Bindings {
		stamped[b.ForwarderName] = b.IdentitySince != nil
	}
	want := map[string]bool{"stamped-on": true, "stamped-off": true, "legacy": false}
	for name, w := range want {
		got, ok := stamped[name]
		if !ok || got != w {
			t.Fatalf("binding %s: peeked stamped=%v (present %v); want %v (all: %v)", name, got, ok, w, stamped)
		}
	}
}

// W-0021 5F.5 (operator review, 2026-10-14): an identity restore resolves its
// target from the peek's logbooks. Schemas 12 and 13 already carry the archive
// and logbook UUIDs but no binding table, so the logbooks must be read
// whether the binding table exists or not. Below 12 there is no logbook UUID,
// and the logbooks read with none.
func TestPeekDestinationBindings_ReadsLogbooksBelowTheBindingTable(t *testing.T) {
	for _, tc := range []struct {
		version  uint
		withUUID bool
	}{{13, true}, {12, true}, {11, false}} {
		t.Run(fmt.Sprintf("schema %d", tc.version), func(t *testing.T) {
			svc, path := testFileService(t)
			ctx := context.Background()
			if _, err := svc.EnsureArchiveIdentityWithContext(ctx, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.handle.ExecContext(ctx, `INSERT INTO logbook (name, callsign, uuid) VALUES ('First', 'M0TEST', '019fd5c5-efcc-7193-be4f-1fee532ee3aa'), ('Second', 'M0TEST', '019fd5c5-efcc-7193-be4f-1fee532ee3ab')`); err != nil {
				t.Fatal(err)
			}
			migrateToVersion(t, svc, tc.version)
			peek, err := PeekDestinationBindings(path)
			if err != nil {
				t.Fatalf("peek at schema %d: %v", tc.version, err)
			}
			if peek.HasBindingsTable {
				t.Fatalf("schema %d reported a binding table", tc.version)
			}
			if len(peek.Logbooks) != 2 {
				t.Fatalf("schema %d: peeked logbooks %+v; want both live logbooks", tc.version, peek.Logbooks)
			}
			for _, lb := range peek.Logbooks {
				if (lb.UUID != "") != tc.withUUID {
					t.Fatalf("schema %d: logbook %d UUID %q; want one: %v", tc.version, lb.ID, lb.UUID, tc.withUUID)
				}
			}
			if peek.HasIdentity != tc.withUUID {
				t.Fatalf("schema %d: HasIdentity = %v; want %v", tc.version, peek.HasIdentity, tc.withUUID)
			}
		})
	}
}
