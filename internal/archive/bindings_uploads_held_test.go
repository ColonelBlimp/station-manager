package archive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 5a, ruling C2: a binding the running daemon started held
   (adopted, not confirmed for the account it started with) shows
   uploads_held, apart from the adoption status.

     UH1  waiting for confirmation while it is not confirmed for the current
          account; on every held binding, the default or not.
     UH2  restart required once a confirmation for the current account is
          recorded during the run.
     UH3  nothing on a binding that did not start held, whatever its marker
          says now; nothing once a restart starts it unheld.
     UH4  the PUT's answer carries it too.
     UH5  disabled and saved, it says the queued uploads are discarded at the
          next restart, waiting or confirmed (operator review of 5a).
*/

func wantHeld(t *testing.T, row types.LogbookBindingView, state, message string) {
	t.Helper()
	if state == "" {
		if row.UploadsHeld != nil {
			t.Fatalf("logbook %d uploads_held = %+v; want none", row.LogbookID, *row.UploadsHeld)
		}
		return
	}
	if row.UploadsHeld == nil || row.UploadsHeld.State != state || row.UploadsHeld.Message != message {
		t.Fatalf("logbook %d uploads_held = %+v; want %q %q", row.LogbookID, row.UploadsHeld, state, message)
	}
}

const (
	heldWaiting = "Uploads are held until adoption is confirmed for the current station account."
	heldRestart = "Uploads resume after a restart."
)

func TestUploadsHeld_UH1_WaitingOnEveryHeldBinding(t *testing.T) {
	db, a, b, _ := unadoptedHome(t)
	execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
		VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, b)
	execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'),
		remote_adopted_account = 'another'`)
	m, _ := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	m.SetHeldUploads([]string{"cloud", "cloud2"})
	v := homeView(t, m)
	wantHeld(t, adoptionRow(t, v, a), UploadsHeldWaiting, heldWaiting)
	wantHeld(t, adoptionRow(t, v, b), UploadsHeldWaiting, heldWaiting)
	// The adoption status is a separate line.
	if r := adoptionRow(t, v, a); r.Adoption == nil || r.Adoption.State != AdoptionStateNeedsConfirmation {
		t.Fatalf("adoption = %+v; want needs_confirmation beside the hold", r.Adoption)
	}
}

func TestUploadsHeld_UH2_RestartRequiredOnceConfirmed(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	// The daemon started under the old account: held.
	setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
	m.SetHeldUploads([]string{"cloud"})
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldWaiting, heldWaiting)
	now, _ := smcloudRow(t, db, a)
	reserveAndRecord(t, m, AdoptionPin{ArchiveID: homeID, Binding: now, Account: currentHomeAccount(t, cfgSvc)})
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldRestart, heldRestart)
	// The restart starts it unheld.
	m.SetHeldUploads(nil)
	wantHeld(t, adoptionRow(t, homeView(t, m), a), "", "")
}

func TestUploadsHeld_UH3_OnlyWhatStartedHeld(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	// Confirmed at start, the account changed since: not held in this run.
	setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
	wantHeld(t, adoptionRow(t, homeView(t, m), a), "", "")
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.Forwarders[0].Credentials = json.RawMessage(homeStation)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantHeld(t, adoptionRow(t, homeView(t, m), a), "", "")
}

func TestUploadsHeld_UH4_ThePutAnswersWithIt(t *testing.T) {
	db, a, _, _ := unadoptedHome(t)
	execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now')`)
	m, _ := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	m.SetHeldUploads([]string{"cloud"})
	v, err := m.ApplyBindings(context.Background(), homeID, types.ArchiveBindingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wantHeld(t, adoptionRow(t, v, a), UploadsHeldWaiting, heldWaiting)
}

// UH5 (operator review of 5a): a held binding disabled and saved says its
// queued uploads are discarded at the next restart, whether it was waiting or
// confirmed; enabled again, the line is the one before.
func TestUploadsHeld_UH5_DisabledSaysDiscarded(t *testing.T) {
	const heldDisabled = "Uploads are held; this binding is off, so its queued uploads are discarded at the next restart."
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
	m.SetHeldUploads([]string{"cloud"})
	execArchive(t, db, `UPDATE logbook_destination SET enabled = 0`)
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldDisabled, heldDisabled)
	execArchive(t, db, `UPDATE logbook_destination SET enabled = 1`)
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldWaiting, heldWaiting)
	now, _ := smcloudRow(t, db, a)
	reserveAndRecord(t, m, AdoptionPin{ArchiveID: homeID, Binding: now, Account: currentHomeAccount(t, cfgSvc)})
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldRestart, heldRestart)
	execArchive(t, db, `UPDATE logbook_destination SET enabled = 0`)
	wantHeld(t, adoptionRow(t, homeView(t, m), a), UploadsHeldDisabled, heldDisabled)
}
