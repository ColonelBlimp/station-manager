package archive

import (
	"context"
	"database/sql"
	stderr "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Activation (ADR 0071, W-0021 slice 3C): the attended restart is the switch.
// The attempt seals TX admission as check-and-set through two ports, persists
// the pending selector file-first, requests the restart, and holds the seals
// until exit; every exit that is not the restart releases them (finding 3c).

type fakeSeal struct {
	refuse          error
	sealed          bool
	seals, releases int
}

func (f *fakeSeal) Seal() error {
	f.seals++
	if f.refuse != nil {
		return f.refuse
	}
	f.sealed = true
	return nil
}
func (f *fakeSeal) Release() { f.releases++; f.sealed = false }

type activationFixture struct {
	m       *Manager
	cfgSvc  *config.Service
	home    string
	contest CreateResult
	tx, rig *fakeSeal
	restart int
}

// newActivationFixture: an adopted "Home" (active) and a provisioned "Contest"
// (inactive), both seals idle, a restart port that counts.
func newActivationFixture(t *testing.T) *activationFixture {
	t.Helper()
	m, cfgSvc, _ := testManager(t)
	f := &activationFixture{m: m, cfgSvc: cfgSvc, home: "019fd5c5-efcc-7193-be4f-1fee532ee300", tx: &fakeSeal{}, rig: &fakeSeal{}}
	if _, err := cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: f.home, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: c.Datastore.Path}}
		c.ActiveQsoArchiveID = f.home
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res, err := m.Create(context.Background(), CreateRequest{RequestKey: "k-contest", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision Contest: %v", err)
	}
	f.contest = res
	m.SetActivation(f.tx, f.rig, func() error { f.restart++; return nil })
	return f
}

func (f *activationFixture) activate(t *testing.T) (types.QsoArchiveActivation, error) {
	t.Helper()
	return f.m.Activate(context.Background(), f.contest.Entry.ID)
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var re *RequestError
	if !stderr.As(err, &re) {
		t.Fatalf("error %v is not a *RequestError", err)
	}
	return re.Code
}

func (f *activationFixture) onDisk(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(f.cfgSvc.Path)
	if err != nil {
		t.Fatalf("load config.json: %v", err)
	}
	return c
}

// nothingHappened: no seal taken, nothing persisted, no restart.
func (f *activationFixture) nothingHappened(t *testing.T) {
	t.Helper()
	if f.tx.seals != 0 || f.rig.seals != 0 || f.restart != 0 {
		t.Fatalf("tx seals=%d rig seals=%d restarts=%d, want none", f.tx.seals, f.rig.seals, f.restart)
	}
	if p := f.cfgSvc.Snapshot().PendingQsoArchiveID; p != "" {
		t.Fatalf("pending = %q, want none", p)
	}
}

func TestActivate_SealsPersistsPendingAndRequestsTheRestart(t *testing.T) {
	f := newActivationFixture(t)
	out, err := f.activate(t)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if out.ID != f.contest.Entry.ID || out.Durability != types.QsoArchiveDurabilityDurable {
		t.Fatalf("activation = %+v", out)
	}
	if !f.tx.sealed || !f.rig.sealed || f.tx.releases != 0 || f.rig.releases != 0 {
		t.Fatalf("seals must be held until exit: tx=%+v rig=%+v", f.tx, f.rig)
	}
	if f.restart != 1 {
		t.Fatalf("restart requested %d times, want 1", f.restart)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.PendingQsoArchiveID != f.contest.Entry.ID || c.ActiveQsoArchiveID != f.home {
			t.Fatalf("%s: pending=%q active=%q; the request is a pending candidate, Home stays active", name, c.PendingQsoArchiveID, c.ActiveQsoArchiveID)
		}
	}
	views := f.m.List()
	if len(views) != 2 || views[0].State != types.QsoArchiveStateActive || views[1].State != types.QsoArchiveStatePending {
		t.Fatalf("list = %+v, want Home active, Contest pending", views)
	}
	// The file's stat rides the listing (slice 4): Contest exists, Home's path
	// (the fixture's datastore.path) was never created.
	if views[1].SizeBytes <= 0 || views[1].ModifiedAt == "" {
		t.Fatalf("Contest view carries no file stat: %+v", views[1])
	}
	if _, err := time.Parse(time.RFC3339, views[1].ModifiedAt); err != nil {
		t.Fatalf("modified_at %q is not RFC 3339: %v", views[1].ModifiedAt, err)
	}
	if views[0].SizeBytes != 0 || views[0].ModifiedAt != "" {
		t.Fatalf("a missing file must carry no stat: %+v", views[0])
	}
	// One activation per process: the restart is the switch.
	if _, err := f.activate(t); codeOf(t, err) != "activation_in_progress" {
		t.Fatalf("second activation = %v, want activation_in_progress", err)
	}
	if f.restart != 1 || f.tx.seals != 1 {
		t.Fatalf("a refused second activation touched the ports: restarts=%d tx seals=%d", f.restart, f.tx.seals)
	}
}

