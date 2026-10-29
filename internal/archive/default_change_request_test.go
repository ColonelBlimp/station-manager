package archive

import (
	"context"
	stderr "errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0024 slice 3b (ADR 0097 §2–§3): a change of the default requested in a
   running daemon. The real archive manager, real config (its directory's
   permissions drive persistence outcomes: 0300 an uncertain write, 0500 a
   failed one), real SQLite logbooks; the seals and the restart port are fakes
   writing to one ordered log.

   Logbooks: A (the effective default) and B under one callsign, C under
   another, D soft-deleted.

     RQ1  validation refuses, before anything is sealed or written: a stale
          believed default, an unavailable target (missing, soft-deleted), an
          ineligible one (another callsign), the default itself, a record
          already in play (pending, or an unconfirmed tombstone), an activation
          pending, no restart port.
     RQ2  order: FT8 seal → rig seal → live-entry seal → persistence → restart;
          nothing is persisted before the live entry is drained; the seals
          are held after the restart is requested.
     RQ3  each refusal releases every seal taken before it; a busy FT8 run,
          armed TX or tune refuses without being ended.
     RQ4  persistence: a definitive failure refuses with nothing on disk; an
          uncertain save proceeds, reported uncertain.
     RQ5  a failed restart withdraws the change: durable (confirmed),
          uncertain (unconfirmed tombstone) or failed (the selector stays) —
          the effective default, the seals and the record checked together.
     RQ6  coordination: an operation in flight refuses the others (a default
          change, an activation, a logbook deletion), in each ordering; a
          record that survives the operation still refuses a new change after
          the lock is released.
     RQ7  Cancel / Confirm / Retry cleanup: every permitted state, each
          outcome, and every stale request id; a request confirmed by an
          unrelated save before request S begins leaves late actions on R
          refused and S untouched.
     RQ8  status: the effective default and the change in play; a request
          whose response was lost is found by its id.
*/

// opLog is the order the fakes saw.
type opLog struct {
	mu  sync.Mutex
	ops []string
}

func (l *opLog) add(op string) {
	l.mu.Lock()
	l.ops = append(l.ops, op)
	l.mu.Unlock()
}

func (l *opLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ops)
}

// orderSeal is a seal writing to the log. refuse is a busy owner: the seal
// refuses, and its activity (active) carries on untouched.
type orderSeal struct {
	name   string
	log    *opLog
	refuse error
	active bool
	sealed bool
	// onSeal runs inside Seal, before it returns (the live entry's drain).
	onSeal func()
}

func (s *orderSeal) Seal() error {
	s.log.add("seal:" + s.name)
	if s.onSeal != nil {
		s.onSeal()
	}
	if s.refuse != nil {
		return s.refuse
	}
	s.sealed = true
	return nil
}

func (s *orderSeal) Release() { s.log.add("release:" + s.name); s.sealed = false }

type changeHarness struct {
	m             *Manager
	cfgSvc        *config.Service
	db            *sqlite.Service
	log           *opLog
	tx, rig, live *orderSeal
	a, b, c, d    int64
	uuid          map[int64]string
	archive       string
	// restart is the port's behaviour; restarts counts its calls.
	restart  func() error
	restarts int
}

func newChangeHarness(t *testing.T) *changeHarness {
	t.Helper()
	ctx := context.Background()
	m, cfgSvc, _ := testManager(t)
	db := bindingsDB(t)
	h := &changeHarness{m: m, cfgSvc: cfgSvc, db: db, log: &opLog{}, uuid: map[int64]string{}}
	var err error
	for _, lb := range []struct {
		dst        *int64
		name, call string
	}{{&h.a, "Main", "M0ABC"}, {&h.b, "Contest", "M0ABC"}, {&h.c, "Other", "M0XYZ"}, {&h.d, "Gone", "M0ABC"}} {
		if *lb.dst, err = db.InsertLogbookWithContext(ctx, types.Logbook{Name: lb.name, Callsign: lb.call}); err != nil {
			t.Fatal(err)
		}
	}
	identity, err := db.EnsureArchiveIdentityWithContext(ctx, h.a)
	if err != nil {
		t.Fatal(err)
	}
	h.archive = identity.ArchiveUUID
	// Every UUID is captured while all four are live, D's included: a request
	// naming D then names a real logbook that is soft-deleted, not an unknown.
	books, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, lb := range books {
		h.uuid[lb.ID] = lb.UUID
	}
	if h.uuid[h.d] == "" {
		t.Fatal("D has no UUID")
	}
	if err := db.DeleteLogbookByIDWithContext(ctx, h.d); err != nil {
		t.Fatal(err)
	}
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: h.archive, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: c.Datastore.Path}}
		c.ActiveQsoArchiveID = h.archive
		c.DefaultLogbookID = h.a
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.SetActiveBindings(db, nil)
	h.tx, h.rig, h.live = &orderSeal{name: "tx", log: h.log}, &orderSeal{name: "rig", log: h.log}, &orderSeal{name: "live", log: h.log}
	h.restart = func() error { return nil }
	m.SetActivation(h.tx, h.rig, func() error {
		h.restarts++
		h.log.add("restart")
		return h.restart()
	})
	m.SetLiveEntrySeal(h.live)
	return h
}

func (h *changeHarness) request(id int64) types.DefaultChangeRequest {
	return types.DefaultChangeRequest{LogbookUUID: h.uuid[id], BelievedDefaultUUID: h.uuid[h.a]}
}

func (h *changeHarness) ask(t *testing.T, req types.DefaultChangeRequest) (types.DefaultChangeAccepted, error) {
	t.Helper()
	return h.m.RequestDefaultChange(context.Background(), req)
}

