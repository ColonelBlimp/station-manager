package archive

import (
	stderr "errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// The switch request, in order (ADR 0100 §4; W-0029 part 3, criteria B to F).
// After today's refusals and the FT8 and rig seals: live Phone / CW entry is
// sealed, FT8 logging already earned is waited for, the open session is closed
// in the archive being left, and only then is the selector written and the
// restart asked for. A refusal changes nothing. If the close fails nothing is
// requested and every seal is released. If a later step fails the switch's
// seals are released and the closing phase is ended.

// trace is the order in which the switch touched its ports.
type trace []string

type tracedSeal struct {
	name   string
	t      *trace
	refuse error
	sealed bool
}

func (s *tracedSeal) Seal() error {
	if s.refuse != nil {
		*s.t = append(*s.t, s.name+" refused")
		return s.refuse
	}
	*s.t = append(*s.t, s.name+" sealed")
	s.sealed = true
	return nil
}

func (s *tracedSeal) Release() {
	*s.t = append(*s.t, s.name+" released")
	s.sealed = false
}

type tracedSession struct {
	t        *trace
	closeErr error
}

func (s *tracedSession) Drain() { *s.t = append(*s.t, "drain") }
func (s *tracedSession) Close() error {
	if s.closeErr != nil {
		*s.t = append(*s.t, "close failed")
		return s.closeErr
	}
	*s.t = append(*s.t, "close")
	return nil
}
func (s *tracedSession) Failed() { *s.t = append(*s.t, "phase ended") }

type switchFixture struct {
	*activationFixture
	trace         trace
	tx, rig, live *tracedSeal
	session       *tracedSession
	restartErr    error
	atRestart     func()
}

func newSwitchFixture(t *testing.T) *switchFixture {
	t.Helper()
	f := &switchFixture{activationFixture: newActivationFixture(t)}
	f.tx, f.rig, f.live = &tracedSeal{name: "tx", t: &f.trace}, &tracedSeal{name: "rig", t: &f.trace}, &tracedSeal{name: "live", t: &f.trace}
	f.session = &tracedSession{t: &f.trace}
	f.m.SetActivation(f.tx, f.rig, func() error {
		if f.atRestart != nil {
			f.atRestart()
		}
		if f.restartErr != nil {
			f.trace = append(f.trace, "restart failed")
			return f.restartErr
		}
		f.trace = append(f.trace, "restart")
		return nil
	})
	f.m.SetSwitch(f.live, f.session)
	return f
}

func (f *switchFixture) want(t *testing.T, want ...string) {
	t.Helper()
	if !reflect.DeepEqual([]string(f.trace), want) {
		t.Fatalf("the switch did\n  %v\nwant\n  %v", []string(f.trace), want)
	}
}

func (f *switchFixture) noSelector(t *testing.T) {
	t.Helper()
	for name, c := range map[string]config.Config{"memory": f.cfgSvc.Snapshot(), "disk": f.onDisk(t)} {
		if c.PendingQsoArchiveID != "" || c.PendingQsoArchiveActivationID != "" {
			t.Fatalf("%s: selector %q, identity %q; want none", name, c.PendingQsoArchiveID, c.PendingQsoArchiveActivationID)
		}
	}
}

func TestSwitch_ASuccessTakesItsStepsInOrderAndKeepsItsSeals(t *testing.T) {
	f := newSwitchFixture(t)
	f.atRestart = func() {
		// The selector is written before the restart is asked for, and after the close.
		if p := f.cfgSvc.Snapshot().PendingQsoArchiveID; p != f.contest.Entry.ID {
			t.Errorf("at the restart request the selector is %q, want it written", p)
		}
	}
	if _, err := f.activate(t); err != nil {
		t.Fatalf("activate: %v", err)
	}
	f.want(t, "tx sealed", "rig sealed", "live sealed", "drain", "close", "restart")
	if !f.tx.sealed || !f.rig.sealed || !f.live.sealed {
		t.Fatalf("the seals are held until the process exits: tx=%v rig=%v live=%v", f.tx.sealed, f.rig.sealed, f.live.sealed)
	}
}

// A refusal changes nothing: no seal is taken, live entry's included, and the
// session is not drained, closed or told anything.
func TestSwitch_ARefusalTouchesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange func(t *testing.T, f *switchFixture) (target string)
		code    string
	}{
		"the archive is already active": {func(_ *testing.T, f *switchFixture) string { return f.home }, "archive_active"},
		"no such archive":               {func(*testing.T, *switchFixture) string { return "019fd5c5-efcc-7193-be4f-1fee532ee3aa" }, "archive_not_found"},
		"a default change is pending": {func(t *testing.T, f *switchFixture) string {
			if _, err := f.cfgSvc.Update(func(c *config.Config) error {
				c.DefaultLogbookChange = &config.DefaultLogbookChange{RequestID: "r1", LogbookUUID: "0190aaaa-0000-7000-8000-00000000000b"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return f.contest.Entry.ID
		}, "default_change_pending"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSwitchFixture(t)
			target := tc.arrange(t, f)
			if _, err := f.m.Activate(t.Context(), target); codeOf(t, err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
			f.want(t)
			f.noSelector(t)
		})
	}
}

// FT8 or the rig busy refuses as today. Live entry has not been sealed by the
// switch and the session is untouched.
func TestSwitch_TxBusyLeavesLiveEntryAndTheSessionAlone(t *testing.T) {
	t.Run("FT8 is busy", func(t *testing.T) {
		f := newSwitchFixture(t)
		f.tx.refuse = stderr.New("armed")
		if _, err := f.activate(t); codeOf(t, err) != "tx_busy" {
			t.Fatalf("err = %v", err)
		}
		f.want(t, "tx refused")
		f.noSelector(t)
	})
	t.Run("the rig is transmitting", func(t *testing.T) {
		f := newSwitchFixture(t)
		f.rig.refuse = stderr.New("keyed")
		if _, err := f.activate(t); codeOf(t, err) != "tx_busy" {
			t.Fatalf("err = %v", err)
		}
		if f.live.sealed || f.tx.sealed {
			t.Fatalf("seals left held: tx=%v live=%v", f.tx.sealed, f.live.sealed)
		}
		for _, step := range f.trace {
			if step == "live sealed" || step == "drain" || step == "close" || step == "phase ended" {
				t.Fatalf("a refused switch did %v", []string(f.trace))
			}
		}
		f.noSelector(t)
	})
}

// If the close fails, nothing is requested: every seal the switch took is
// released, no selector is written and no restart is asked for. The closing
// phase never began, so it is not ended.
func TestSwitch_ACloseThatFailsRequestsNothing(t *testing.T) {
	f := newSwitchFixture(t)
	f.session.closeErr = stderr.New("the file cannot be written")
	_, err := f.activate(t)
	if codeOf(t, err) != "session_close_failed" {
		t.Fatalf("err = %v, want session_close_failed", err)
	}
	f.want(t, "tx sealed", "rig sealed", "live sealed", "drain", "close failed", "live released", "rig released", "tx released")
	f.noSelector(t)
	// The sitting goes on, and a later switch can be asked for.
	f.session.closeErr = nil
	f.trace = nil
	if _, err := f.activate(t); err != nil {
		t.Fatalf("a switch after the failed one: %v", err)
	}
}

// A failure after the close: the switch's seals are released, then the closing
// phase is ended, so the new session it opens finds live entry open.
func TestSwitch_AFailureAfterTheCloseReleasesAndEndsThePhase(t *testing.T) {
	released := []string{"live released", "rig released", "tx released", "phase ended"}
	before := []string{"tx sealed", "rig sealed", "live sealed", "drain", "close"}
	t.Run("the selector cannot be recorded", func(t *testing.T) {
		f := newSwitchFixture(t)
		dir := filepath.Dir(f.cfgSvc.Path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if _, err := f.activate(t); codeOf(t, err) != "activation_persist_failed" {
			t.Fatalf("err = %v", err)
		}
		_ = os.Chmod(dir, 0o700)
		f.want(t, append(append([]string{}, before...), released...)...)
		f.noSelector(t)
	})
	t.Run("the restart cannot be requested", func(t *testing.T) {
		f := newSwitchFixture(t)
		f.restartErr = stderr.New("no restart channel")
		if _, err := f.activate(t); codeOf(t, err) != "restart_failed" {
			t.Fatalf("err = %v", err)
		}
		f.want(t, append(append(append([]string{}, before...), "restart failed"), released...)...)
		f.noSelector(t)
	})
	t.Run("the restart cannot be requested and the selector cannot be withdrawn", func(t *testing.T) {
		f := newSwitchFixture(t)
		dir := filepath.Dir(f.cfgSvc.Path)
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		f.restartErr = stderr.New("no restart channel")
		f.atRestart = func() { _ = os.Chmod(dir, 0o500) }
		if _, err := f.activate(t); codeOf(t, err) != "pending_unclear" {
			t.Fatalf("err = %v", err)
		}
		_ = os.Chmod(dir, 0o700)
		f.want(t, append(append(append([]string{}, before...), "restart failed"), released...)...)
		if p := f.onDisk(t).PendingQsoArchiveID; p != f.contest.Entry.ID {
			t.Fatalf("disk selector = %q; the request stands for the next start", p)
		}
	})
}
