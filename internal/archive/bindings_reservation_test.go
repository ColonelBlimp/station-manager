package archive

import (
	"context"
	"encoding/json"
	stderr "errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.3 commit 4a (ADR 0091): the durable reservation of an adopted
   cloud name, and its enforcement on every bindings save.

     RS1  ReserveAdoption pins the binding the attempt read (same logbook,
          enabled, unadopted, same credentials) BEFORE judging: a binding
          changed since the read is reported changed and the judge never runs.
          An unsafe verdict or a judge error writes nothing. A safe verdict
          reserves; reserving again keeps the first timestamp; a failed write
          is an error and reserves nothing.
     RS2  the reservation is not adoption: remote_adopted_at stays unset and
          the restart fingerprint does not change.
     RS3  a reserved binding's adoption key is locked like an adopted one's:
          listed in locked_fields and refused on the PUT.
     RS4  a save that would give ANY other SM Cloud binding a protected name
          (reserved or adopted; disabled ones, deleted logbooks' and
          cleared-to-"main" included; normalized as the type does) is refused
          binding_name_reserved and writes nothing. Other names save.
     RS5  the judgement and the reservation are serialized with a bindings
          PUT: a competing rename held at its write cannot slip between them.
*/

func stampReserved(t *testing.T, db *sqlite.Service, logbookID int64) {
	t.Helper()
	tx, cancel, err := db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err := tx.ExecContext(context.Background(),
		`UPDATE logbook_destination SET adoption_reserved_at = datetime('now') WHERE logbook_id = ? AND destination = 'smcloud'`, logbookID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func safeJudge(context.Context) (string, error) { return "", nil }

// countingJudge reports a verdict and counts its calls.
type countingJudge struct {
	reason string
	err    error
	calls  int
}

func (j *countingJudge) judge(context.Context) (string, error) {
	j.calls++
	return j.reason, j.err
}

func TestReservation_RS1_ReserveAdoptionPinsJudgesThenReserves(t *testing.T) {
	ctx := context.Background()
	t.Run("a safe verdict reserves the binding the attempt read", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		j := &countingJudge{}
		out, reason, err := m.ReserveAdoption(ctx, homePin(t, read), j.judge)
		if err != nil || out != ReserveReserved || reason != "" || j.calls != 1 {
			t.Fatalf("ReserveAdoption = %q, %q, %v (judge calls %d); want reserved after one judgement", out, reason, err, j.calls)
		}
		r, _ := smcloudRow(t, db, a)
		if r.AdoptionReservedAt == nil || time.Since(*r.AdoptionReservedAt) > time.Minute || string(r.Credentials) != `{"logbook":"shack"}` {
			t.Fatalf("after reserving: %+v", r)
		}
	})
	t.Run("reserving again keeps the first reservation", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = '2026-01-02 03:04:05' WHERE logbook_id = ?`, a)
		read, _ = smcloudRow(t, db, a)
		if out, _, err := m.ReserveAdoption(ctx, homePin(t, read), safeJudge); err != nil || out != ReserveReserved {
			t.Fatalf("ReserveAdoption again = %q, %v; want reserved", out, err)
		}
		r, _ := smcloudRow(t, db, a)
		if r.AdoptionReservedAt == nil || r.AdoptionReservedAt.Year() != 2026 || r.AdoptionReservedAt.Month() != 1 {
			t.Fatalf("the reservation time moved: %v", r.AdoptionReservedAt)
		}
	})
	t.Run("an unsafe verdict writes nothing", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		j := &countingJudge{reason: "binding x shares the cloud name"}
		out, reason, err := m.ReserveAdoption(ctx, homePin(t, read), j.judge)
		if err != nil || out != ReserveUnsafe || reason != j.reason {
			t.Fatalf("ReserveAdoption = %q, %q, %v; want unsafe with the judge's reason", out, reason, err)
		}
		if r, _ := smcloudRow(t, db, a); r.AdoptionReservedAt != nil {
			t.Fatalf("an unsafe verdict reserved: %+v", r)
		}
	})
	t.Run("a judge error writes nothing", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		j := &countingJudge{err: stderr.New("manifest unreadable")}
		if out, _, err := m.ReserveAdoption(ctx, homePin(t, read), j.judge); err == nil || out == ReserveReserved {
			t.Fatalf("ReserveAdoption = %q, %v; want an error", out, err)
		}
		if r, _ := smcloudRow(t, db, a); r.AdoptionReservedAt != nil {
			t.Fatalf("a judge error reserved: %+v", r)
		}
	})
	t.Run("a failed reservation write is an error", func(t *testing.T) {
		db, a, _, read := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(failingReserveDB{db}, nil)
		if out, _, err := m.ReserveAdoption(ctx, homePin(t, read), safeJudge); err == nil || out == ReserveReserved {
			t.Fatalf("ReserveAdoption = %q, %v; want an error", out, err)
		}
	})
	for name, change := range map[string]func(t *testing.T, db *sqlite.Service, a int64, read *types.LogbookDestination){
		"the name changed since the read": func(t *testing.T, db *sqlite.Service, a int64, _ *types.LogbookDestination) {
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: true, Credentials: map[string]string{"logbook": "portable"}}); err != nil {
				t.Fatal(err)
			}
		},
		"the binding was disabled since the read": func(t *testing.T, db *sqlite.Service, a int64, _ *types.LogbookDestination) {
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: false}); err != nil {
				t.Fatal(err)
			}
		},
		"the binding is already confirmed under this account": func(t *testing.T, db *sqlite.Service, a int64, _ *types.LogbookDestination) {
			stampConfirmed(t, db, a, accountOf(t, homeStation))
		},
		"the read names another logbook": func(t *testing.T, _ *sqlite.Service, _ int64, read *types.LogbookDestination) {
			read.LogbookID++
		},
		"an unknown binding name": func(t *testing.T, _ *sqlite.Service, _ int64, read *types.LogbookDestination) {
			read.ForwarderName = "smcloud.gone"
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, a, _, read := unadoptedHome(t)
			change(t, db, a, &read)
			m := homeManager(t, db, a)
			m.SetActiveBindings(db, nil)
			j := &countingJudge{}
			out, _, err := m.ReserveAdoption(ctx, homePin(t, read), j.judge)
			if err != nil || out != ReserveChanged {
				t.Fatalf("ReserveAdoption = %q, %v; want changed", out, err)
			}
			if j.calls != 0 {
				t.Fatalf("the judge ran %d time(s) on a binding that changed since the read", j.calls)
			}
			if r, _ := smcloudRow(t, db, a); r.AdoptionReservedAt != nil {
				t.Fatalf("a changed binding was reserved: %+v", r)
			}
		})
	}
	t.Run("a destination without an adoption key", func(t *testing.T) {
		db, a, _, _ := unadoptedHome(t)
		m := homeManager(t, db, a)
		m.SetActiveBindings(db, nil)
		qrz := types.LogbookDestination{LogbookID: a, Destination: "bindview-qrz", ForwarderName: "qrz", Enabled: true}
		if out, _, err := m.ReserveAdoption(ctx, homePin(t, qrz), safeJudge); err == nil || out == ReserveReserved {
			t.Fatalf("ReserveAdoption(qrz) = %q, %v; want an error", out, err)
		}
	})
	t.Run("no active archive database", func(t *testing.T) {
		_, _, _, read := unadoptedHome(t)
		m, _, _ := testManager(t)
		if out, _, err := m.ReserveAdoption(ctx, homePin(t, read), safeJudge); bindingCode(err) != "bindings_unavailable" || out == ReserveReserved {
			t.Fatalf("ReserveAdoption = %q, %v; want bindings_unavailable", out, err)
		}
	})
}

func TestReservation_RS2_AReservationIsNotAdoption(t *testing.T) {
	ctx := context.Background()
	db, a, _, read := unadoptedHome(t)
	atStart, err := db.ListLogbookDestinationsWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := homeManager(t, db, a)
	m.SetActiveBindings(db, atStart)
	if out, _, err := m.ReserveAdoption(ctx, homePin(t, read), safeJudge); err != nil || out != ReserveReserved {
		t.Fatalf("ReserveAdoption = %q, %v", out, err)
	}
	r, _ := smcloudRow(t, db, a)
	if r.RemoteAdoptedAt != nil {
		t.Fatalf("a reservation set remote_adopted_at: %+v", r)
	}
	v, err := m.Bindings(ctx, homeID)
	if err != nil || v.RestartRequired {
		t.Fatalf("after reserving: restart_required=%v err=%v; want false (a reservation switches no wire)", v.RestartRequired, err)
	}
}

func TestReservation_RS3_AReservedBindingsKeyIsLocked(t *testing.T) {
	ctx := context.Background()
	db, a, _, _ := unadoptedHome(t)
	stampReserved(t, db, a)
	v, err := BindingsView(ctx, db, homeSmcloudCfg(a), homeArchive, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := smcloudViewRow(t, v, a).LockedFields; !slices.Equal(got, []string{"logbook"}) {
		t.Fatalf("reserved row locked_fields = %v; want [logbook]", got)
	}
	for name, edit := range map[string]types.LogbookBindingEdit{
		"typed new name": {Enabled: true, Credentials: map[string]string{"logbook": "portable"}},
		"cleared":        {Enabled: false, CredentialsClear: []string{"logbook"}},
	} {
		t.Run(name, func(t *testing.T) {
			edit.LogbookID = a
			if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), edit); bindingCode(err) != "binding_field_locked" {
				t.Fatalf("PUT = %v; want binding_field_locked", err)
			}
			if r, _ := smcloudRow(t, db, a); string(r.Credentials) != `{"logbook":"shack"}` {
				t.Fatalf("a refused PUT changed the reserved row: %+v", r)
			}
		})
	}
}

// protectedHome: Home's default binding "cloud" (logbook a) under "shack",
// reserved; logbook b holds a disabled SM Cloud binding under "portable".
func protectedHome(t *testing.T) (*sqlite.Service, int64, int64) {
	t.Helper()
	db, a, b, _ := unadoptedHome(t)
	if err := db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{{
		LogbookID: b, Destination: "smcloud", ForwarderName: "smcloud.second", Enabled: false, Credentials: json.RawMessage(`{"logbook":"portable"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	stampReserved(t, db, a)
	return db, a, b
}

func TestReservation_RS4_NoOtherBindingMayTakeAProtectedName(t *testing.T) {
	ctx := context.Background()
	refused := map[string]struct {
		setup func(t *testing.T, db *sqlite.Service, a int64)
		edit  types.LogbookBindingEdit
	}{
		"renamed onto the reserved name": {
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "shack"}},
		},
		"renamed onto it with padding": {
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "  shack "}},
		},
		"protected by an adoption, not a reservation": {
			setup: func(t *testing.T, db *sqlite.Service, a int64) {
				execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = NULL WHERE logbook_id = ?`, a)
				stampAdopted(t, db, a)
			},
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "shack"}},
		},
		"protected by a disabled binding": {
			setup: func(t *testing.T, db *sqlite.Service, a int64) {
				if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: a, Enabled: false}); err != nil {
					t.Fatal(err)
				}
			},
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "shack"}},
		},
		"protected by a deleted logbook's binding": {
			setup: func(t *testing.T, db *sqlite.Service, a int64) {
				moveReservationToDeletedLogbook(t, db, a)
			},
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "shack"}},
		},
		"cleared back to main while main is protected": {
			setup: func(t *testing.T, db *sqlite.Service, a int64) {
				execArchive(t, db, `UPDATE logbook_destination SET credentials = NULL WHERE logbook_id = ?`, a)
			},
			edit: types.LogbookBindingEdit{CredentialsClear: []string{"logbook"}},
		},
		"typed main while a blank name is protected": {
			setup: func(t *testing.T, db *sqlite.Service, a int64) {
				execArchive(t, db, `UPDATE logbook_destination SET credentials = '{"logbook":"  "}' WHERE logbook_id = ?`, a)
			},
			edit: types.LogbookBindingEdit{Credentials: map[string]string{"logbook": "main"}},
		},
	}
	for name, c := range refused {
		t.Run("refused: "+name, func(t *testing.T) {
			db, a, b := protectedHome(t)
			if c.setup != nil {
				c.setup(t, db, a)
			}
			before, _ := smcloudRow(t, db, b)
			edit := c.edit
			edit.LogbookID = b
			_, err := putSmcloud(ctx, db, homeSmcloudCfg(a), edit)
			if bindingCode(err) != "binding_name_reserved" {
				t.Fatalf("PUT = %v; want binding_name_reserved", err)
			}
			if after, _ := smcloudRow(t, db, b); string(after.Credentials) != string(before.Credentials) {
				t.Fatalf("a refused PUT wrote: before %s after %s", before.Credentials, after.Credentials)
			}
		})
	}
	t.Run("refused: a new binding under the name, and nothing else in the PUT is written", func(t *testing.T) {
		db, a, b := protectedHome(t)
		c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Third", Callsign: "M0QQQ"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = putSmcloud(ctx, db, homeSmcloudCfg(a),
			types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "renamed"}},
			types.LogbookBindingEdit{LogbookID: c, Credentials: map[string]string{"logbook": "shack"}})
		if bindingCode(err) != "binding_name_reserved" {
			t.Fatalf("PUT = %v; want binding_name_reserved", err)
		}
		if r, _ := smcloudRow(t, db, b); string(r.Credentials) != `{"logbook":"portable"}` {
			t.Fatalf("the refused PUT wrote its other row: %s", r.Credentials)
		}
		if _, ok := smcloudRow(t, db, c); ok {
			t.Fatal("the refused PUT created the new binding")
		}
	})
	t.Run("refused: another binding's stored name cannot be read", func(t *testing.T) {
		db, a, b := protectedHome(t)
		c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Third", Callsign: "M0QQQ"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{{
			LogbookID: c, Destination: "smcloud", ForwarderName: "smcloud.third", Credentials: json.RawMessage(`"not an object"`),
		}}); err != nil {
			t.Fatal(err)
		}
		_, err = putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "renamed"}})
		if bindingCode(err) != "binding_credentials_corrupt" {
			t.Fatalf("PUT = %v; want binding_credentials_corrupt (it may share the protected name)", err)
		}
		if r, _ := smcloudRow(t, db, b); string(r.Credentials) != `{"logbook":"portable"}` {
			t.Fatalf("the refused PUT wrote: %s", r.Credentials)
		}
	})
	t.Run("refused: a protected name cannot be read", func(t *testing.T) {
		db, a, b := protectedHome(t)
		execArchive(t, db, `UPDATE logbook_destination SET credentials = '"not an object"' WHERE logbook_id = ?`, a)
		_, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "renamed"}})
		if err == nil {
			t.Fatal("PUT saved while a protected name could not be read")
		}
		if r, _ := smcloudRow(t, db, b); string(r.Credentials) != `{"logbook":"portable"}` {
			t.Fatalf("the refused PUT wrote: %s", r.Credentials)
		}
	})
	t.Run("refused message does not echo the name", func(t *testing.T) {
		db, a, b := protectedHome(t)
		_, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "shack"}})
		var re *RequestError
		if !stderr.As(err, &re) || re.Code != "binding_name_reserved" {
			t.Fatalf("PUT = %v", err)
		}
		if containsFold(re.Message, "shack") {
			t.Fatalf("the refusal echoes a credential value: %q", re.Message)
		}
	})
	t.Run("other names save, and the owner keeps its own name", func(t *testing.T) {
		db, a, b := protectedHome(t)
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a),
			types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "shack2"}},
			types.LogbookBindingEdit{LogbookID: a, Enabled: true}); err != nil {
			t.Fatalf("PUT: %v", err)
		}
		if r, _ := smcloudRow(t, db, b); string(r.Credentials) != `{"logbook":"shack2"}` {
			t.Fatalf("rename: %s", r.Credentials)
		}
	})
	t.Run("without a protected name the same rename saves", func(t *testing.T) {
		db, a, b := protectedHome(t)
		execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = NULL WHERE logbook_id = ?`, a)
		if _, err := putSmcloud(ctx, db, homeSmcloudCfg(a), types.LogbookBindingEdit{LogbookID: b, Credentials: map[string]string{"logbook": "shack"}}); err != nil {
			t.Fatalf("PUT: %v", err)
		}
	})
}