func (h *changeHarness) disk(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(h.cfgSvc.Path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (h *changeHarness) memory() *config.DefaultLogbookChange {
	return h.cfgSvc.Snapshot().DefaultLogbookChange
}

// configDir runs fn with the config directory at mode.
func (h *changeHarness) configDir(t *testing.T, mode os.FileMode, fn func()) {
	t.Helper()
	dir := filepath.Dir(h.cfgSvc.Path)
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	fn()
}

// setRecord puts rec in config through a write of the given durability: an
// unconfirmed tombstone needs an uncertain write (a durable one confirms it).
func (h *changeHarness) setRecord(t *testing.T, rec *config.DefaultLogbookChange, uncertain bool) {
	t.Helper()
	write := func() {
		if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.DefaultLogbookChange = rec; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if uncertain {
		h.configDir(t, 0o300, write)
		return
	}
	write()
}

func (h *changeHarness) record(id int64, removed string) *config.DefaultLogbookChange {
	return &config.DefaultLogbookChange{RequestID: "019fd5c5-0000-7000-8000-0000000000aa", ArchiveID: h.archive, LogbookUUID: h.uuid[id], Removed: removed}
}

// sealsReleased: every seal taken was released, and none is held.
func (h *changeHarness) sealsReleased(t *testing.T) {
	t.Helper()
	for _, s := range []*orderSeal{h.tx, h.rig, h.live} {
		if s.sealed {
			t.Fatalf("the %s seal is still held; log %v", s.name, h.log.list())
		}
	}
}

// wantChange checks the status's change: its state, failure code and whether
// config still holds it; requestID "" accepts any non-empty id.
func (h *changeHarness) wantChange(t *testing.T, requestID, state, code string, unconfirmed bool) {
	t.Helper()
	st, err := h.m.DefaultChangeStatus(context.Background())
	if err != nil {
		t.Fatalf("status = %v", err)
	}
	c := st.Change
	if c == nil || c.State != state || c.FailureCode != code || c.Unconfirmed != unconfirmed ||
		c.RequestID == "" || (requestID != "" && c.RequestID != requestID) {
		t.Fatalf("status change = %+v; want request %q, state %q, failure %q, unconfirmed %v", c, requestID, state, code, unconfirmed)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("succeeded; want %s", code)
	}
	var re *RequestError
	if !stderr.As(err, &re) || re.Code != code {
		t.Fatalf("err = %v; want %s", err, code)
	}
}

func (h *changeHarness) untouched(t *testing.T) {
	t.Helper()
	if ops := h.log.list(); len(ops) != 0 {
		t.Fatalf("ops = %v; want nothing sealed or restarted", ops)
	}
	if h.memory() != nil || h.disk(t).DefaultLogbookChange != nil {
		t.Fatalf("a record was written: memory %+v, disk %+v", h.memory(), h.disk(t).DefaultLogbookChange)
	}
	if got := h.cfgSvc.Snapshot().DefaultLogbookID; got != h.a {
		t.Fatalf("the effective default moved to %d", got)
	}
}

// ---- RQ1 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ1_Validation(t *testing.T) {
	for name, tc := range map[string]struct {
		req  func(h *changeHarness) types.DefaultChangeRequest
		code string
	}{
		"a stale believed default": {func(h *changeHarness) types.DefaultChangeRequest {
			r := h.request(h.b)
			r.BelievedDefaultUUID = h.uuid[h.b]
			return r
		}, "default_stale"},
		"a missing target": {func(h *changeHarness) types.DefaultChangeRequest {
			r := h.request(h.b)
			r.LogbookUUID = "019fd5c5-0000-7000-8000-00000000dead"
			return r
		}, "default_target_unavailable"},
		"a soft-deleted target": {func(h *changeHarness) types.DefaultChangeRequest { return h.request(h.d) }, "default_target_unavailable"},
		"another callsign":      {func(h *changeHarness) types.DefaultChangeRequest { return h.request(h.c) }, "default_target_ineligible"},
		"the default itself":    {func(h *changeHarness) types.DefaultChangeRequest { return h.request(h.a) }, "default_target_active"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newChangeHarness(t)
			_, err := h.ask(t, tc.req(h))
			wantCode(t, err, tc.code)
			h.untouched(t)
		})
	}
	t.Run("a pending record", func(t *testing.T) {
		h := newChangeHarness(t)
		h.setRecord(t, h.record(h.b, ""), false)
		_, err := h.ask(t, h.request(h.b))
		wantCode(t, err, "default_change_pending")
	})
	t.Run("an unconfirmed tombstone", func(t *testing.T) {
		h := newChangeHarness(t)
		h.setRecord(t, h.record(h.b, config.RemovedCancelled), true)
		_, err := h.ask(t, h.request(h.b))
		wantCode(t, err, "default_change_unconfirmed")
	})
	t.Run("an activation pending", func(t *testing.T) {
		h := newChangeHarness(t)
		if _, err := h.cfgSvc.Update(func(c *config.Config) error {
			c.QsoArchives = append(c.QsoArchives, types.QsoArchiveConfig{ID: "019fd5c5-efcc-7193-be4f-1fee532ee3a9", Label: "Drill", Ownership: types.QsoArchiveOwnershipManaged})
			c.PendingQsoArchiveID = "019fd5c5-efcc-7193-be4f-1fee532ee3a9"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := h.ask(t, h.request(h.b))
		wantCode(t, err, "activation_pending")
		if h.memory() != nil {
			t.Fatal("a record was written")
		}
	})
	t.Run("no restart port", func(t *testing.T) {
		h := newChangeHarness(t)
		h.m.SetActivation(h.tx, h.rig, nil)
		_, err := h.ask(t, h.request(h.b))
		wantCode(t, err, "restart_unavailable")
		h.untouched(t)
	})
}

// ---- RQ2 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ2_Order(t *testing.T) {
	h := newChangeHarness(t)
	persistedAtDrain := true
	h.live.onSeal = func() { persistedAtDrain = h.memory() != nil || h.disk(t).DefaultLogbookChange != nil }
	var atRestart *config.DefaultLogbookChange
	h.restart = func() error { atRestart = h.disk(t).DefaultLogbookChange; return nil }
	out, err := h.ask(t, h.request(h.b))
	if err != nil {
		t.Fatalf("request = %v", err)
	}
	if want := []string{"seal:tx", "seal:rig", "seal:live", "restart"}; !slices.Equal(h.log.list(), want) {
		t.Fatalf("ops = %v; want %v", h.log.list(), want)
	}
	if persistedAtDrain {
		t.Fatal("the record was persisted before the live entry was drained")
	}
	if atRestart == nil || atRestart.Removed != "" || atRestart.RequestID != out.RequestID || atRestart.LogbookUUID != h.uuid[h.b] || atRestart.ArchiveID != h.archive {
		t.Fatalf("on disk at the restart: %+v; want the pending change %s to B", atRestart, out.RequestID)
	}
	if out.RequestID == "" || out.Durability != string(types.QsoArchiveDurabilityDurable) {
		t.Fatalf("accepted = %+v; want a request id, durable", out)
	}
	if !h.tx.sealed || !h.rig.sealed || !h.live.sealed {
		t.Fatal("the seals were released after the restart was requested; they are held until the exit")
	}
	if got := h.cfgSvc.Snapshot().DefaultLogbookID; got != h.a {
		t.Fatalf("the effective default moved to %d before the restart", got)
	}
}

// ---- RQ3 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ3_RefusalsReleaseEarlierSeals(t *testing.T) {
	busy := stderr.New("an FT8 QSO is in progress")
	for name, tc := range map[string]struct {
		set     func(h *changeHarness)
		owner   func(h *changeHarness) *orderSeal
		want    []string
		blocker string
	}{
		"a busy FT8 run, or armed TX": {func(h *changeHarness) { h.tx.refuse, h.tx.active = busy, true },
			func(h *changeHarness) *orderSeal { return h.tx }, []string{"seal:tx"}, "tx_busy"},
		"tune": {func(h *changeHarness) { h.rig.refuse, h.rig.active = stderr.New("tuning"), true },
			func(h *changeHarness) *orderSeal { return h.rig }, []string{"seal:tx", "seal:rig", "release:tx"}, "tx_busy"},
		"a failed live-entry seal": {func(h *changeHarness) { h.live.refuse = stderr.New("cannot seal") },
			func(h *changeHarness) *orderSeal { return h.live }, []string{"seal:tx", "seal:rig", "seal:live", "release:rig", "release:tx"}, "live_entry_busy"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newChangeHarness(t)
			tc.set(h)
			_, err := h.ask(t, h.request(h.b))
			wantCode(t, err, tc.blocker)
			if !slices.Equal(h.log.list(), tc.want) {
				t.Fatalf("ops = %v; want %v", h.log.list(), tc.want)
			}
			h.sealsReleased(t)
			if o := tc.owner(h); o.refuse != nil && o.name != "live" && !o.active {
				t.Fatalf("the %s activity was ended by the refusal", o.name)
			}
			if h.memory() != nil || h.disk(t).DefaultLogbookChange != nil {
				t.Fatal("a record was written")
			}
		})
	}
}

