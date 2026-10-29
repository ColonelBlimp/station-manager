package archive

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 2 (ADR 0097 §5): a claimed Home binding — one holding an
   adoption reservation, or an adoption not confirmed for the current station
   account — keeps its recovery path after the default moves off its logbook.
   The marker, not the default, decides; an unmarked binding's initiation is
   still the default's alone.

     RF1  the reservation: the pin of a claimed former default still holds,
          so its judgement runs and it stays reserved; an unmarked former
          default's pin has moved, and nothing is judged or written.
     RF2  the confirmation: a reserved former default, and one adopted under
          another account, record a confirmation for the current account; a
          changed account still discards it.
     RF3  the view: a claimed former default shows its own status, or "needs
          confirmation"; an unmarked one shows nothing, even with a status;
          two bindings' statuses are shown each on its own row.
     RF4  the enable rule's safeguards hold for a claimed former default
          re-enabled off the default: its adoption key stays locked, its
          protected name stays its own, and conflicting markers still refuse.
*/

type formerDefaultHome struct {
	db     *sqlite.Service
	m      *Manager
	cfgSvc *config.Service
	a, b   int64
}

// movedOffClaimed: unadoptedHome with mark applied to the default's binding
// ("cloud" under "shack" on logbook a), then the default moved to logbook b.
func movedOffClaimed(t *testing.T, mark string) formerDefaultHome {
	t.Helper()
	db, a, b, _ := unadoptedHome(t)
	m, cfgSvc := homeManagerCfg(t, db, a)
	m.SetActiveBindings(db, nil)
	if mark != "" {
		execArchive(t, db, `UPDATE logbook_destination SET `+mark+` WHERE forwarder_name = 'cloud'`)
	}
	if _, err := cfgSvc.Update(func(c *config.Config) error { c.DefaultLogbookID = b; return nil }); err != nil {
		t.Fatal(err)
	}
	return formerDefaultHome{db: db, m: m, cfgSvc: cfgSvc, a: a, b: b}
}

// read is logbook a's binding as an attempt reads it now.
func (h formerDefaultHome) read(t *testing.T) types.LogbookDestination {
	t.Helper()
	r, ok := smcloudRow(t, h.db, h.a)
	if !ok {
		t.Fatal("no binding on logbook a")
	}
	return r
}

const (
	markReserved = `adoption_reserved_at = datetime('now')`
	// markAdoptedElsewhere: adopted under an account that is not the current
	// station account's.
	markAdoptedElsewhere = `adoption_reserved_at = datetime('now'), remote_adopted_at = datetime('now'), remote_adopted_account = 'another-account'`
)

var claimedMarks = map[string]string{"reserved": markReserved, "adopted elsewhere": markAdoptedElsewhere}

func TestFormerDefault_RF1_TheReservation(t *testing.T) {
	ctx := context.Background()
	for name, mark := range claimedMarks {
		t.Run(name+": judged, and stays reserved", func(t *testing.T) {
			h := movedOffClaimed(t, mark)
			j := &countingJudge{}
			out, _, err := h.m.ReserveAdoption(ctx, homePin(t, h.read(t)), j.judge)
			if err != nil || out != ReserveReserved || j.calls != 1 {
				t.Fatalf("ReserveAdoption = %q, %v (judge calls %d); want reserved after one judgement", out, err, j.calls)
			}
		})
	}
	t.Run("unmarked: the pin has moved", func(t *testing.T) {
		h := movedOffClaimed(t, "")
		j := &countingJudge{}
		out, _, err := h.m.ReserveAdoption(ctx, homePin(t, h.read(t)), j.judge)
		if err != nil || out != ReserveChanged || j.calls != 0 {
			t.Fatalf("ReserveAdoption = %q, %v (judge calls %d); want changed without judging", out, err, j.calls)
		}
		if r := h.read(t); r.AdoptionReservedAt != nil {
			t.Fatalf("reserved after the default moved: %+v", r)
		}
	})
}

func TestFormerDefault_RF2_TheConfirmation(t *testing.T) {
	ctx := context.Background()
	for name, mark := range claimedMarks {
		t.Run(name+": recorded for the current account", func(t *testing.T) {
			h := movedOffClaimed(t, mark)
			recorded, err := h.m.RecordAdoption(ctx, homePin(t, h.read(t)))
			if err != nil || !recorded {
				t.Fatalf("RecordAdoption = %v, %v; want recorded", recorded, err)
			}
			if r := h.read(t); !AdoptionConfirmed(r, currentHomeAccount(t, h.cfgSvc)) {
				t.Fatalf("after recording: %+v; want confirmed for the current account", r)
			}
		})
		t.Run(name+": a changed account still discards it", func(t *testing.T) {
			h := movedOffClaimed(t, mark)
			pin := homePin(t, h.read(t))
			setStation(t, h.cfgSvc, `{"url":"https://c","token":"rotated"}`)
			if recorded, err := h.m.RecordAdoption(ctx, pin); err != nil || recorded {
				t.Fatalf("RecordAdoption = %v, %v; want the completion discarded", recorded, err)
			}
		})
	}
}