// RS5: a PUT renaming the OTHER binding onto the default's name is held at its
// write; the reservation is then attempted with a judge that refuses when any
// other binding shares the name. Unserialized, the judge reads before the
// rename lands, finds nothing, and reserves; the held rename then gives the
// protected name to a second binding. Serialized, the judge runs after the
// rename and refuses.
func TestReservation_RS5_TheJudgementAndTheReservationAreSerializedWithAPUT(t *testing.T) {
	ctx := context.Background()
	db, a, b := protectedHome(t)
	execArchive(t, db, `UPDATE logbook_destination SET adoption_reserved_at = NULL WHERE logbook_id = ?`, a)
	read, _ := smcloudRow(t, db, a)
	m := homeManager(t, db, a)
	g := &gatedDB{Service: db, arrived: make(chan struct{}), release: make(chan struct{})}
	m.SetActiveBindings(g, nil)
	putErr := make(chan error, 1)
	go func() {
		_, err := m.ApplyBindings(ctx, homeID, types.ArchiveBindingsRequest{Destinations: []types.DestinationBindingEdit{{
			Type: "smcloud", Logbooks: []types.LogbookBindingEdit{{LogbookID: b, Credentials: map[string]string{"logbook": "shack"}}}}}})
		putErr <- err
	}()
	within(t, g.arrived, "the rename at its write")
	judge := func(ctx context.Context) (string, error) {
		r, _ := smcloudRow(t, db, b)
		if string(r.Credentials) == `{"logbook":"shack"}` {
			return "binding smcloud.second shares the cloud name", nil
		}
		return "", nil
	}
	type res struct {
		out ReserveOutcome
		err error
	}
	done := make(chan res, 1)
	go func() {
		out, _, err := m.ReserveAdoption(ctx, homePin(t, read), judge)
		done <- res{out, err}
	}()
	select {
	case r := <-done:
		done <- r
	case <-time.After(300 * time.Millisecond):
	}
	g.release <- struct{}{}
	putRes := <-putErr
	var r res
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ReserveAdoption")
	}
	other, _ := smcloudRow(t, db, b)
	owner, _ := smcloudRow(t, db, a)
	if owner.AdoptionReservedAt != nil && string(other.Credentials) == `{"logbook":"shack"}` {
		t.Fatalf("the reserved name was given to a second binding (reserve %q, rename %v)", r.out, putRes)
	}
	if r.err != nil || r.out != ReserveUnsafe || putRes != nil {
		t.Fatalf("reserve = %q, %v; rename = %v; want the rename saved and the reservation judged unsafe", r.out, r.err, putRes)
	}
}

