package archive

import (
	stderr "errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// An activation has an identity (ADR 0100 §4; W-0029 part 3, criteria A1). The
// switch request writes it into config.json with the pending selector, in one
// update, and a withdrawn request removes both. The start that processes the
// activation is what moves it into effect; the request never touches the
// activation in effect.

const inEffectBefore = "0190cccc-0000-7000-8000-000000000001"

func (f *activationFixture) withActivationInEffect(t *testing.T) {
	t.Helper()
	if _, err := f.cfgSvc.Update(func(c *config.Config) error {
		c.QsoArchiveActivationID = inEffectBefore
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestActivate_WritesAnIdentityWithTheSelector(t *testing.T) {
	f := newActivationFixture(t)
	f.withActivationInEffect(t)
	if _, err := f.activate(t); err != nil {
		t.Fatalf("activate: %v", err)
	}
	mem, disk := f.cfgSvc.Snapshot(), f.onDisk(t)
	id := mem.PendingQsoArchiveActivationID
	if !utils.IsValidUUIDv7(id) {
		t.Fatalf("the pending activation's identity is %q, want a new UUID", id)
	}
	for name, c := range map[string]config.Config{"memory": mem, "disk": disk} {
		if c.PendingQsoArchiveID != f.contest.Entry.ID || c.PendingQsoArchiveActivationID != id {
			t.Fatalf("%s: selector %q with identity %q; want the selector and %q together", name, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID, id)
		}
		if c.PendingActivationIdentity() != id {
			t.Fatalf("%s: the pending identity reads %q, want the stored %q", name, c.PendingActivationIdentity(), id)
		}
		if c.QsoArchiveActivationID != inEffectBefore {
			t.Fatalf("%s: the request changed the activation in effect to %q", name, c.QsoArchiveActivationID)
		}
	}
}

// Two requests are two activations, a retry after a withdrawn one included.
func TestActivate_EachRequestHasItsOwnIdentity(t *testing.T) {
	f := newActivationFixture(t)
	fail := true
	var first string
	f.m.SetActivation(f.tx, f.rig, func() error {
		if fail {
			first = f.cfgSvc.Snapshot().PendingQsoArchiveActivationID
			return stderr.New("no restart channel")
		}
		f.restart++
		return nil
	})
	if _, err := f.activate(t); codeOf(t, err) != "restart_failed" {
		t.Fatalf("err = %v", err)
	}
	fail = false
	if _, err := f.activate(t); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	second := f.cfgSvc.Snapshot().PendingQsoArchiveActivationID
	if !utils.IsValidUUIDv7(first) || !utils.IsValidUUIDv7(second) || first == second {
		t.Fatalf("the two requests' identities are %q and %q; want two different UUIDs", first, second)
	}
}

// A withdrawn request leaves neither the selector nor its identity, and has not
// touched the activation in effect.
func TestActivate_AWithdrawnRequestRemovesItsIdentity(t *testing.T) {
	f := newActivationFixture(t)
	f.withActivationInEffect(t)
	f.m.SetActivation(f.tx, f.rig, func() error { return stderr.New("no restart channel") })
	if _, err := f.activate(t); codeOf(t, err) != "restart_failed" {
		t.Fatalf("err = %v", err)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.PendingQsoArchiveID != "" || c.PendingQsoArchiveActivationID != "" {
			t.Fatalf("%s: selector %q and identity %q after a withdrawn activation", name, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID)
		}
		if c.QsoArchiveActivationID != inEffectBefore {
			t.Fatalf("%s: the activation in effect is %q, want %q untouched", name, c.QsoArchiveActivationID, inEffectBefore)
		}
	}
}

// A selector that could not be withdrawn keeps its identity: it is the same
// activation the next start processes.
func TestActivate_AnUnclearablePendingKeepsItsIdentity(t *testing.T) {
	f := newActivationFixture(t)
	dir := filepath.Dir(f.cfgSvc.Path)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	var written string
	f.m.SetActivation(f.tx, f.rig, func() error {
		written = f.cfgSvc.Snapshot().PendingQsoArchiveActivationID
		_ = os.Chmod(dir, 0o500) // the disk goes read-only between the persist and the restart request
		return stderr.New("no restart channel")
	})
	if _, err := f.activate(t); codeOf(t, err) != "pending_unclear" {
		t.Fatalf("err = %v", err)
	}
	_ = os.Chmod(dir, 0o700)
	disk := f.onDisk(t)
	if !utils.IsValidUUIDv7(written) || disk.PendingQsoArchiveID != f.contest.Entry.ID || disk.PendingQsoArchiveActivationID != written {
		t.Fatalf("disk: selector %q with identity %q; want the selector with %q", disk.PendingQsoArchiveID, disk.PendingQsoArchiveActivationID, written)
	}
	if mem := f.cfgSvc.Snapshot(); mem.PendingQsoArchiveActivationID != written {
		t.Fatalf("memory: identity %q, want %q", mem.PendingQsoArchiveActivationID, written)
	}
}

// A refusal writes no identity.
func TestActivate_ARefusalWritesNoIdentity(t *testing.T) {
	f := newActivationFixture(t)
	f.tx.refuse = stderr.New("armed")
	if _, err := f.activate(t); codeOf(t, err) != "tx_busy" {
		t.Fatalf("err = %v", err)
	}
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.PendingQsoArchiveActivationID != "" || c.PendingQsoArchiveID != "" {
			t.Fatalf("%s: selector %q, identity %q after a refusal", name, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID)
		}
	}
}
