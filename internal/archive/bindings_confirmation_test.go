package archive

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4b1 (ADR 0091, "Which account a confirmation belongs
   to"): a confirmation is recorded with the fingerprint of the station account
   it was made under, and counts only for that account.

     CF1  reserve then record under the pinned account: the confirmation and
          its account are written together and confirm that account only.
     CF2  the pin is rechecked under the locks before the reservation AND the
          confirmation: another active archive, a moved default or a changed
          account abandons the step; the judge does not run; nothing is written.
     CF3  an account replacement leaves the confirmation unmatched; the binding
          is reserved again (judged again) and re-confirmed under the new one.
     CF4  a token rotation does the same; a trailing slash on the URL does not.
     CF5  an offline config.json edit, read at the next start, does the same.
     CF6  an account save racing with the confirmation, in both orderings: the
          save first discards the old completion; the confirmation first holds
          the save until it commits, and the save then leaves it unmatched.
     CF7  the account joins the restart fingerprint: a re-confirmation under a
          new account raises restart_required.
     CF8  the fingerprint never reaches the wire.
*/

const homeStation = `{"url":"https://c","token":"t"}`

func accountOf(t *testing.T, station string) string {
	t.Helper()
	fp, err := smcloud.AccountFingerprint(homeID, json.RawMessage(station))
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// homePin pins an attempt on Home under the station account homeManager saves.
func homePin(t *testing.T, read types.LogbookDestination) AdoptionPin {
	t.Helper()
	return AdoptionPin{ArchiveID: homeID, Binding: read, Account: accountOf(t, homeStation)}
}

func stampConfirmed(t *testing.T, db *sqlite.Service, logbookID int64, account string) {
	t.Helper()
	execArchive(t, db, `UPDATE logbook_destination SET remote_adopted_at = datetime('now'), remote_adopted_account = ?
		WHERE logbook_id = ? AND destination = 'smcloud'`, account, logbookID)
}

func setStation(t *testing.T, cfgSvc *config.Service, station string) {
	t.Helper()
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		for i := range c.Forwarders {
			if c.Forwarders[i].Type == "smcloud" {
				c.Forwarders[i].Credentials = json.RawMessage(station)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func reserveAndRecord(t *testing.T, m *Manager, pin AdoptionPin) {
	t.Helper()
	ctx := context.Background()
	if out, reason, err := m.ReserveAdoption(ctx, pin, safeJudge); err != nil || out != ReserveReserved {
		t.Fatalf("ReserveAdoption = %q, %q, %v; want reserved", out, reason, err)
	}
	if ok, err := m.RecordAdoption(ctx, pin); err != nil || !ok {
		t.Fatalf("RecordAdoption = %v, %v; want recorded", ok, err)
	}
}

func currentHomeAccount(t *testing.T, cfgSvc *config.Service) string {
	t.Helper()
	fp, err := CurrentAccount(cfgSvc.Snapshot(), "smcloud", homeID)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func TestConfirmation_CF1_RecordedWithItsAccount(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	if got, want := currentHomeAccount(t, cfgSvc), accountOf(t, homeStation); got != want {
		t.Fatalf("CurrentAccount = %q; want the saved account's fingerprint %q", got, want)
	}
	reserveAndRecord(t, m, homePin(t, read))
	r, _ := smcloudRow(t, db, a)
	if r.RemoteAdoptedAt == nil || r.RemoteAdoptedAccount != accountOf(t, homeStation) {
		t.Fatalf("after recording: adopted %v under %q", r.RemoteAdoptedAt, r.RemoteAdoptedAccount)
	}
	if !AdoptionConfirmed(r, accountOf(t, homeStation)) {
		t.Fatal("not confirmed for the account it was recorded under")
	}
	if AdoptionConfirmed(r, accountOf(t, `{"url":"https://c","token":"other"}`)) {
		t.Fatal("confirmed for another account")
	}
	r.RemoteAdoptedAt = nil
	if AdoptionConfirmed(r, accountOf(t, homeStation)) {
		t.Fatal("confirmed without remote_adopted_at")
	}
	if AdoptionConfirmed(types.LogbookDestination{RemoteAdoptedAt: &time.Time{}}, "") {
		t.Fatal("an empty account confirms a binding adopted without one")
	}
}

func TestConfirmation_CF2_ThePinIsRecheckedBeforeEachWrite(t *testing.T) {
	ctx := context.Background()
	for name, change := range map[string]func(c *config.Config){
		"another archive is active": func(c *config.Config) {
			const drill = "019fd5c5-efcc-7193-be4f-1fee532ee3a9"
			c.QsoArchives = append(c.QsoArchives, types.QsoArchiveConfig{ID: drill, Label: "Drill", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/x/drill.db"})
			c.ActiveQsoArchiveID = drill
		},
		"the default moved": func(c *config.Config) { c.DefaultLogbookID++ },
		"the account changed": func(c *config.Config) {
			c.Forwarders[0].Credentials = json.RawMessage(`{"url":"https://c","token":"rotated"}`)
		},
		"the account is incomplete": func(c *config.Config) { c.Forwarders[0].Credentials = json.RawMessage(`{"url":"https://c"}`) },
	} {
		t.Run(name+": the reservation", func(t *testing.T) {
			db, a, _, read := unadoptedHome(t)
			m, cfgSvc := homeManagerCfg(t, db, a)
			m.SetActiveBindings(db, nil)
			pin := homePin(t, read)
			if _, err := cfgSvc.Update(func(c *config.Config) error { change(c); return nil }); err != nil {
				t.Fatal(err)
			}
			j := &countingJudge{}
			out, _, err := m.ReserveAdoption(ctx, pin, j.judge)
			if err != nil || out != ReserveChanged || j.calls != 0 {
				t.Fatalf("ReserveAdoption = %q, %v (judge calls %d); want changed without judging", out, err, j.calls)
			}
			if r, _ := smcloudRow(t, db, a); r.AdoptionReservedAt != nil {
				t.Fatalf("reserved after the pin changed: %+v", r)
			}
		})
		t.Run(name+": the confirmation", func(t *testing.T) {
			db, a, _, read := unadoptedHome(t)
			m, cfgSvc := homeManagerCfg(t, db, a)
			m.SetActiveBindings(db, nil)
			pin := homePin(t, read)
			if out, _, err := m.ReserveAdoption(ctx, pin, safeJudge); err != nil || out != ReserveReserved {
				t.Fatalf("ReserveAdoption = %q, %v", out, err)
			}
			if _, err := cfgSvc.Update(func(c *config.Config) error { change(c); return nil }); err != nil {
				t.Fatal(err)
			}
			if ok, err := m.RecordAdoption(ctx, pin); err != nil || ok {
				t.Fatalf("RecordAdoption = %v, %v; want the completion discarded", ok, err)
			}
			r, _ := smcloudRow(t, db, a)
			if r.RemoteAdoptedAt != nil || r.AdoptionReservedAt == nil {
				t.Fatalf("after a discarded completion: %+v; want unconfirmed and still reserved", r)
			}
		})
	}
}

// reconfirmAfter confirms Home under homeStation, applies change to the saved
// account, and then proves the confirmation no longer counts, that the
// binding can be judged and reserved again, and that it re-confirms under the
// new account.
func reconfirmAfter(t *testing.T, newStation string, change func(t *testing.T, m *Manager, cfgSvc *config.Service) (*Manager, *config.Service)) {
	t.Helper()
	ctx := context.Background()
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	m, cfgSvc = change(t, m, cfgSvc)
	current := currentHomeAccount(t, cfgSvc)
	if current != accountOf(t, newStation) {
		t.Fatalf("CurrentAccount = %q; want the new account's", current)
	}
	confirmed, _ := smcloudRow(t, db, a)
	if AdoptionConfirmed(confirmed, current) {
		t.Fatal("the old account's confirmation counts for the new account")
	}
	pin := AdoptionPin{ArchiveID: homeID, Binding: confirmed, Account: current}
	j := &countingJudge{}
	if out, _, err := m.ReserveAdoption(ctx, pin, j.judge); err != nil || out != ReserveReserved || j.calls != 1 {
		t.Fatalf("ReserveAdoption under the new account = %q, %v (judge calls %d); want reserved after judging", out, err, j.calls)
	}
	if ok, err := m.RecordAdoption(ctx, pin); err != nil || !ok {
		t.Fatalf("RecordAdoption under the new account = %v, %v", ok, err)
	}
	r, _ := smcloudRow(t, db, a)
	if !AdoptionConfirmed(r, current) || r.RemoteAdoptedAccount != current {
		t.Fatalf("after re-confirming: under %q; want the new account", r.RemoteAdoptedAccount)
	}
	if out, _, err := m.ReserveAdoption(ctx, AdoptionPin{ArchiveID: homeID, Binding: r, Account: current}, safeJudge); err != nil || out != ReserveChanged {
		t.Fatalf("ReserveAdoption once confirmed = %q, %v; want changed (nothing to confirm)", out, err)
	}
}

func TestConfirmation_CF3_AnAccountReplacementNeedsAFreshConfirmation(t *testing.T) {
	const b = `{"url":"https://other.example","token":"b-token"}`
	reconfirmAfter(t, b, func(t *testing.T, m *Manager, cfgSvc *config.Service) (*Manager, *config.Service) {
		setStation(t, cfgSvc, b)
		return m, cfgSvc
	})
}

func TestConfirmation_CF4_ATokenRotationNeedsAFreshConfirmation(t *testing.T) {
	const rotated = `{"url":"https://c","token":"t2"}`
	reconfirmAfter(t, rotated, func(t *testing.T, m *Manager, cfgSvc *config.Service) (*Manager, *config.Service) {
		setStation(t, cfgSvc, rotated)
		return m, cfgSvc
	})
}

func TestConfirmation_CF4_ATrailingSlashIsTheSameAccount(t *testing.T) {
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	setStation(t, cfgSvc, `{"url":"https://c/","token":"t"}`)
	if r, _ := smcloudRow(t, db, a); !AdoptionConfirmed(r, currentHomeAccount(t, cfgSvc)) {
		t.Fatal("a trailing slash unmatched the confirmation")
	}
}

// CF5: the daemon is stopped, config.json is edited by hand, and the next start
// loads it. The new process reads the edited account and the stored
// confirmation no longer counts.
func TestConfirmation_CF5_AnOfflineConfigEditNeedsAFreshConfirmation(t *testing.T) {
	const edited = `{"url":"https://c","token":"edited-offline"}`
	reconfirmAfter(t, edited, func(t *testing.T, m *Manager, cfgSvc *config.Service) (*Manager, *config.Service) {
		path := cfgSvc.Path
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for _, f := range doc["forwarders"].([]any) {
			fw := f.(map[string]any)
			if fw["type"] == "smcloud" {
				fw["credentials"] = json.RawMessage(edited)
			}
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		next := config.New(loaded)
		next.SetPath(path)
		if err := next.Initialize(); err != nil {
			t.Fatal(err)
		}
		restarted := NewManager(next, m.logger, m.validCallsign)
		m.mu.Lock()
		db := m.activeDB
		m.mu.Unlock()
		restarted.SetActiveBindings(db, nil)
		return restarted, next
	})
}

// confirmGatedDB holds the confirmation write until released.
type confirmGatedDB struct {
	*sqlite.Service
	arrived chan struct{}
	release chan struct{}
}

func (g *confirmGatedDB) RecordLogbookDestinationAdoptedWithContext(ctx context.Context, name string, logbookID int64, creds json.RawMessage, account string) (bool, error) {
	g.arrived <- struct{}{}
	<-g.release
	return g.Service.RecordLogbookDestinationAdoptedWithContext(ctx, name, logbookID, creds, account)
}

func TestConfirmation_CF6_AnAccountSaveRacingTheConfirmation(t *testing.T) {
	ctx := context.Background()
	const b = `{"url":"https://other.example","token":"b-token"}`
	t.Run("the save wins: the old completion is discarded", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		m.SetActiveBindings(db, nil)
		pin := homePin(t, read)
		if out, _, err := m.ReserveAdoption(ctx, pin, safeJudge); err != nil || out != ReserveReserved {
			t.Fatalf("ReserveAdoption = %q, %v", out, err)
		}
		setStation(t, cfgSvc, b) // lands before the completion is recorded
		if ok, err := m.RecordAdoption(ctx, pin); err != nil || ok {
			t.Fatalf("RecordAdoption = %v, %v; want discarded", ok, err)
		}
		r, _ := smcloudRow(t, db, a)
		if r.RemoteAdoptedAt != nil || r.AdoptionReservedAt == nil {
			t.Fatalf("after the discarded completion: %+v; want unconfirmed, still reserved", r)
		}
	})
	t.Run("the confirmation wins: the save waits, then unmatches it", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m, cfgSvc := homeManagerCfg(t, db, a)
		g := &confirmGatedDB{Service: db, arrived: make(chan struct{}), release: make(chan struct{})}
		m.SetActiveBindings(g, nil)
		pin := homePin(t, read)
		if out, _, err := m.ReserveAdoption(ctx, pin, safeJudge); err != nil || out != ReserveReserved {
			t.Fatalf("ReserveAdoption = %q, %v", out, err)
		}
		recorded := make(chan bool, 1)
		go func() {
			ok, err := m.RecordAdoption(ctx, pin)
			if err != nil {
				t.Error(err)
			}
			recorded <- ok
		}()
		within(t, g.arrived, "the confirmation at its write")
		saved := make(chan struct{})
		go func() {
			setStation(t, cfgSvc, b)
			close(saved)
		}()
		select {
		case <-saved:
			t.Fatal("the account save completed while the confirmation was being written")
		case <-time.After(300 * time.Millisecond):
		}
		g.release <- struct{}{}
		if !<-recorded {
			t.Fatal("the confirmation that held the save was not recorded")
		}
		within(t, saved, "the account save after the confirmation")
		r, _ := smcloudRow(t, db, a)
		if r.RemoteAdoptedAccount != accountOf(t, homeStation) {
			t.Fatalf("confirmed under %q; want the account the attempt used", r.RemoteAdoptedAccount)
		}
		if AdoptionConfirmed(r, currentHomeAccount(t, cfgSvc)) {
			t.Fatal("the confirmation counts for the account saved after it")
		}
	})
}

func TestConfirmation_CF7_TheAccountJoinsTheRestartFingerprint(t *testing.T) {
	ctx := context.Background()
	const b = `{"url":"https://other.example","token":"b-token"}`
	db, a, _, read := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	atStart, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.SetActiveBindings(db, atStart)
	setStation(t, cfgSvc, b)
	if v, err := m.Bindings(ctx, homeID); err != nil || v.RestartRequired {
		t.Fatalf("after the account save: restart_required=%v, %v; want false (the binding itself is unchanged)", v.RestartRequired, err)
	}
	confirmed, _ := smcloudRow(t, db, a)
	pin := AdoptionPin{ArchiveID: homeID, Binding: confirmed, Account: accountOf(t, b)}
	reserveAndRecord(t, m, pin)
	if v, err := m.Bindings(ctx, homeID); err != nil || !v.RestartRequired {
		t.Fatalf("after re-confirming under the new account: restart_required=%v, %v; want true", v.RestartRequired, err)
	}
}

func TestConfirmation_CF8_TheFingerprintNeverReachesTheWire(t *testing.T) {
	ctx := context.Background()
	db, a, _, read := unadoptedHome(t)
	m := homeManager(t, db, a)
	m.SetActiveBindings(db, nil)
	reserveAndRecord(t, m, homePin(t, read))
	fp := accountOf(t, homeStation)
	r, _ := smcloudRow(t, db, a)
	if r.RemoteAdoptedAccount != fp {
		t.Fatalf("row account %q; want %q (the check below needs it read back)", r.RemoteAdoptedAccount, fp)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), fp) {
		t.Fatalf("a binding's JSON carries the fingerprint: %s", raw)
	}
	v, err := m.Bindings(ctx, homeID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), fp) {
		t.Fatalf("the bindings view carries the fingerprint: %s", raw)
	}
}
