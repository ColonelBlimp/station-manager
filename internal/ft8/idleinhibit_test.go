package ft8

// Behaviour spec for desktop idle inhibition, written BEFORE the implementation.
//
// The rules below are stated in terms of what an OPERATOR or the DESKTOP can
// observe — is an inhibition held right now, and did arming still work — not in
// terms of which field the mechanism happens to use. That is deliberate: the
// 2026-07-27 arc established that field-level assertions get deleted within a
// round or two while behavioural ones keep catching defects.
//
// Two rules exist only because of states this change CREATES, and they are the
// ones an implementation is most likely to get wrong:
//
//   - Rule 3 (a FAILED arm holds nothing). Acquiring the inhibition before the
//     arm has actually succeeded leaks one on every refused arm — and arms are
//     refused routinely, on a CAT blink or an unreadable dial. A leaked
//     inhibition never releases, so the desktop stops idling FOREVER, which is
//     a worse bug than the one this feature fixes.
//   - Rule 4 (arming twice holds exactly one). ArmTx is documented idempotent
//     and the SPA re-sends it; a second acquire either stacks a second
//     inhibition that the single release cannot free, or replaces the stored
//     release func and orphans the first.
//
// Rule 5 is the invariant this must not violate: inhibition is a courtesy to
// the desktop, and a courtesy must never be able to stop the operator
// transmitting. Same shape as "enrichment never blocks logging".

import (
	"errors"
	"sync"
	"testing"
)

// fakeInhibitor counts inhibitions and can be made to fail. releases counts
// calls to the returned release func, so a test can tell "acquired and freed"
// from "acquired and leaked".
type fakeInhibitor struct {
	mu       sync.Mutex
	acquires int
	releases int
	err      error
	why      string
}

func (f *fakeInhibitor) Inhibit(why string) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.acquires++
	f.why = why
	return func() {
		f.mu.Lock()
		f.releases++
		f.mu.Unlock()
	}, nil
}

// held reports inhibitions acquired but not yet released — the observable that
// every rule below is stated against.
func (f *fakeInhibitor) held() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires - f.releases
}

func (f *fakeInhibitor) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acquires, f.releases
}

// armedNow reads the arm flag under txMu — race-safe for assertions.
func armedNow(s *Service) bool {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return s.txArmed
}

func newInhibitTestService(in IdleInhibitor, keyer TxKeyer, playerErr error) *Service {
	s := newTxTestService(keyer, newFakeTxPlayer(), playerErr)
	s.SetIdleInhibitor(in)
	return s
}

// Rule 1 — while TX is armed, the desktop is asked not to idle.
func TestIdleInhibit_HeldWhileArmed(t *testing.T) {
	in := &fakeInhibitor{}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)

	if in.held() != 0 {
		t.Fatalf("fixture: expected no inhibition before arming, held=%d", in.held())
	}
	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if got := in.held(); got != 1 {
		t.Errorf("armed: want exactly 1 inhibition held, got %d", got)
	}
}

// Rule 2 — disarming releases it. An inhibition that outlives the arm is the
// leak that stops the machine ever idling again.
func TestIdleInhibit_ReleasedOnDisarm(t *testing.T) {
	in := &fakeInhibitor{}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if in.held() != 1 {
		t.Fatalf("fixture: expected 1 held after arm, got %d", in.held())
	}
	if err := s.ArmTx(false); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if got := in.held(); got != 0 {
		t.Errorf("disarmed: want 0 inhibitions held, got %d", got)
	}
	if _, rel := in.counts(); rel != 1 {
		t.Errorf("want exactly 1 release, got %d", rel)
	}
}

// Rule 3 — a FAILED arm holds nothing.
//
// Arms are refused routinely (rig not ready, dial unreadable, no output device).
// Acquiring before the arm has committed leaks an inhibition every time, and
// nothing ever releases it. Both refusal paths are covered: one refused by the
// gate before any device work, one refused by the device itself, because an
// implementation could plausibly sit between them.
func TestIdleInhibit_NothingHeldWhenArmFails(t *testing.T) {
	t.Run("refused by the TX gate (rig not ready)", func(t *testing.T) {
		in := &fakeInhibitor{}
		s := newInhibitTestService(in, &fakeKeyer{notReady: true}, nil)

		if err := s.ArmTx(true); err == nil {
			t.Fatal("fixture: arm must fail for this rule to mean anything")
		}
		if acq, _ := in.counts(); acq != 0 {
			t.Errorf("refused arm: want 0 acquisitions, got %d", acq)
		}
		if got := in.held(); got != 0 {
			t.Errorf("refused arm: want 0 held, got %d", got)
		}
	})

	t.Run("refused by the output device", func(t *testing.T) {
		in := &fakeInhibitor{}
		s := newInhibitTestService(in, &fakeKeyer{}, ErrTxUnavailable)

		if err := s.ArmTx(true); err == nil {
			t.Fatal("fixture: arm must fail for this rule to mean anything")
		}
		if got := in.held(); got != 0 {
			t.Errorf("refused arm: want 0 held, got %d", got)
		}
	})
}

