package bridge

// LC-1 — a panic in an RF-safety worker must NOT exit the process (which does not prove a
// CAT-keyed rig dropped PTT). Each worker recovers under safego, records a NAMED structured
// panic, keeps the WaitGroup balanced (Stop still returns), and lands the ruled safe state:
//
//   - defensive unkey / stuck-tx unkey: TX stays UNAVAILABLE (TxReady()==false) and the
//     durable TX alarm is active — no idle/recovery-success without positive evidence (AC-3/4);
//   - the two bounded workers consume their attempt budget UP FRONT, so an always-panicking
//     seam walks the budget down and STOPS rather than looping forever;
//   - after Stop returns, no worker mutates state (AC-2, covered by a clean Stop under -race).

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// rfSafetyTestService builds a bridge Service with a captured log buffer (for the onPanic
// record) and the minimal TX machinery + a run context so Stop can cancel + drain.
func rfSafetyTestService(t *testing.T) (*Service, *fakeSerial, *syncBuf) {
	t.Helper()
	s, buf := newIdentityLogTestService(t, "yaesu-ftdx10")
	f := newFakeSerial()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.activeClient = f
	s.identityConfirmed = true
	s.runCtx = ctx
	s.cancel = cancel
	s.mu.Unlock()
	t.Cleanup(func() { _ = s.Stop() }) // cancels ctx, waits the WaitGroup — hangs if unbalanced
	return s, f, buf
}

func (s *Service) alarmActiveForTest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txAlarmActive
}

func TestDefensiveUnkeyWorker_PanicRecoversAndLatchesSafeState(t *testing.T) {
	s, f, buf := rfSafetyTestService(t)
	def, _ := cat.Lookup("yaesu-ftdx10")

	defensiveUnkeyPanicForTest = func() { panic("boom: defensive unkey") }
	defer func() { defensiveUnkeyPanicForTest = nil }()

	s.beginDefensiveRecovery(def, f)

	// AC-1: a named structured panic record.
	waitFor(t, func() bool {
		return countLines(buf, "subsystem goroutine panicked") >= 1 &&
			countLines(buf, "bridge.defensiveUnkey") >= 1
	}, "no structured defensive-unkey panic record")

	// AC-3/4: the safe state — TX unavailable and the durable alarm active.
	waitFor(t, s.alarmActiveForTest, "defensive-unkey panic did not latch the TX alarm")
	if s.TxReady() {
		t.Error("TxReady()==true after a defensive-unkey panic; TX must stay blocked")
	}
}

func TestAlarmProbeWorker_PanicRetainsAlarmAndConsumesBudget(t *testing.T) {
	s, _, buf := rfSafetyTestService(t)

	// panics is written on the probe worker goroutine (the injected seam) and read on
	// this test goroutine — atomic, or the -race detector flags the counter (CI red on
	// a63523ba).
	var panics atomic.Int64
	alarmProbePanicForTest = func() { panics.Add(1); panic("boom: alarm probe") }
	defer func() { alarmProbePanicForTest = nil }()

	// Raise an alarm → startAlarmProbes launches the probe, which panics on every attempt.
	s.raiseTxAlarm(TxAlarmUnconfirmed)

	// AC-1 + budget: the always-panicking probe records a panic and STOPS after consuming
	// its bounded budget (never an infinite loop). We give it well over the delay budget.
	waitFor(t, func() bool { return countLines(buf, "bridge.txAlarmProbe") >= 1 }, "no alarm-probe panic record")
	waitFor(t, func() bool { return int(panics.Load()) >= txAlarmProbeAttempts }, "probe did not walk its full attempt budget")
	// It must not exceed the budget (no fresh full run / panic loop). Give it a moment to
	// prove it has stopped, then confirm it did not keep panicking.
	time.Sleep(50 * time.Millisecond)
	if got := int(panics.Load()); got > txAlarmProbeAttempts {
		t.Errorf("alarm probe panicked %d times, want exactly the budget %d (no loop)", got, txAlarmProbeAttempts)
	}
	// AC-4: the alarm is RETAINED across the panics (probeTxRecovery never cleared it).
	if !s.alarmActiveForTest() {
		t.Error("TX alarm was cleared by an alarm-probe panic; it must be retained")
	}
}

func TestStuckUnkeyWorker_PanicRetainsSafeStateAndConsumesBudget(t *testing.T) {
	s, _, buf := rfSafetyTestService(t)

	// Pre-arm the alarm + uncertainty (the state retryUnkeyStillKeyed runs under), so the
	// panic policy's idempotent raiseTxAlarm doesn't bump the generation and resume works.
	s.mu.Lock()
	s.txAlarmActive = true
	s.txUncertain = true
	s.txAlarmProbeGen = 7
	s.mu.Unlock()

	// panics: written on the stuck-unkey worker goroutine, read here — atomic (see the
	// alarm-probe test above; same -race cause).
	var panics atomic.Int64
	stuckUnkeyPanicForTest = func() { panics.Add(1); panic("boom: stuck-tx unkey") }
	defer func() { stuckUnkeyPanicForTest = nil }()

	s.retryUnkeyStillKeyed()

	waitFor(t, func() bool { return countLines(buf, "bridge.stuckTxUnkey") >= 1 }, "no stuck-unkey panic record")
	// Budget consumed up front → walks down and stops (no infinite loop).
	waitFor(t, func() bool { return int(panics.Load()) >= txStopRetryAttempts }, "stuck-unkey did not walk its full attempt budget")
	time.Sleep(50 * time.Millisecond)
	if got := int(panics.Load()); got > txStopRetryAttempts {
		t.Errorf("stuck-unkey panicked %d times, want exactly the budget %d (no loop)", got, txStopRetryAttempts)
	}
	// AC-3/4: TX stays blocked, alarm retained.
	if s.TxReady() {
		t.Error("TxReady()==true after a stuck-unkey panic; TX must stay blocked")
	}
	if !s.alarmActiveForTest() {
		t.Error("TX alarm was cleared by a stuck-unkey panic; it must be retained")
	}
}