func TestActivate_RefusesBeforeSealing(t *testing.T) {
	t.Run("unknown archive", func(t *testing.T) {
		f := newActivationFixture(t)
		_, err := f.m.Activate(context.Background(), "019fd5c5-efcc-7193-be4f-1fee532ee3ff")
		if codeOf(t, err) != "archive_not_found" {
			t.Fatalf("err = %v", err)
		}
		f.nothingHappened(t)
	})
	t.Run("the active archive", func(t *testing.T) {
		f := newActivationFixture(t)
		_, err := f.m.Activate(context.Background(), f.home)
		if codeOf(t, err) != "archive_active" {
			t.Fatalf("err = %v", err)
		}
		f.nothingHappened(t)
	})
	t.Run("no restart port", func(t *testing.T) {
		f := newActivationFixture(t)
		f.m.SetActivation(f.tx, f.rig, nil)
		_, err := f.activate(t)
		if codeOf(t, err) != "restart_unavailable" {
			t.Fatalf("err = %v", err)
		}
		f.nothingHappened(t)
	})
	t.Run("the file holds another archive", func(t *testing.T) {
		f := newActivationFixture(t)
		raw, err := sql.Open("sqlite", "file:"+f.contest.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`UPDATE archive_metadata SET archive_uuid = '019fd5c5-efcc-7193-be4f-1fee532ee3ff'`); err != nil {
			t.Fatal(err)
		}
		_ = raw.Close()
		_, err = f.activate(t)
		if codeOf(t, err) != "archive_unavailable" || !strings.Contains(err.Error(), "holds archive") {
			t.Fatalf("err = %v", err)
		}
		f.nothingHappened(t)
	})
	t.Run("the file is missing", func(t *testing.T) {
		f := newActivationFixture(t)
		if err := os.Remove(f.contest.Path); err != nil {
			t.Fatal(err)
		}
		_, err := f.activate(t)
		if codeOf(t, err) != "archive_unavailable" {
			t.Fatalf("err = %v", err)
		}
		f.nothingHappened(t)
	})
}

func TestActivate_TxBusyLeavesNoSealBehind(t *testing.T) {
	t.Run("FT8 busy: the rig is never asked", func(t *testing.T) {
		f := newActivationFixture(t)
		f.tx.refuse = stderr.New("ft8: transmit is armed")
		_, err := f.activate(t)
		if codeOf(t, err) != "tx_busy" || !strings.Contains(err.Error(), "transmit is armed") {
			t.Fatalf("err = %v", err)
		}
		if f.rig.seals != 0 || f.tx.sealed || f.restart != 0 || f.cfgSvc.Snapshot().PendingQsoArchiveID != "" {
			t.Fatalf("rig seals=%d tx sealed=%v restarts=%d pending=%q", f.rig.seals, f.tx.sealed, f.restart, f.cfgSvc.Snapshot().PendingQsoArchiveID)
		}
	})
	t.Run("rig keyed: the FT8 seal is dropped", func(t *testing.T) {
		f := newActivationFixture(t)
		f.rig.refuse = stderr.New("bridge: transmission active")
		_, err := f.activate(t)
		if codeOf(t, err) != "tx_busy" {
			t.Fatalf("err = %v", err)
		}
		if f.tx.sealed || f.tx.releases != 1 || f.rig.sealed || f.restart != 0 || f.cfgSvc.Snapshot().PendingQsoArchiveID != "" {
			t.Fatalf("tx=%+v rig=%+v restarts=%d", f.tx, f.rig, f.restart)
		}
	})
}