func TestFormerDefault_RF3_TheView(t *testing.T) {
	const state, message = "uncertain", "Adoption outcome uncertain: no confirmation was received from the server. Retrying."
	t.Run("reserved: its own status", func(t *testing.T) {
		h := movedOffClaimed(t, markReserved)
		h.m.SetAdoptionStatus(statusOf(t, h.cfgSvc, h.read(t), state, message))
		wantAdoption(t, adoptionRow(t, homeView(t, h.m), h.a), state, message)
	})
	t.Run("adopted elsewhere: needs confirmation, or its own status", func(t *testing.T) {
		h := movedOffClaimed(t, markAdoptedElsewhere)
		wantAdoption(t, adoptionRow(t, homeView(t, h.m), h.a), AdoptionStateNeedsConfirmation, "Adoption needs confirmation for the current station account.")
		h.m.SetAdoptionStatus(statusOf(t, h.cfgSvc, h.read(t), "confirming", "Confirming the adoption for the current station account."))
		wantAdoption(t, adoptionRow(t, homeView(t, h.m), h.a), "confirming", "Confirming the adoption for the current station account.")
	})
	t.Run("unmarked: nothing, even with a status of its subject", func(t *testing.T) {
		h := movedOffClaimed(t, "")
		h.m.SetAdoptionStatus(statusOf(t, h.cfgSvc, h.read(t), state, message))
		wantAdoption(t, adoptionRow(t, homeView(t, h.m), h.a), "", "")
	})
	t.Run("two bindings, each its own status", func(t *testing.T) {
		h := movedOffClaimed(t, markReserved)
		execArchive(t, h.db, `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, 'smcloud', 'cloud2', 1, '{"logbook":"portable"}')`, h.b)
		second, _ := smcloudRow(t, h.db, h.b)
		first := statusOf(t, h.cfgSvc, h.read(t), state, message)
		other := statusOf(t, h.cfgSvc, second, "unauthorized", "Not adopted: the token was refused.")
		h.m.SetAdoptionStatus(func(forwarder string) AdoptionStatus {
			if s := first(forwarder); s.State != "" {
				return s
			}
			return other(forwarder)
		})
		v := homeView(t, h.m)
		wantAdoption(t, adoptionRow(t, v, h.a), state, message)
		wantAdoption(t, adoptionRow(t, v, h.b), "unauthorized", "Not adopted: the token was refused.")
	})
}

func TestFormerDefault_RF4_SafeguardsOfARecoveredBinding(t *testing.T) {
	// Each starts disabled, so the PUT is a re-enable off the default.
	disabled := func(t *testing.T, mark string) formerDefaultHome {
		t.Helper()
		h := movedOffClaimed(t, mark+`, enabled = 0`)
		startAsStored(t, h.m, h.db)
		return h
	}
	for name, mark := range claimedMarks {
		t.Run(name+": its adoption key stays locked", func(t *testing.T) {
			h := disabled(t, mark)
			_, err := putVia(h.m, homeID, types.LogbookBindingEdit{LogbookID: h.a, Enabled: true, Credentials: map[string]string{"logbook": "elsewhere"}})
			if bindingCode(err) != "binding_field_locked" {
				t.Fatalf("PUT = %v; want binding_field_locked", err)
			}
			if r := h.read(t); r.Enabled || string(r.Credentials) != `{"logbook":"shack"}` {
				t.Fatalf("after the refused PUT: %+v; want unchanged", r)
			}
		})
		t.Run(name+": its name stays its own", func(t *testing.T) {
			h := disabled(t, mark)
			_, err := putVia(h.m, homeID,
				types.LogbookBindingEdit{LogbookID: h.a, Enabled: true},
				types.LogbookBindingEdit{LogbookID: h.b, Enabled: false, Credentials: map[string]string{"logbook": "shack"}})
			if bindingCode(err) != "binding_name_reserved" {
				t.Fatalf("PUT = %v; want binding_name_reserved", err)
			}
			if r := h.read(t); r.Enabled {
				t.Fatalf("after the refused PUT: %+v; want still disabled", r)
			}
		})
	}
	t.Run("conflicting markers still refuse", func(t *testing.T) {
		h := disabled(t, markReserved+`, identity_since = datetime('now')`)
		_, err := putVia(h.m, homeID, types.LogbookBindingEdit{LogbookID: h.a, Enabled: true})
		wantRefused(t, h.db, h.a, err, wantConflict)
	})
}