// ---- RQ4 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ4_Persistence(t *testing.T) {
	t.Run("a definitive failure: refused, released, nothing on disk", func(t *testing.T) {
		h := newChangeHarness(t)
		var err error
		h.configDir(t, 0o500, func() { _, err = h.ask(t, h.request(h.b)) })
		wantCode(t, err, "default_change_persist_failed")
		h.sealsReleased(t)
		if h.restarts != 0 {
			t.Fatal("the restart was requested")
		}
		if h.memory() != nil || h.disk(t).DefaultLogbookChange != nil {
			t.Fatal("a record remains")
		}
	})
	t.Run("an uncertain save: proceeds, reported uncertain", func(t *testing.T) {
		h := newChangeHarness(t)
		var out types.DefaultChangeAccepted
		var err error
		h.configDir(t, 0o300, func() { out, err = h.ask(t, h.request(h.b)) })
		if err != nil || out.Durability != string(types.QsoArchiveDurabilityUncertain) || h.restarts != 1 {
			t.Fatalf("accepted = %+v, %v (restarts %d); want uncertain and the restart requested", out, err, h.restarts)
		}
		if r := h.disk(t).DefaultLogbookChange; r == nil || r.RequestID != out.RequestID || r.Removed != "" {
			t.Fatalf("disk = %+v; want the pending change", r)
		}
	})
}

