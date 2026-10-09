package archive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4b2: the adoption status on the bindings view (ADR 0090
   T4; ADR 0091, "status belongs to its subject").

     AS1  a binding confirmed under the current account reads "adopted", or
          "adopted, applies after a restart" while the running generation
          started without that confirmation; disabled or not.
     AS2  the adopter's status is shown only on its subject: the active
          archive, the default logbook's binding, its name and the account. A
          moved default, a changed account, a renamed binding or another
          archive shows none of it; a late status for an old subject never
          reaches the new one.
     AS3  adopted under another account, with no status of its own subject:
          "Adoption needs confirmation for the current station account."
     AS4  nowhere else: not on another logbook, another destination, a
          disabled unconfirmed default, or outside Home.
     AS5  SubjectOf: the normalized name and the account fingerprint; an
          incomplete account or an unreadable name has no subject.
     AS6  neither the subject nor the fingerprint reaches the wire.
*/

func adoptionRow(t *testing.T, v types.ArchiveBindingsView, logbookID int64) types.LogbookBindingView {
	t.Helper()
	for _, row := range destView(t, v, "smcloud").Logbooks {
		if row.LogbookID == logbookID {
			return row
		}
	}
	t.Fatalf("no smcloud row for logbook %d", logbookID)
	return types.LogbookBindingView{}
}

func homeView(t *testing.T, m *Manager) types.ArchiveBindingsView {
	t.Helper()
	v, err := m.Bindings(context.Background(), homeID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func wantAdoption(t *testing.T, row types.LogbookBindingView, state, message string) {
	t.Helper()
	if state == "" {
		if row.Adoption != nil {
			t.Fatalf("logbook %d shows adoption %+v; want none", row.LogbookID, *row.Adoption)
		}
		return
	}
	if row.Adoption == nil || row.Adoption.State != state || row.Adoption.Message != message {
		t.Fatalf("logbook %d adoption = %+v; want %q %q", row.LogbookID, row.Adoption, state, message)
	}
}

// statusOf is a fixed adopter status for the subject of the binding read.
func statusOf(t *testing.T, cfgSvc *config.Service, read types.LogbookDestination, state, message string) func() AdoptionStatus {
	t.Helper()
	sub, err := SubjectOf(cfgSvc.Snapshot(), read)
	if err != nil {
		t.Fatal(err)
	}
	return func() AdoptionStatus { return AdoptionStatus{Subject: sub, State: state, Message: message} }
}

func TestAdoptionStatus_AS1_ConfirmedReadsAdopted(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, []types.LogbookDestination{read})
	reserveAndRecord(t, m, homePin(t, read))
	// A stale status of the same subject never hides the confirmation.
	m.SetAdoptionStatus(statusOf(t, cfgSvc, read, "unreachable", "Not yet: the server could not be reached (retrying)."))
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), AdoptionStateAdoptedRestart, "Adopted; applies after a restart.")

	confirmed, _ := smcloudRow(t, db, a)
	m.SetActiveBindings(db, []types.LogbookDestination{confirmed})
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), AdoptionStateAdopted, "Adopted.")

	execArchive(t, db, `UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), AdoptionStateAdopted, "Adopted.")
}

func TestAdoptionStatus_AS1_StartedConfirmedUnderAnotherAccount(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	before, _ := smcloudRow(t, db, a)
	setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
	after, _ := smcloudRow(t, db, a)
	pin := AdoptionPin{ArchiveID: homeID, Binding: after, Account: currentHomeAccount(t, cfgSvc)}
	reserveAndRecord(t, m, pin)
	m.SetActiveBindings(db, []types.LogbookDestination{before})
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), AdoptionStateAdoptedRestart, "Adopted; applies after a restart.")
}

func TestAdoptionStatus_AS2_StatusBelongsToItsSubject(t *testing.T) {
	const state, message = "unauthorized", "Not adopted: the token was refused."
	t.Run("shown on its subject, through a PUT's view too", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		m.SetAdoptionStatus(statusOf(t, cfgSvc, read, state, message))
		wantAdoption(t, adoptionRow(t, homeView(t, m), a), state, message)
		v, err := m.ApplyBindings(context.Background(), homeID, types.ArchiveBindingsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		wantAdoption(t, adoptionRow(t, v, a), state, message)
	})
	for name, change := range map[string]func(t *testing.T, db *sqlite.Service, cfgSvc *config.Service, a, b int64){
		"the default moved": func(t *testing.T, db *sqlite.Service, cfgSvc *config.Service, a, b int64) {
			execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"shack2"}')`, b)
			if _, err := cfgSvc.Update(func(c *config.Config) error { c.DefaultLogbookID = b; return nil }); err != nil {
				t.Fatal(err)
			}
		},
		"the account changed": func(t *testing.T, _ *sqlite.Service, cfgSvc *config.Service, _, _ int64) {
			setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
		},
		"the binding was renamed": func(t *testing.T, db *sqlite.Service, _ *config.Service, _, _ int64) {
			execArchive(t, db, `UPDATE logbook_destination SET credentials = '{"logbook":"other"}' WHERE forwarder_name = 'cloud'`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, a, b, read := unadoptedHome(t)
			m, cfgSvc := homeManagerCfg(t, db, a)
			m.SetActiveBindings(db, nil)
			// The status is fixed before the change: a late completion of the
			// old attempt publishes exactly this.
			m.SetAdoptionStatus(statusOf(t, cfgSvc, read, state, message))
			change(t, db, cfgSvc, a, b)
			v := homeView(t, m)
			wantAdoption(t, adoptionRow(t, v, a), "", "")
			wantAdoption(t, adoptionRow(t, v, b), "", "")
		})
	}
	t.Run("another archive's status", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		other := statusOf(t, cfgSvc, read, state, message)
		m.SetAdoptionStatus(func() AdoptionStatus {
			s := other()
			s.Subject.ArchiveID = "019fd5c5-efcc-7193-be4f-1fee532ee3a9"
			return s
		})
		wantAdoption(t, adoptionRow(t, homeView(t, m), a), "", "")
	})
}

