package main

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 2 (ADR 0097 §5): the restart boundary of a former default's
   recovery. Home's default moves (the archive's own pointer, as the start
   projects it) off a logbook whose SM Cloud binding was adopted under another
   station account; the new default has its own, unmarked binding.

     FW1  the former default starts held; its confirmation, recorded during
          the run though it is no longer the default, turns the held line to
          "restart required"; after the restart its queued upload drains
          through the identity worker for ITS logbook, and nothing is held.
          The new default's binding is untouched throughout: unmarked,
          enabled, never held.
*/

func TestFormerDefault_FW1_ConfirmedThenRestarted(t *testing.T) {
	cloud := newIdentityCloud(t)
	d, orch := homeWithCloud(t, cloud)
	second, err := d.db.InsertLogbook(types.Logbook{Name: "Second", Callsign: "7Q5MLV"})
	if err != nil {
		t.Fatal(err)
	}
	u1 := logQso(t, d, "DL9UW") // into logbook 1, the default for now
	stopGen(t, orch)

	rawExec(t, d, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'smcloud-b', 1, '{"logbook":"portable"}')`, second)
	stampAdoption(t, d, true, true, "another")
	// The archive's pointer is the default (ADR 0071); the start projects it.
	rawExec(t, d, `UPDATE archive_metadata SET default_logbook_id = ? WHERE singleton = 1`, second)
	cloud.setStatus(0)
	ownerOnlySocketDir(t, d)
	d2, orch2 := startGen(t, d)
	if got := d2.cfgSvc.Snapshot().DefaultLogbookID; got != second {
		t.Fatalf("the default is %d; want the second logbook %d", got, second)
	}
	if h := heldLine(t, d2); h == nil || h.State != archive.UploadsHeldWaiting {
		t.Fatalf("uploads_held = %+v; want waiting for confirmation", h)
	}

	// The adopter's confirmation of the former default, for the current account.
	former := bindingOf(t, d2, "smcloud")
	pin := archive.AdoptionPin{ArchiveID: d2.cfgSvc.Snapshot().ActiveQsoArchiveID, Binding: former, Account: startAccount(t, d2)}
	if recorded, err := d2.archives.RecordAdoption(context.Background(), pin); err != nil || !recorded {
		t.Fatalf("RecordAdoption of the former default = %v, %v; want recorded", recorded, err)
	}
	if h := heldLine(t, d2); h == nil || h.State != archive.UploadsHeldRestart {
		t.Fatalf("uploads_held = %+v; want restart required", h)
	}

	d3, _ := restartGen(t, d2, orch2)
	waitUntil(t, "the former default's held upload to drain", func() bool { return uploadStatus(t, d3, u1) == "uploaded" })
	want := identityPath(t, d3) // logbook 1's identity path
	for _, p := range cloud.seen() {
		if p != want {
			t.Fatalf("an upload went to %s; want only %s", p, want)
		}
	}
	if h := heldLine(t, d3); h != nil {
		t.Fatalf("uploads_held = %+v after the restart", *h)
	}
	b := bindingOf(t, d3, "smcloud-b")
	if !b.Enabled || b.AdoptionReservedAt != nil || b.RemoteAdoptedAt != nil || b.IdentitySince != nil {
		t.Fatalf("the new default's binding = %+v; want enabled and unmarked", b)
	}
}

func bindingOf(t *testing.T, d *daemon, forwarder string) types.LogbookDestination {
	t.Helper()
	rows, err := d.db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ForwarderName == forwarder {
			return r
		}
	}
	t.Fatalf("no binding %q", forwarder)
	return types.LogbookDestination{}
}