// ---- RQ5 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ5_ARestartFailureWithdraws(t *testing.T) {
	failing := func(h *changeHarness, mode os.FileMode) {
		h.restart = func() error {
			if mode != 0 {
				if err := os.Chmod(filepath.Dir(h.cfgSvc.Path), mode); err != nil {
					return err
				}
			}
			return stderr.New("the service manager refused")
		}
	}
	t.Run("withdrawal durable: confirmed", func(t *testing.T) {
		h := newChangeHarness(t)
		failing(h, 0)
		_, err := h.ask(t, h.request(h.b))
		wantCode(t, err, "restart_failed")
		h.sealsReleased(t)
		if h.memory() != nil {
			t.Fatalf("memory = %+v; want the withdrawal confirmed", h.memory())
		}
		disk := h.disk(t).DefaultLogbookChange
		if disk == nil || disk.Removed != config.RemovedWithdrawn {
			t.Fatalf("disk = %+v; want the confirming document's withdrawn tombstone", disk)
		}
		if h.cfgSvc.Snapshot().DefaultLogbookID != h.a {
			t.Fatal("the effective default moved")
		}
		// The record is gone; the status still tells a client that lost the
		// response what became of its request.
		h.wantChange(t, disk.RequestID, "restart_failed", "restart_failed", false)
	})
	t.Run("withdrawal uncertain: an unconfirmed tombstone protects B", func(t *testing.T) {
		h := newChangeHarness(t)
		failing(h, 0o300)
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700) })
		_, err := h.ask(t, h.request(h.b))
		_ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700)
		wantCode(t, err, "restart_failed_withdrawal_unconfirmed")
		h.sealsReleased(t)
		r := h.memory()
		if r == nil || r.Removed != config.RemovedWithdrawn || r.LogbookUUID != h.uuid[h.b] {
			t.Fatalf("memory = %+v; want an unconfirmed withdrawn tombstone naming B", r)
		}
		if h.cfgSvc.Snapshot().DefaultLogbookID != h.a {
			t.Fatal("the effective default moved")
		}
		h.wantChange(t, r.RequestID, "withdrawal_unconfirmed", "restart_failed_withdrawal_unconfirmed", true)
		// Confirmed later — by a Confirm, or here by any durable save.
		if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.UserAgent += "x"; return nil }); err != nil {
			t.Fatal(err)
		}
		h.wantChange(t, r.RequestID, "restart_failed", "restart_failed", false)
	})
	t.Run("withdrawal uncertain, then a Confirm", func(t *testing.T) {
		h := newChangeHarness(t)
		failing(h, 0o300)
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700) })
		_, _ = h.ask(t, h.request(h.b))
		_ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700)
		r := h.memory()
		if r == nil {
			t.Fatal("no unconfirmed tombstone")
		}
		if _, err := h.m.ConfirmDefaultChange(context.Background(), r.RequestID); err != nil {
			t.Fatalf("Confirm = %v", err)
		}
		h.wantChange(t, r.RequestID, "restart_failed", "restart_failed", false)
	})
	t.Run("withdrawal failed: the selector stays, pending unclear", func(t *testing.T) {
		h := newChangeHarness(t)
		failing(h, 0o500)
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700) })
		_, err := h.ask(t, h.request(h.b))
		_ = os.Chmod(filepath.Dir(h.cfgSvc.Path), 0o700)
		wantCode(t, err, "default_change_pending_unclear")
		h.sealsReleased(t)
		if r := h.memory(); r == nil || r.Removed != "" {
			t.Fatalf("memory = %+v; want the pending selector kept", r)
		}
		r := h.disk(t).DefaultLogbookChange
		if r == nil || r.Removed != "" {
			t.Fatalf("disk = %+v; want the pending selector kept", r)
		}
		h.wantChange(t, r.RequestID, "pending_unclear", "default_change_pending_unclear", true)
		// Cancelled later, durably: the status says so.
		if _, err := h.m.CancelDefaultChange(context.Background(), r.RequestID); err != nil {
			t.Fatalf("Cancel = %v", err)
		}
		h.wantChange(t, r.RequestID, "cancelled", "", false)
	})
}