func TestAdoptionStatus_AS3_AdoptedUnderAnotherAccount(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	m.SetAdoptionStatus(statusOf(t, cfgSvc, read, "unauthorized", "Not adopted: the token was refused."))
	setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), AdoptionStateNeedsConfirmation, "Adoption needs confirmation for the current station account.")

	// A status of the current subject (an attempt confirming it) wins.
	now, _ := smcloudRow(t, db, a)
	m.SetAdoptionStatus(statusOf(t, cfgSvc, now, "confirming", "Confirming the adoption for the current station account."))
	wantAdoption(t, adoptionRow(t, homeView(t, m), a), "confirming", "Confirming the adoption for the current station account.")
}

func TestAdoptionStatus_AS4_NowhereElse(t *testing.T) {
	const state, message = "unreachable", "Not yet: the server could not be reached (retrying)."
	t.Run("another logbook's binding, the same name's status", func(t *testing.T) {
		db, a, b, _ := unadoptedHome(t)
		execArchive(t, db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"elsewhere"}')`, b)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		second, _ := smcloudRow(t, db, b)
		m.SetAdoptionStatus(statusOf(t, cfgSvc, second, state, message))
		v := homeView(t, m)
		wantAdoption(t, adoptionRow(t, v, a), "", "")
		wantAdoption(t, adoptionRow(t, v, b), "", "")
	})
	t.Run("a disabled, unconfirmed default", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		m.SetAdoptionStatus(statusOf(t, cfgSvc, read, state, message))
		execArchive(t, db, `UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		wantAdoption(t, adoptionRow(t, homeView(t, m), a), "", "")
	})
	t.Run("adopted under another account, disabled", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		reserveAndRecord(t, m, homePin(t, read))
		execArchive(t, db, `UPDATE logbook_destination SET enabled = 0 WHERE forwarder_name = 'cloud'`)
		setStation(t, cfgSvc, `{"url":"https://c","token":"rotated"}`)
		wantAdoption(t, adoptionRow(t, homeView(t, m), a), "", "")
	})
	t.Run("outside Home", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		reserveAndRecord(t, m, homePin(t, read))
		m.SetAdoptionStatus(statusOf(t, cfgSvc, read, state, message))
		if _, err := cfgSvc.Update(func(c *config.Config) error {
			c.QsoArchives[0].Ownership, c.QsoArchives[0].Path = types.QsoArchiveOwnershipManaged, ""
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		wantAdoption(t, adoptionRow(t, homeView(t, m), a), "", "")
	})
}

func TestAdoptionStatus_AS5_SubjectOf(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	_, cfgSvc := homeManagerCfg(t, db, a)
	sub, err := SubjectOf(cfgSvc.Snapshot(), read)
	want := AdoptionSubject{ArchiveID: homeID, ForwarderName: "cloud", LogbookID: a, Name: "shack", Account: accountOf(t, homeStation)}
	if err != nil || sub != want {
		t.Fatalf("SubjectOf = %+v, %v; want %+v", sub, err, want)
	}
	padded := read
	padded.Credentials = json.RawMessage(`{"logbook":"  shack "}`)
	if got, _ := SubjectOf(cfgSvc.Snapshot(), padded); got.Name != "shack" {
		t.Fatalf("padded name = %q; want shack", got.Name)
	}
	cleared := read
	cleared.Credentials = nil
	if got, _ := SubjectOf(cfgSvc.Snapshot(), cleared); got.Name != "main" {
		t.Fatalf("cleared name = %q; want main", got.Name)
	}
	broken := read
	broken.Credentials = json.RawMessage(`[`)
	if _, err := SubjectOf(cfgSvc.Snapshot(), broken); err == nil {
		t.Fatal("an unreadable name has a subject")
	}
	setStation(t, cfgSvc, `{"url":"https://c"}`)
	if _, err := SubjectOf(cfgSvc.Snapshot(), read); err == nil {
		t.Fatal("an incomplete account has a subject")
	}
}

func TestAdoptionStatus_AS6_NoSubjectOnTheWire(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	m.SetAdoptionStatus(statusOf(t, cfgSvc, read, "unreachable", "Not yet: the server could not be reached (retrying)."))
	raw, err := json.Marshal(homeView(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"adoption":{"state":"unreachable"`) {
		t.Fatalf("no adoption on the wire: %s", raw)
	}
	if strings.Contains(string(raw), accountOf(t, homeStation)) || strings.Contains(string(raw), "Subject") {
		t.Fatalf("the subject reached the wire: %s", raw)
	}
}
