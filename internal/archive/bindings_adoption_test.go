package archive

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 2 (ADR 0090, T1 and T6): the adoption marker and the
   locked adoption key.

     AK1  the view lists an adopted binding's adoption key (SM Cloud's cloud
          logbook name) in locked_fields; an unadopted binding lists nothing.
     AK2  a PUT that types or clears the key of an adopted binding is refused
          binding_field_locked and writes nothing; other edits keep the key
          and the marker. An unadopted binding's name still changes.
     AK3  the marker is recorded only on the enabled, unadopted binding whose
          stored credentials are the ones the attempt read: a changed name, a
          disabled or already adopted binding, or an unknown name stamps
          nothing. A destination without an adoption key is refused.
     AK4  the marker joins the restart fingerprint: recording it makes the
          view report restart_required.
     AK5  recording the marker is serialized with a bindings PUT, so a name
          change in flight cannot land on an adopted binding.
*/

const homeID = "019fd5c5-efcc-7193-be4f-1fee532ee3a1"

func stampAdopted(t *testing.T, db *sqlite.Service, logbookID int64) {
	t.Helper()
	tx, cancel, err := db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err := tx.ExecContext(context.Background(),
		`UPDATE logbook_destination SET remote_adopted_at = datetime('now') WHERE logbook_id = ? AND destination = 'smcloud'`, logbookID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// adoptedHome stores Home's default SM Cloud binding, enabled under the cloud
// name "shack", and marks it adopted.
func adoptedHome(t *testing.T) (*sqlite.Service, int64, int64) {
	t.Helper()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	if err := db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{{
		LogbookID: a, Destination: "smcloud", ForwarderName: "cloud", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	stampAdopted(t, db, a)
	return db, a, b
}

func smcloudViewRow(t *testing.T, v types.ArchiveBindingsView, logbookID int64) types.LogbookBindingView {
	t.Helper()
	for _, r := range destView(t, v, "smcloud").Logbooks {
		if r.LogbookID == logbookID {
			return r
		}
	}
	t.Fatalf("no smcloud row for logbook %d", logbookID)
	return types.LogbookBindingView{}
}

func TestAdoption_AK1_ViewLocksTheAdoptionKeyOfAnAdoptedBindingOnly(t *testing.T) {
	db, a, b := adoptedHome(t)
	if err := db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{{
		LogbookID: b, Destination: "smcloud", ForwarderName: "smcloud.second", Enabled: false, Credentials: json.RawMessage(`{"logbook":"other"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	v, err := BindingsView(context.Background(), db, homeSmcloudCfg(a), homeArchive, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := smcloudViewRow(t, v, a).LockedFields; !slices.Equal(got, []string{"logbook"}) {
		t.Fatalf("adopted row locked_fields = %v; want [logbook]", got)
	}
	if got := smcloudViewRow(t, v, b).LockedFields; len(got) != 0 {
		t.Fatalf("unadopted row locked_fields = %v; want none", got)
	}
	raw, err := json.Marshal(smcloudViewRow(t, v, b))
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if _, present := wire["locked_fields"]; present {
		t.Fatalf("an unadopted row put locked_fields on the wire: %s", raw)
	}
}

func TestAdoption_AK2_ThePUTRefusesChangingOrClearingTheAdoptionKey(t *testing.T) {
	ctx := context.Background()
	for name, edit := range map[string]types.LogbookBindingEdit{
		"typed new name":              {Enabled: true, Credentials: map[string]string{"logbook": "elsewhere"}},
		"typed the same name":         {Enabled: true, Credentials: map[string]string{"logbook": "shack"}},
		"cleared on a disabled row":   {Enabled: false, CredentialsClear: []string{"logbook"}},
		"typed while disabling it":    {Enabled: false, Credentials: map[string]string{"logbook": "elsewhere"}},
		"typed with padding (normal)": {Enabled: true, Credentials: map[string]string{"logbook": " shack "}},
	} {
		t.Run(name, func(t *testing.T) {
			db, a, _ := adoptedHome(t)
			before, _ := smcloudRow(t, db, a)
			edit.LogbookID = a
			_, err := putSmcloud(ctx, db, homeSmcloudCfg(a), edit)
			if bindingCode(err) != "binding_field_locked" {
				t.Fatalf("PUT = %v; want binding_field_locked", err)
			}
			after, _ := smcloudRow(t, db, a)
			if string(after.Credentials) != string(before.Credentials) || after.Enabled != before.Enabled || after.RemoteAdoptedAt == nil {
				t.Fatalf("a refused PUT changed the adopted row: before %+v after %+v", before, after)
			}
		})
	}
	t.Run("other edits keep the key and the marker", func(t *testing.T) {
		db, a, _ := adoptedHome(t)
		for _, edit := range []types.LogbookBindingEdit{
			{LogbookID: a, Enabled: false},
			{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": ""}},
			{LogbookID: a, Enabled: true},
		} {
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), edit); err != nil {
				t.Fatalf("PUT %+v: %v", edit, err)
			}
			r, _ := smcloudRow(t, db, a)
			if string(r.Credentials) != `{"logbook":"shack"}` || r.RemoteAdoptedAt == nil || r.Enabled != edit.Enabled {
				t.Fatalf("after %+v: %+v; want the key and the marker kept", edit, r)
			}
		}
	})
	t.Run("an unadopted binding's name still changes", func(t *testing.T) {
		db := bindingsDB(t)
		a, _ := twoLogbooks(t, db)
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": "shack"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": "elsewhere"}}); err != nil {
			t.Fatalf("renaming an unadopted binding: %v", err)
		}
		if r, _ := smcloudRow(t, db, a); string(r.Credentials) != `{"logbook":"elsewhere"}` {
			t.Fatalf("unadopted rename: %+v", r)
		}
	})
}

// homeManager wires a Manager to db as Home, the active archive.
func homeManager(t *testing.T, db BindingsDB, defaultID int64) *Manager {
	t.Helper()
	m, cfgSvc, _ := testManager(t)
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: homeID, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/home.db"}}
		c.ActiveQsoArchiveID = homeID
		c.DefaultLogbookID = defaultID
		c.Forwarders = homeSmcloudCfg(defaultID).Forwarders
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// unadoptedHome stores Home's default SM Cloud binding, enabled under "shack",
// and returns the row as an adoption attempt reads it.
func unadoptedHome(t *testing.T) (*sqlite.Service, int64, int64, types.LogbookDestination) {
	t.Helper()
	db := bindingsDB(t)
	a, b := twoLogbooks(t, db)
	if err := db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{{
		LogbookID: a, Destination: "smcloud", ForwarderName: "cloud", Enabled: true, Credentials: json.RawMessage(`{"logbook":"shack"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	read, _ := smcloudRow(t, db, a)
	return db, a, b, read
}

func TestAdoption_AK3_TheMarkerIsRecordedOnlyOnTheBindingTheAttemptRead(t *testing.T) {
	ctx := context.Background()
	t.Run("recorded", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		recorded, err := m.RecordAdoption(ctx, read)
		if err != nil || !recorded {
			t.Fatalf("RecordAdoption = %v, %v; want recorded", recorded, err)
		}
		r, _ := smcloudRow(t, db, a)
		if r.RemoteAdoptedAt == nil || time.Since(*r.RemoteAdoptedAt) > time.Minute || string(r.Credentials) != `{"logbook":"shack"}` || !r.Enabled {
			t.Fatalf("after recording: %+v", r)
		}
	})
	for name, change := range map[string]func(t *testing.T, db *sqlite.Service, a int64){
		"the name changed since the read": func(t *testing.T, db *sqlite.Service, a int64) {
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": "elsewhere"}}); err != nil {
				t.Fatal(err)
			}
		},
		"the binding was disabled since the read": func(t *testing.T, db *sqlite.Service, a int64) {
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: false}); err != nil {
				t.Fatal(err)
			}
		},
		"the binding is already adopted": func(t *testing.T, db *sqlite.Service, a int64) {
			stampAdopted(t, db, a)
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, a, _, read := unadoptedHome(t)
			change(t, db, a)
			before, _ := smcloudRow(t, db, a)
			m := homeManager(t, db, a)
			m.SetActiveBindings(db, nil)
			recorded, err := m.RecordAdoption(ctx, read)
			if err != nil || recorded {
				t.Fatalf("RecordAdoption = %v, %v; want not recorded, no error", recorded, err)
			}
			after, _ := smcloudRow(t, db, a)
			if (before.RemoteAdoptedAt == nil) != (after.RemoteAdoptedAt == nil) ||
				(before.RemoteAdoptedAt != nil && !before.RemoteAdoptedAt.Equal(*after.RemoteAdoptedAt)) {
				t.Fatalf("the marker changed: before %v after %v", before.RemoteAdoptedAt, after.RemoteAdoptedAt)
			}
		})
	}
	t.Run("an unknown binding name", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		read.ForwarderName = "smcloud.gone"
		if recorded, err := m.RecordAdoption(ctx, read); err != nil || recorded {
			t.Fatalf("RecordAdoption = %v, %v; want not recorded, no error", recorded, err)
		}
		if r, _ := smcloudRow(t, db, a); r.RemoteAdoptedAt != nil {
			t.Fatalf("an unknown name stamped the binding: %+v", r)
		}
	})
	t.Run("a destination without an adoption key", func(t *testing.T) {
		db, a, _, _ := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		qrz := types.LogbookDestination{LogbookID: a, Destination: "bindview-qrz", ForwarderName: "qrz", Enabled: true}
		if recorded, err := m.RecordAdoption(ctx, qrz); err == nil || recorded {
			t.Fatalf("RecordAdoption(qrz) = %v, %v; want an error", recorded, err)
		}
	})
	t.Run("no active archive database", func(t *testing.T) {
		_, a, _, read := unadoptedHome(t)
		m, _, _ := testManager(t)
		if recorded, err := m.RecordAdoption(ctx, read); bindingCode(err) != "bindings_unavailable" || recorded {
			t.Fatalf("RecordAdoption = %v, %v; want bindings_unavailable", recorded, err)
		}
		_ = a
	})
}

func TestAdoption_AK4_TheMarkerJoinsTheRestartFingerprint(t *testing.T) {
	ctx := context.Background()
	db, a, _, read := unadoptedHome(t)
	atStart, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := homeManager(t, db, a)
	m.SetActiveBindings(db, atStart)
	v, err := m.Bindings(ctx, homeID)
	if err != nil || v.RestartRequired {
		t.Fatalf("before recording: restart_required=%v err=%v; want false", v.RestartRequired, err)
	}
	if recorded, err := m.RecordAdoption(ctx, read); err != nil || !recorded {
		t.Fatalf("RecordAdoption = %v, %v", recorded, err)
	}
	v, err = m.Bindings(ctx, homeID)
	if err != nil || !v.RestartRequired {
		t.Fatalf("after recording: restart_required=%v err=%v; want true (the identity worker starts at the next start)", v.RestartRequired, err)
	}
}

// AK5: a PUT renaming the binding is held at its write; the marker is then
// recorded from the PRE-rename read. Unserialized, the stamp lands first (the
// stored credentials still match) and the held rename then writes a new name
// onto an adopted binding. Serialized, the stamp waits for the PUT, finds the
// credentials changed and records nothing. The window only lets the bad
// interleaving happen; the serialized path passes whatever it waits.
func TestAdoption_AK5_RecordingIsSerializedWithABindingsPUT(t *testing.T) {
	ctx := context.Background()
	db, a, _, read := unadoptedHome(t)
	m := homeManager(t, db, a)
	g := &gatedDB{Service: db, arrived: make(chan struct{}), release: make(chan struct{})}
	m.SetActiveBindings(g, nil)
	putErr := make(chan error, 1)
	go func() {
		_, err := m.ApplyBindings(ctx, homeID, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": "elsewhere"}}}}}})
		putErr <- err
	}()
	within(t, g.arrived, "the rename at its write")
	type res struct {
		recorded bool
		err      error
	}
	recDone := make(chan res, 1)
	go func() {
		recorded, err := m.RecordAdoption(ctx, read)
		recDone <- res{recorded, err}
	}()
	select {
	case <-recDone:
		// Finished while the rename was held: not serialized. Fall through;
		// the assertions below name the consequence.
		recDone <- res{}
	case <-time.After(300 * time.Millisecond):
	}
	g.release <- struct{}{}
	if err := <-putErr; err != nil {
		t.Fatalf("rename PUT: %v", err)
	}
	select {
	case <-recDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for RecordAdoption")
	}
	r, _ := smcloudRow(t, db, a)
	if r.RemoteAdoptedAt != nil && string(r.Credentials) != `{"logbook":"shack"}` {
		t.Fatalf("an adopted binding carries a name it was not adopted under: %s", r.Credentials)
	}
}