// ---- RQ6 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ6_Coordination(t *testing.T) {
	// holdRestart blocks the restart port until release is called, returning
	// once the operation is inside it. release is also a cleanup, so a failing
	// assertion never leaves the operation blocked.
	holdRestart := func(t *testing.T, h *changeHarness) (entered chan struct{}, release func()) {
		entered, gate := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release = func() { once.Do(func() { close(gate) }) }
		t.Cleanup(release)
		h.restart = func() error { close(entered); <-gate; return nil }
		return entered, release
	}
	wait := func(t *testing.T, what string, ch chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not reach its barrier", what)
		}
	}
	// refuses runs a competing call with a bound: one that blocks behind the
	// held operation instead of refusing fails here, informatively.
	refuses := func(t *testing.T, what, code string, call func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- call() }()
		select {
		case err := <-done:
			wantCode(t, err, code)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s blocked behind the operation in flight instead of refusing with %s", what, code)
		}
	}
	ctx := context.Background()
	t.Run("a default change in flight refuses an activation, another change and a deletion", func(t *testing.T) {
		h := newChangeHarness(t)
		entered, release := holdRestart(t, h)
		done := make(chan error, 1)
		go func() { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); done <- err }()
		wait(t, "the change", entered)
		if op := h.m.OperationInFlight(); op != "default_change" {
			t.Fatalf("operation in flight = %q; want default_change", op)
		}
		refuses(t, "an activation", "default_change_in_progress", func() error { _, err := h.m.Activate(ctx, h.archive); return err })
		refuses(t, "another change", "default_change_in_progress", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
		ran := false
		refuses(t, "a deletion", "default_change_in_progress", func() error {
			return h.m.WithLogbookChange(func() error { ran = true; return nil })
		})
		if ran {
			t.Fatal("the deletion ran during the change")
		}
		release()
		if err := <-done; err != nil {
			t.Fatalf("the held change = %v", err)
		}
	})
	t.Run("an activation in flight refuses a default change and a deletion", func(t *testing.T) {
		h := newChangeHarness(t)
		f := newActivationFixtureOn(t, h)
		entered, release := holdRestart(t, h)
		done := make(chan error, 1)
		go func() { _, err := h.m.Activate(ctx, f.contest.Entry.ID); done <- err }()
		wait(t, "the activation", entered)
		if op := h.m.OperationInFlight(); op != "activation" {
			t.Fatalf("operation in flight = %q; want activation", op)
		}
		refuses(t, "a change", "activation_in_progress", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
		refuses(t, "a deletion", "activation_in_progress", func() error { return h.m.WithLogbookChange(func() error { return nil }) })
		release()
		if err := <-done; err != nil {
			t.Fatalf("the held activation = %v", err)
		}
		// Released, the activation's pending selector still refuses a change.
		refuses(t, "a change after the activation", "activation_pending", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
	})
	t.Run("a deletion in flight refuses a default change and an activation", func(t *testing.T) {
		h := newChangeHarness(t)
		entered, gate := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(release)
		done := make(chan error, 1)
		go func() {
			done <- h.m.WithLogbookChange(func() error { close(entered); <-gate; return nil })
		}()
		wait(t, "the deletion", entered)
		if op := h.m.OperationInFlight(); op != "logbook_change" {
			t.Fatalf("operation in flight = %q; want logbook_change", op)
		}
		refuses(t, "a change", "logbook_change_in_progress", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
		refuses(t, "an activation", "logbook_change_in_progress", func() error { _, err := h.m.Activate(ctx, h.archive); return err })
		release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a surviving record refuses after the lock is released", func(t *testing.T) {
		h := newChangeHarness(t)
		dir := filepath.Dir(h.cfgSvc.Path)
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		h.restart = func() error {
			_ = os.Chmod(dir, 0o500)
			return stderr.New("refused")
		}
		_, _ = h.ask(t, h.request(h.b)) // pending unclear: the selector survives
		_ = os.Chmod(dir, 0o700)
		h.restart = func() error { return nil }
		if op := h.m.OperationInFlight(); op != "" {
			t.Fatalf("operation in flight = %q after it returned; want none", op)
		}
		refuses(t, "a change", "default_change_pending", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
		refuses(t, "an activation", "default_change_pending", func() error { _, err := h.m.Activate(ctx, h.archive); return err })
	})
	t.Run("an accepted change refuses another and its own Cancel while restarting", func(t *testing.T) {
		h := newChangeHarness(t)
		out, err := h.ask(t, h.request(h.b))
		if err != nil {
			t.Fatal(err)
		}
		refuses(t, "another change", "default_change_pending", func() error { _, err := h.m.RequestDefaultChange(ctx, h.request(h.b)); return err })
		refuses(t, "its Cancel", "default_change_in_progress", func() error { _, err := h.m.CancelDefaultChange(ctx, out.RequestID); return err })
	})
}

// newActivationFixtureOn provisions an archive to activate on h's manager.
func newActivationFixtureOn(t *testing.T, h *changeHarness) *activationFixture {
	t.Helper()
	res, err := h.m.Create(context.Background(), CreateRequest{RequestKey: "k-contest", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	return &activationFixture{m: h.m, cfgSvc: h.cfgSvc, home: h.archive, contest: res}
}

// ---- RQ7 ---------------------------------------------------------------------

// outcome is what an action's status and the disk should show afterwards.
// none: nothing in play; otherwise the change's state, removal and whether
// config still holds it. disk is the record's removal on disk ("" pending,
// "none" absent).
type outcome struct {
	none        bool
	settled     string // with none: the state the status reports, unconfirmed false
	state       string
	removed     string
	unconfirmed bool
	disk        string
}

// checkOutcome checks the action's returned status, the status read back
// afterwards, memory and disk together.
func (h *changeHarness) checkOutcome(t *testing.T, returned *types.DefaultChangeStatus, w outcome) {
	t.Helper()
	read, err := h.m.DefaultChangeStatus(context.Background())
	if err != nil {
		t.Fatalf("status = %v", err)
	}
	check := func(where string, st types.DefaultChangeStatus) {
		t.Helper()
		if st.Effective.ID != h.a {
			t.Fatalf("%s: effective = %+v; want A", where, st.Effective)
		}
		if w.none {
			if c := st.Change; c == nil || c.State != w.settled || c.Unconfirmed {
				t.Fatalf("%s: change = %+v; want it settled as %q, nothing held", where, c, w.settled)
			}
			return
		}
		c := st.Change
		if c == nil || c.State != w.state || c.Removed != w.removed || c.Unconfirmed != w.unconfirmed {
			t.Fatalf("%s: change = %+v; want state %q, removed %q, unconfirmed %v", where, c, w.state, w.removed, w.unconfirmed)
		}
	}
	// The state first, then what the statuses say about it.
	mem := h.memory()
	if w.none != (mem == nil) || (mem != nil && mem.Removed != w.removed) {
		t.Fatalf("memory = %+v; want none=%v removed=%q", mem, w.none, w.removed)
	}
	disk := h.disk(t).DefaultLogbookChange
	switch {
	case w.disk == "none" && disk != nil:
		t.Fatalf("disk = %+v; want none", *disk)
	case w.disk != "none" && (disk == nil || disk.Removed != w.disk):
		t.Fatalf("disk = %+v; want removed=%q", disk, w.disk)
	}
	if returned != nil {
		check("returned", *returned)
	}
	check("read back", read)
}

const rid = "019fd5c5-0000-7000-8000-0000000000aa"

type changeAction func(m *Manager, id string) (types.DefaultChangeStatus, error)

var (
	cancelAct = changeAction(func(m *Manager, id string) (types.DefaultChangeStatus, error) {
		return m.CancelDefaultChange(context.Background(), id)
	})
	confirmAct = changeAction(func(m *Manager, id string) (types.DefaultChangeStatus, error) {
		return m.ConfirmDefaultChange(context.Background(), id)
	})
	retryAct = changeAction(func(m *Manager, id string) (types.DefaultChangeStatus, error) {
		return m.RetryDefaultCleanup(context.Background(), id)
	})
)

// runAction runs act with the config directory at mode (0: writable).
func (h *changeHarness) runAction(t *testing.T, act changeAction, mode os.FileMode) (types.DefaultChangeStatus, error) {
	t.Helper()
	var st types.DefaultChangeStatus
	var err error
	run := func() { st, err = act(h.m, rid) }
	if mode != 0 {
		h.configDir(t, mode, run)
	} else {
		run()
	}
	return st, err
}

func TestDefaultChangeRequest_RQ7_RecoveryActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		// setup puts the record in play: its target, removal, and whether the
		// write that placed it was uncertain (an unconfirmed tombstone).
		target    func(h *changeHarness) int64
		removed   string
		uncertain bool
		act       changeAction
		mode      os.FileMode
		code      string
		want      outcome
	}{
		{"Cancel, durable: confirmed", tgtB, "", false, cancelAct, 0, "",
			outcome{none: true, settled: "cancelled", disk: config.RemovedCancelled}},
		{"Cancel, uncertain: an unconfirmed tombstone", tgtB, "", false, cancelAct, 0o300, "",
			outcome{state: "withdrawal_unconfirmed", removed: config.RemovedCancelled, unconfirmed: true, disk: config.RemovedCancelled}},
		{"Cancel, failed: still pending", tgtB, "", false, cancelAct, 0o500, "default_change_write_failed",
			outcome{state: "pending_unclear", removed: "", unconfirmed: true, disk: ""}},

		{"Confirm, durable: confirmed", tgtB, config.RemovedWithdrawn, true, confirmAct, 0, "",
			outcome{none: true, settled: "withdrawn", disk: config.RemovedWithdrawn}},
		{"Confirm, uncertain: still unconfirmed", tgtB, config.RemovedWithdrawn, true, confirmAct, 0o300, "",
			outcome{state: "withdrawal_unconfirmed", removed: config.RemovedWithdrawn, unconfirmed: true, disk: config.RemovedWithdrawn}},
		{"Confirm, failed: still unconfirmed", tgtB, config.RemovedWithdrawn, true, confirmAct, 0o500, "default_change_write_failed",
			outcome{state: "withdrawal_unconfirmed", removed: config.RemovedWithdrawn, unconfirmed: true, disk: config.RemovedWithdrawn}},

		{"Retry of a selector naming the default in effect, durable", tgtA, "", false, retryAct, 0, "",
			outcome{none: true, settled: "effective", disk: config.RemovedApplied}},
		{"Retry of a selector naming the default in effect, uncertain", tgtA, "", false, retryAct, 0o300, "",
			outcome{state: "applied_cleanup_pending", removed: config.RemovedApplied, unconfirmed: true, disk: config.RemovedApplied}},
		{"Retry of a selector naming the default in effect, failed", tgtA, "", false, retryAct, 0o500, "default_change_write_failed",
			outcome{state: "applied_cleanup_pending", removed: "", unconfirmed: true, disk: ""}},

		{"Retry of an applied tombstone, durable", tgtA, config.RemovedApplied, true, retryAct, 0, "",
			outcome{none: true, settled: "effective", disk: config.RemovedApplied}},
		{"Retry of an applied tombstone, uncertain", tgtA, config.RemovedApplied, true, retryAct, 0o300, "",
			outcome{state: "applied_cleanup_pending", removed: config.RemovedApplied, unconfirmed: true, disk: config.RemovedApplied}},
		{"Retry of an applied tombstone, failed", tgtA, config.RemovedApplied, true, retryAct, 0o500, "default_change_write_failed",
			outcome{state: "applied_cleanup_pending", removed: config.RemovedApplied, unconfirmed: true, disk: config.RemovedApplied}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newChangeHarness(t)
			h.setRecord(t, h.record(tc.target(h), tc.removed), tc.uncertain)
			st, err := h.runAction(t, tc.act, tc.mode)
			if tc.code != "" {
				wantCode(t, err, tc.code)
				h.checkOutcome(t, nil, tc.want)
				return
			}
			if err != nil {
				t.Fatalf("action = %v", err)
			}
			h.checkOutcome(t, &st, tc.want)
		})
	}
}

func tgtA(h *changeHarness) int64 { return h.a }
func tgtB(h *changeHarness) int64 { return h.b }

func TestDefaultChangeRequest_RQ7b_RefusedOutsideItsState(t *testing.T) {
	for name, tc := range map[string]struct {
		target  func(h *changeHarness) int64
		removed string
		act     changeAction
	}{
		"Cancel of a tombstone":                 {tgtB, config.RemovedWithdrawn, cancelAct},
		"Cancel of an already-applied selector": {tgtA, "", cancelAct},
		"Confirm of a pending change":           {tgtB, "", confirmAct},
		"Retry of a pending, unapplied change":  {tgtB, "", retryAct},
		"Retry of a cancelled tombstone":        {tgtB, config.RemovedCancelled, retryAct},
	} {
		t.Run(name, func(t *testing.T) {
			h := newChangeHarness(t)
			h.setRecord(t, h.record(tc.target(h), tc.removed), tc.removed != "")
			before := *h.memory()
			beforeDisk := h.disk(t).DefaultLogbookChange
			_, err := tc.act(h.m, rid)
			wantCode(t, err, "not_permitted")
			if got := h.memory(); got == nil || *got != before {
				t.Fatalf("memory = %+v; want %+v unchanged", got, before)
			}
			if got := h.disk(t).DefaultLogbookChange; got == nil || beforeDisk == nil || *got != *beforeDisk {
				t.Fatalf("disk = %+v; want %+v unchanged", got, beforeDisk)
			}
		})
	}
}

func TestDefaultChangeRequest_RQ7c_StaleRequestIDs(t *testing.T) {
	for name, act := range map[string]changeAction{"Cancel": cancelAct, "Confirm": confirmAct, "Retry": retryAct} {
		t.Run(name+": nothing in play", func(t *testing.T) {
			h := newChangeHarness(t)
			_, err := act(h.m, rid)
			wantCode(t, err, "already_resolved")
			if h.memory() != nil || h.disk(t).DefaultLogbookChange != nil {
				t.Fatal("a record was written")
			}
		})
		t.Run(name+": another request in play", func(t *testing.T) {
			h := newChangeHarness(t)
			h.setRecord(t, h.record(h.b, ""), false)
			_, err := act(h.m, "019fd5c5-0000-7000-8000-0000000000bb")
			wantCode(t, err, "pending_changed")
			for where, got := range map[string]*config.DefaultLogbookChange{"memory": h.memory(), "disk": h.disk(t).DefaultLogbookChange} {
				if got == nil || got.RequestID != rid || got.Removed != "" {
					t.Fatalf("%s = %+v; want the other request untouched", where, got)
				}
			}
		})
	}
	t.Run("R confirmed by an unrelated save, then S: late actions on R leave S untouched", func(t *testing.T) {
		h := newChangeHarness(t)
		h.setRecord(t, h.record(h.b, config.RemovedCancelled), true) // R, unconfirmed
		if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.UserAgent += "x"; return nil }); err != nil {
			t.Fatal(err)
		}
		if h.memory() != nil {
			t.Fatalf("memory = %+v; want R confirmed by the unrelated durable save", h.memory())
		}
		s, err := h.ask(t, h.request(h.b))
		if err != nil {
			t.Fatalf("request S = %v", err)
		}
		for name, act := range map[string]changeAction{"Cancel": cancelAct, "Confirm": confirmAct, "Retry": retryAct} {
			_, err := act(h.m, rid)
			wantCode(t, err, "pending_changed")
			for where, got := range map[string]*config.DefaultLogbookChange{"memory": h.memory(), "disk": h.disk(t).DefaultLogbookChange} {
				if got == nil || got.RequestID != s.RequestID || got.Removed != "" {
					t.Fatalf("after a late %s of R, %s = %+v; want S pending", name, where, got)
				}
			}
			st, err := h.m.DefaultChangeStatus(context.Background())
			if err != nil || st.Change == nil || st.Change.RequestID != s.RequestID || st.Change.State != "pending_restart" {
				t.Fatalf("after a late %s of R, status = %+v, %v; want S pending its restart", name, st.Change, err)
			}
		}
	})
}