// failingReserveDB fails the reservation write.
type failingReserveDB struct{ *sqlite.Service }

func (failingReserveDB) ReserveLogbookDestinationAdoptionWithContext(context.Context, string, int64, json.RawMessage, string) (bool, error) {
	return false, stderr.New("disk full")
}

func execArchive(t *testing.T, db *sqlite.Service, q string, args ...any) {
	t.Helper()
	tx, cancel, err := db.BeginTxContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err := tx.ExecContext(context.Background(), q, args...); err != nil {
		_ = tx.Rollback()
		t.Fatalf("%s: %v", q, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// moveReservationToDeletedLogbook moves the reserved "shack" binding from the
// default logbook a onto a third logbook, then deletes that logbook. The
// default keeps an unreserved binding under another name.
func moveReservationToDeletedLogbook(t *testing.T, db *sqlite.Service, a int64) {
	t.Helper()
	ctx := context.Background()
	c, err := db.InsertLogbookWithContext(ctx, types.Logbook{Name: "Gone", Callsign: "M0GON"})
	if err != nil {
		t.Fatal(err)
	}
	execArchive(t, db, `UPDATE logbook_destination SET logbook_id = ? WHERE logbook_id = ?`, c, a)
	if err := db.DeleteLogbookByIDWithContext(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertLogbookDestinationsWithContext(ctx, []sqlite.DestinationUpsert{{
		LogbookID: a, Destination: "smcloud", ForwarderName: "cloud.new", Enabled: true, Credentials: json.RawMessage(`{"logbook":"fresh"}`),
	}}); err != nil {
		t.Fatal(err)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