// A definitive persist failure: nothing on disk, both seals released, no restart.
func TestActivate_PersistFailureReleasesBothSeals(t *testing.T) {
	f := newActivationFixture(t)
	dir := filepath.Dir(f.cfgSvc.Path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := f.activate(t)
	if codeOf(t, err) != "activation_persist_failed" {
		t.Fatalf("err = %v", err)
	}
	_ = os.Chmod(dir, 0o700)
	if f.tx.sealed || f.rig.sealed || f.tx.releases != 1 || f.rig.releases != 1 || f.restart != 0 {
		t.Fatalf("tx=%+v rig=%+v restarts=%d", f.tx, f.rig, f.restart)
	}
	if p := f.onDisk(t).PendingQsoArchiveID; p != "" {
		t.Fatalf("disk pending = %q, want none", p)
	}
	if p := f.cfgSvc.Snapshot().PendingQsoArchiveID; p != "" {
		t.Fatalf("memory pending = %q, want none", p)
	}
	// Admission is open again: a later activation goes through.
	if _, err := f.activate(t); err != nil {
		t.Fatalf("activation after the failed one: %v", err)
	}
}

// A failed restart request after the persist withdraws the activation.
func TestActivate_RestartFailureClearsPendingAndReleases(t *testing.T) {
	f := newActivationFixture(t)
	f.m.SetActivation(f.tx, f.rig, func() error { return stderr.New("no restart channel") })
	_, err := f.activate(t)
	if codeOf(t, err) != "restart_failed" {
		t.Fatalf("err = %v", err)
	}
	if f.tx.sealed || f.rig.sealed {
		t.Fatalf("seals held after a withdrawn activation: tx=%+v rig=%+v", f.tx, f.rig)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.PendingQsoArchiveID != "" {
			t.Fatalf("%s: pending = %q after a withdrawn activation", name, c.PendingQsoArchiveID)
		}
	}
}

// The restart request fails AND pending cannot be cleared: seals released, the
// archive lists as pending with the diagnostic, the next restart activates it.
func TestActivate_RestartFailureWithUnclearablePendingIsReported(t *testing.T) {
	f := newActivationFixture(t)
	dir := filepath.Dir(f.cfgSvc.Path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	f.m.SetActivation(f.tx, f.rig, func() error {
		_ = os.Chmod(dir, 0o500) // the disk goes read-only between the persist and the restart request
		return stderr.New("no restart channel")
	})
	_, err := f.activate(t)
	if codeOf(t, err) != "pending_unclear" {
		t.Fatalf("err = %v", err)
	}
	_ = os.Chmod(dir, 0o700)
	if f.tx.sealed || f.rig.sealed {
		t.Fatalf("seals held: tx=%+v rig=%+v", f.tx, f.rig)
	}
	if p := f.onDisk(t).PendingQsoArchiveID; p != f.contest.Entry.ID {
		t.Fatalf("disk pending = %q; the persisted request stands for the next restart", p)
	}
	views := f.m.List()
	if len(views) != 2 || views[1].State != types.QsoArchiveStatePending || !strings.Contains(views[1].LastActivationError, "could not be cleared") {
		t.Fatalf("list = %+v, want Contest pending with the diagnostic", views)
	}
}