// ---- RQ8 ---------------------------------------------------------------------

func TestDefaultChangeRequest_RQ8_Status(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	st, err := h.m.DefaultChangeStatus(ctx)
	if err != nil || st.Effective.ID != h.a || st.Effective.UUID != h.uuid[h.a] || st.Change != nil {
		t.Fatalf("status = %+v, %v; want A in effect and nothing in play", st, err)
	}
	out, err := h.ask(t, h.request(h.b))
	if err != nil {
		t.Fatal(err)
	}
	// The response is lost; the status finds the request by its id.
	st, err = h.m.DefaultChangeStatus(ctx)
	if err != nil || st.Change == nil || st.Change.RequestID != out.RequestID || st.Change.LogbookUUID != h.uuid[h.b] ||
		st.Change.State != "pending_restart" || !st.Change.Unconfirmed || st.Effective.ID != h.a {
		t.Fatalf("status = %+v (change %+v), %v; want A in effect and %s pending its restart", st, st.Change, err, out.RequestID)
	}
}

// RQ8b: with nothing in play, the status reports what this start did.
func TestDefaultChangeRequest_RQ8b_TheStartsReport(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	h.m.SetDefaultChangeReport(func() StartReport {
		return StartReport{RequestID: rid8, State: "not_applied_abandoned", Settled: "not_applied_abandoned", FailureCode: "default_change_target_missing"}
	})
	st, err := h.m.DefaultChangeStatus(ctx)
	if err != nil || st.Change == nil || st.Change.State != "not_applied_abandoned" || !strings.HasPrefix(st.Change.FailureCode, "default_change_") {
		t.Fatalf("status = %+v, %v; want the start's report", st, err)
	}
}

