package archive

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 3 (ADR 0092; rulings F1, F2, H2, 2026-10-10): bindings
   born on SM Cloud identity in the bindings view and the PUT.

     AB1  a stamp written during the run makes the view report
          restart_required: the wire changes at the next start.
     AB2  a clean born-identity binding without a cloud name takes no part in
          the legacy-name collision check (it would read as "main" beside
          Home's protected "main"); the same binding unstamped is still
          refused; a binding holding both a stamp and a reservation keeps its
          legacy claim on its name.
     AB3  a binding started held for conflicting markers says so, with its own
          fixed reason: never "waiting for confirmation", never "resumes
          after a restart", whatever its confirmation or its enabled flag;
          no needs-confirmation line beside it.
*/

const (
	heldConflictState   = "conflicting_markers"
	heldConflictMessage = "Uploads are held: this binding carries conflicting identity and adoption markers; manual recovery is required."
)

func TestIdentityBindings_AB1_AStampRequiresARestart(t *testing.T) {
	db, a, _, _ := unadoptedHome(t)
	m, _ := homeManagerCfg(t, db, a)
	running, err := db.ListLogbookDestinationsWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.SetActiveBindings(db, running)
	if v := homeView(t, m); v.RestartRequired {
		t.Fatal("restart_required before any change")
	}
	execArchive(t, db, `UPDATE logbook_destination SET identity_since = datetime('now') WHERE logbook_id = ?`, a)
	if v := homeView(t, m); !v.RestartRequired {
		t.Fatal("a stamp written during the run did not require a restart")
	}
}

func TestIdentityBindings_AB2_CleanBornIdentityLeavesTheNameCheck(t *testing.T) {
	ctx := context.Background()
	// Home's default protects "main" (no cloud name); the second binding has
	// no cloud name either, so it reads as "main" too.
	setup := func(t *testing.T, stamp string) (*sqlite.Service, int64, int64) {
		db, a, b := protectedHome(t)
		execArchive(t, db, `UPDATE logbook_destination SET credentials = NULL WHERE logbook_id IN (?, ?)`, a, b)
		if stamp != "" {
			execArchive(t, db, `UPDATE logbook_destination SET `+stamp+` WHERE logbook_id = ?`, b)
		}
		return db, a, b
	}
	t.Run("stamped: the PUT is not refused", func(t *testing.T) {
		db, a, b := setup(t, `identity_since = datetime('now')`)
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Enabled: false}); err != nil {
			t.Fatalf("PUT = %v; want a born-identity binding outside the legacy-name check", err)
		}
	})
	t.Run("control: unstamped, the PUT is refused", func(t *testing.T) {
		db, a, b := setup(t, "")
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Enabled: false}); bindingCode(err) != "binding_name_reserved" {
			t.Fatalf("PUT = %v; want binding_name_reserved", err)
		}
	})
	t.Run("mixed markers keep the legacy claim on the name", func(t *testing.T) {
		db, a, b := protectedHome(t)
		execArchive(t, db, `UPDATE logbook_destination SET identity_since = datetime('now'), adoption_reserved_at = datetime('now') WHERE logbook_id = ?`, b)
		c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Third", Callsign: "M0QQQ"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: c, Credentials: map[string]string{"logbook": "portable"}})
		if bindingCode(err) != "binding_name_reserved" {
			t.Fatalf("PUT = %v; want the mixed binding's name still protected", err)
		}
	})
	t.Run("mixed markers are not exempt from the check", func(t *testing.T) {
		// The mixed binding on b uses "portable", which a later claim on c
		// protects; any PUT checks the whole candidate, so it is refused.
		db, a, b := protectedHome(t)
		execArchive(t, db, `UPDATE logbook_destination SET identity_since = datetime('now'), adoption_reserved_at = datetime('now') WHERE logbook_id = ?`, b)
		c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Third", Callsign: "M0QQQ"})
		if err != nil {
			t.Fatal(err)
		}
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at)
			VALUES (?, 'smcloud', 'smcloud.third', 0, '{"logbook":"portable"}', datetime('now'))`, c)
		_, err = putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true})
		if bindingCode(err) != "binding_name_reserved" {
			t.Fatalf("PUT = %v; want the mixed binding still checked against another claim", err)
		}
	})
}

func TestIdentityBindings_AB3_AConflictIsItsOwnHold(t *testing.T) {
	db, a, _, _ := unadoptedHome(t)
	execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'),
		remote_adopted_account = 'another', identity_since = datetime('now') WHERE logbook_id = ?`, a)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	m.SetHeldUploads([]string{"cloud"})
	check := func(when string) {
		t.Helper()
		row := adoptionRow(t, homeView(t, m), a)
		if row.UploadsHeld == nil || row.UploadsHeld.State != heldConflictState || row.UploadsHeld.Message != heldConflictMessage {
			t.Fatalf("%s: uploads_held = %+v; want the conflict reason", when, row.UploadsHeld)
		}
		if row.Adoption != nil {
			t.Fatalf("%s: adoption = %+v; want no adoption line on a conflicting binding", when, *row.Adoption)
		}
	}
	check("held")
	execArchive(t, db, `UPDATE logbook_destination SET remote_adopted_account = ? WHERE logbook_id = ?`, currentHomeAccount(t, cfgSvc), a)
	check("confirmed for the current account")
	execArchive(t, db, `UPDATE logbook_destination SET enabled = 0 WHERE logbook_id = ?`, a)
	check("disabled")
}