// Rule 4 — arming twice holds exactly ONE inhibition, and the second arm
// releases nothing. ArmTx is documented idempotent and the SPA re-sends it.
func TestIdleInhibit_ArmIsIdempotent(t *testing.T) {
	in := &fakeInhibitor{}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("first arm: %v", err)
	}
	if err := s.ArmTx(true); err != nil {
		t.Fatalf("second arm: %v", err)
	}

	acq, rel := in.counts()
	if acq != 1 {
		t.Errorf("double arm: want 1 acquisition, got %d (a stacked inhibition cannot be freed by one release)", acq)
	}
	if rel != 0 {
		t.Errorf("double arm: want 0 releases, got %d (the second arm must not free the first arm's inhibition)", rel)
	}
	if got := in.held(); got != 1 {
		t.Errorf("double arm: want 1 held, got %d", got)
	}

	if err := s.ArmTx(false); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if got := in.held(); got != 0 {
		t.Errorf("after disarm: want 0 held, got %d — one disarm must free what two arms took", got)
	}
}

// Rule 5 — a failing inhibitor NEVER stops the operator transmitting.
//
// The desktop bus is absent on a headless box and can fail at any time. TX is
// the operator's licence to use; idle inhibition is a courtesy. If this rule
// ever inverts, a broken D-Bus stops the station working.
func TestIdleInhibit_FailureNeverBlocksArming(t *testing.T) {
	in := &fakeInhibitor{err: errors.New("no session bus")}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm must succeed despite an inhibitor failure, got: %v", err)
	}
	if !armedNow(s) {
		t.Error("TX must be armed despite the inhibitor failing")
	}
	if got := in.held(); got != 0 {
		t.Errorf("failed inhibitor: want 0 held, got %d", got)
	}

	// And the failure must not wedge the next disarm/arm cycle.
	if err := s.ArmTx(false); err != nil {
		t.Fatalf("disarm after failed inhibit: %v", err)
	}
	if err := s.ArmTx(true); err != nil {
		t.Fatalf("re-arm after failed inhibit: %v", err)
	}
}

// Rule 6 — stopping the subsystem releases the inhibition. Stop disarms with
// closing=true, a different path from an operator disarm, and it is the one that
// runs when the daemon shuts down. A leak here survives the process that made it
// only if the implementation holds an OS resource, but the rule is the same:
// nothing may outlive the arm.
func TestIdleInhibit_ReleasedOnClose(t *testing.T) {
	in := &fakeInhibitor{}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if in.held() != 1 {
		t.Fatalf("fixture: expected 1 held after arm, got %d", in.held())
	}

	s.disarmTx(disarmShutdown) // the Stop path

	if got := in.held(); got != 0 {
		t.Errorf("after close: want 0 held, got %d", got)
	}
}

// Rule 8 — the held INTERVAL is logged. The file exists to reconstruct a suspected
// host-sleep event mid-run, and "inhibition held from T1 to T2" is exactly that
// fact — the fact that was not recorded, only the acquire FAILURE was (#9). Both
// ends are needed: an acquire line with no release line cannot bound the interval.
func TestIdleInhibit_LogsHeldInterval(t *testing.T) {
	in := &fakeInhibitor{}
	s := newInhibitTestService(in, &fakeKeyer{}, nil)
	sink, logger := newLogSink()
	s.log = logger

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if _, ok := sink.record(t, "desktop idle inhibited"); !ok {
		t.Fatal("acquiring the inhibition must log the start of the held interval")
	}

	if err := s.ArmTx(false); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if _, ok := sink.record(t, "releasing desktop idle inhibition"); !ok {
		t.Fatal("releasing the inhibition must log the end of the held interval")
	}
}

// Rule 7 — no inhibitor wired at all (headless, or any existing deployment)
// leaves arming completely unaffected. This is the default for every other test
// in the package, so a regression here breaks the suite broadly; it is stated
// explicitly so the intent is not merely implied by other tests passing.
func TestIdleInhibit_AbsentInhibitorIsInert(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)

	if err := s.ArmTx(true); err != nil {
		t.Fatalf("arm with no inhibitor wired: %v", err)
	}
	if !armedNow(s) {
		t.Error("want armed with no inhibitor wired")
	}
	if err := s.ArmTx(false); err != nil {
		t.Fatalf("disarm with no inhibitor wired: %v", err)
	}
}