const rid8 = "019fd5c5-0000-7000-8000-0000000000cc"

// RQ8c: the status is derived from one config snapshot. A durable save that
// confirms the record while the status is being built must not yield a state
// that holds only while config holds the record, reported as not held. The
// barrier is the start report's port, called while the status is built.
func TestDefaultChangeRequest_RQ8c_StatusIsCoherentAcrossAConfirmingSave(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		removed string
		start   StartReport
	}{
		"a withdrawal found at start": {config.RemovedWithdrawn, StartReport{RequestID: rid, State: "withdrawal_unconfirmed"}},
		"an applied cleanup":          {config.RemovedApplied, StartReport{RequestID: rid, State: "applied_cleanup_pending", Settled: "effective"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newChangeHarness(t)
			h.setRecord(t, h.record(h.b, tc.removed), true)
			entered, gate := make(chan struct{}), make(chan struct{})
			var once, arm sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(release)
			h.m.SetDefaultChangeReport(func() StartReport {
				arm.Do(func() { close(entered); <-gate })
				return tc.start
			})
			done := make(chan types.DefaultChangeStatus, 1)
			go func() { st, _ := h.m.DefaultChangeStatus(ctx); done <- st }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the status never read the start report")
			}
			if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.UserAgent += "x"; return nil }); err != nil {
				t.Fatal(err)
			}
			if h.memory() != nil {
				t.Fatal("the save did not confirm the record")
			}
			release()
			st := <-done
			held := map[string]bool{"withdrawal_unconfirmed": true, "applied_cleanup_pending": true, "pending_unclear": true, "pending_restart": true}
			if c := st.Change; c != nil && held[c.State] != c.Unconfirmed {
				t.Fatalf("status change = %+v: state %q with unconfirmed %v mixes two snapshots", *c, c.State, c.Unconfirmed)
			}
		})
	}
}

// pausingDB is the real store with FetchAllLogbooksWithContext able to pause
// once, which an action calls (for the effective default) after it has read
// the record it acts on and before it writes.
type pausingDB struct {
	*sqlite.Service
	once          sync.Once
	entered, gate chan struct{}
}

func (p *pausingDB) FetchAllLogbooksWithContext(ctx context.Context) ([]types.Logbook, error) {
	p.once.Do(func() { close(p.entered); <-p.gate })
	return p.Service.FetchAllLogbooksWithContext(ctx)
}

// RQ7d (R12): an action rechecks the record under config's lock before
// writing. Here a durable unrelated save confirms the tombstone after the
// action read it; the action must answer already_resolved and must not put it
// back — its own write would be uncertain, so a restored tombstone would stay.
func TestDefaultChangeRequest_RQ7d_ARecordConfirmedMeanwhileIsNotRestored(t *testing.T) {
	for name, tc := range map[string]struct {
		target  func(h *changeHarness) int64
		removed string
		act     changeAction
	}{
		"Confirm of a withdrawal":       {tgtB, config.RemovedWithdrawn, confirmAct},
		"Retry of an applied tombstone": {tgtA, config.RemovedApplied, retryAct},
	} {
		t.Run(name, func(t *testing.T) {
			h := newChangeHarness(t)
			h.setRecord(t, h.record(tc.target(h), tc.removed), true)
			p := &pausingDB{Service: h.db, entered: make(chan struct{}), gate: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(p.gate) }) }
			t.Cleanup(release)
			h.m.SetActiveBindings(p, nil)
			dir := filepath.Dir(h.cfgSvc.Path)
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

			done := make(chan error, 1)
			go func() { _, err := tc.act(h.m, rid); done <- err }()
			select {
			case <-p.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the action never read the effective default")
			}
			if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.UserAgent += "x"; return nil }); err != nil {
				t.Fatal(err)
			}
			if h.memory() != nil {
				t.Fatal("the unrelated save did not confirm the tombstone")
			}
			if err := os.Chmod(dir, 0o300); err != nil { // the action's own write, if any, is uncertain
				t.Fatal(err)
			}
			release()
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the action never finished")
			}
			_ = os.Chmod(dir, 0o700)
			wantCode(t, err, "already_resolved")
			if r := h.memory(); r != nil {
				t.Fatalf("memory = %+v; the action restored a confirmed record", *r)
			}
		})
	}
}

// RQ8d: once config no longer holds the record, the status reports only what
// the start settled into — never a state that holds only while the record is
// held (here: a withdrawal found unconfirmed at start, confirmed since).
func TestDefaultChangeRequest_RQ8d_OnlySettledStatesWithoutTheRecord(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	for _, tc := range []struct {
		start StartReport
		want  string // "" = nothing reported
	}{
		{StartReport{RequestID: rid, State: "withdrawal_unconfirmed"}, ""},
		{StartReport{RequestID: rid, State: "applied_cleanup_pending", Settled: "effective"}, "effective"},
		{StartReport{RequestID: rid, State: "not_applied_abandoned", Settled: "not_applied_abandoned", FailureCode: "default_change_target_missing"}, "not_applied_abandoned"},
	} {
		h.m.SetDefaultChangeReport(func() StartReport { return tc.start })
		st, err := h.m.DefaultChangeStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		switch c := st.Change; {
		case tc.want == "" && c != nil:
			t.Fatalf("start %+v: change = %+v; want nothing reported", tc.start, *c)
		case tc.want != "" && (c == nil || c.State != tc.want || c.Unconfirmed):
			t.Fatalf("start %+v: change = %+v; want %q, not held", tc.start, c, tc.want)
		}
	}
}

// RQ8e: the status reads config and the runtime outcome coherently. Paused
// after its config snapshot (no record yet), it waits while a new request's
// restart fails and its withdrawal is uncertain. Released, it may report the
// earlier idle state or the later unconfirmed withdrawal, but never a
// confirmation that has not happened.
func TestDefaultChangeRequest_RQ8e_NoInventedConfirmation(t *testing.T) {
	ctx := context.Background()
	h := newChangeHarness(t)
	dir := filepath.Dir(h.cfgSvc.Path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	entered, gate := make(chan struct{}), make(chan struct{})
	var once, arm sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	h.m.SetDefaultChangeReport(func() StartReport {
		arm.Do(func() { close(entered); <-gate })
		return StartReport{}
	})
	done := make(chan types.DefaultChangeStatus, 1)
	go func() { st, _ := h.m.DefaultChangeStatus(ctx); done <- st }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the status never reached its barrier")
	}

	h.restart = func() error {
		_ = os.Chmod(dir, 0o300) // the withdrawal will be uncertain
		return stderr.New("refused")
	}
	_, err := h.ask(t, h.request(h.b))
	_ = os.Chmod(dir, 0o700)
	wantCode(t, err, "restart_failed_withdrawal_unconfirmed")
	held := h.memory()
	if held == nil || held.Removed != config.RemovedWithdrawn {
		t.Fatalf("memory = %+v; want the unconfirmed withdrawn tombstone", held)
	}

	release()
	st := <-done
	switch c := st.Change; {
	case c == nil:
		// The earlier idle state.
	case c.RequestID == held.RequestID && c.State == "withdrawal_unconfirmed" && c.Unconfirmed:
		// The later unconfirmed withdrawal.
	default:
		t.Fatalf("status change = %+v while config still holds the unconfirmed tombstone %+v: an invented or mixed state", *c, *held)
	}
}

// RQ7e: Confirm keeps an abandonment's reason. The tombstone carries it
// (written at the start that abandoned the change); confirming the removal —
// durably, or uncertainly and then by an unrelated durable save — must report
// it with the settled state.
func TestDefaultChangeRequest_RQ7e_ConfirmKeepsTheAbandonmentReason(t *testing.T) {
	const code = "default_change_target_missing"
	abandoned := func(h *changeHarness) *config.DefaultLogbookChange {
		r := h.record(h.b, config.RemovedAbandoned)
		r.FailureCode = code
		return r
	}
	t.Run("a durable Confirm", func(t *testing.T) {
		h := newChangeHarness(t)
		h.setRecord(t, abandoned(h), true)
		if _, err := h.m.ConfirmDefaultChange(context.Background(), rid); err != nil {
			t.Fatalf("Confirm = %v", err)
		}
		h.wantChange(t, rid, "not_applied_abandoned", code, false)
	})
	t.Run("an uncertain Confirm, then an unrelated durable save", func(t *testing.T) {
		h := newChangeHarness(t)
		h.setRecord(t, abandoned(h), true)
		var err error
		h.configDir(t, 0o300, func() { _, err = h.m.ConfirmDefaultChange(context.Background(), rid) })
		if err != nil {
			t.Fatalf("Confirm = %v", err)
		}
		h.wantChange(t, rid, "withdrawal_unconfirmed", code, true)
		if _, err := h.cfgSvc.Update(func(c *config.Config) error { c.UserAgent += "x"; return nil }); err != nil {
			t.Fatal(err)
		}
		h.wantChange(t, rid, "not_applied_abandoned", code, false)
	})
}
