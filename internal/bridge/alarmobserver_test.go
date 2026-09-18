package bridge

import (
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// recordingAlarmObserver captures every seam call with its stamps.
type alarmCall struct {
	what     string // raise | clear | drive | drive-ok
	code     string
	raisedAt time.Time
	at       time.Time
}

type recordingAlarmObserver struct {
	mu    sync.Mutex
	calls []alarmCall
}

func (r *recordingAlarmObserver) add(c alarmCall) {
	r.mu.Lock()
	r.calls = append(r.calls, c)
	r.mu.Unlock()
}

func (r *recordingAlarmObserver) TxAlarmRaised(code string, at time.Time) {
	r.add(alarmCall{what: "raise", code: code, at: at})
}

func (r *recordingAlarmObserver) TxAlarmCleared(code string, raisedAt, at time.Time) {
	r.add(alarmCall{what: "clear", code: code, raisedAt: raisedAt, at: at})
}

func (r *recordingAlarmObserver) DriveAlarmRaised(code string, at time.Time) {
	r.add(alarmCall{what: "drive", code: code, at: at})
}

func (r *recordingAlarmObserver) DriveAlarmRecovered(code string, at time.Time) {
	r.add(alarmCall{what: "drive-ok", code: code, at: at})
}

func (r *recordingAlarmObserver) snapshot() []alarmCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]alarmCall(nil), r.calls...)
}

func (r *recordingAlarmObserver) count(what string) int {
	n := 0
	for _, c := range r.snapshot() {
		if c.what == what {
			n++
		}
	}
	return n
}

// The seam is nil-safe by construction: an unwired service reports nil, and
// wiring is a plain injection under the service lock.
func TestAlarmObserver_UnwiredIsNilAndInjectionSticks(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	if s.alarmObserverSnapshot() != nil {
		t.Fatal("a fresh service must have no alarm observer")
	}
	obs := &recordingAlarmObserver{}
	s.SetAlarmObserver(obs)
	if got := s.alarmObserverSnapshot(); got != AlarmObserver(obs) {
		t.Fatalf("observer snapshot = %v, want the injected one", got)
	}
}

// AC2: the confirm timeout raises tx_unconfirmed with its stamp; the rig's RX
// answer clears it, and the clear carries the SAME code and the raise stamp the
// bridge retained — the hub payload for the clear stays empty, the observer's
// does not.
func TestAlarmObserver_TimeoutRaisesAndTheRxAnswerClearsWithTheRetainedIdentity(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	s.confirmTimeout = 40 * time.Millisecond
	obs := &recordingAlarmObserver{}
	s.SetAlarmObserver(obs)
	def, _ := cat.Lookup("yaesu-ftdx10")
	before := time.Now()
	s.beginTxConfirm(def, fake)

	waitFor(t, func() bool { return obs.count("raise") == 1 }, "the confirm timeout did not report a raised alarm")
	raise := obs.snapshot()[0]
	if raise.code != TxAlarmUnconfirmed || raise.at.Before(before) || raise.at.After(time.Now()) {
		t.Fatalf("raise = %+v, want tx_unconfirmed stamped now-ish", raise)
	}

	s.observeTxStatus("0")
	waitFor(t, func() bool { return obs.count("clear") == 1 }, "the RX answer did not report the clear")
	calls := obs.snapshot()
	clear := calls[len(calls)-1]
	if clear.code != TxAlarmUnconfirmed {
		t.Errorf("clear code = %q, want the raised code tx_unconfirmed (the hub's clear carries none)", clear.code)
	}
	if !clear.raisedAt.Equal(raise.at) {
		t.Errorf("clear raisedAt = %v, want the raise stamp %v", clear.raisedAt, raise.at)
	}
	if clear.at.Before(clear.raisedAt) {
		t.Errorf("clear at %v precedes raisedAt %v", clear.at, clear.raisedAt)
	}
	if s.TxAlarmActive() {
		t.Error("alarm still active after the RX answer")
	}
}

// A re-raise while already alarmed is not a second alarm: one raise, and the
// eventual clear names the FIRST code, which is the alarm that actually stood.
func TestAlarmObserver_ReRaiseWhileAlarmedReportsNothingAndTheClearNamesTheFirstCode(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	obs := &recordingAlarmObserver{}
	s.SetAlarmObserver(obs)

	s.raiseTxAlarm(TxAlarmStillKeyed)
	s.raiseTxAlarm(TxAlarmUnconfirmed) // already alarmed: no edge
	if got := obs.count("raise"); got != 1 {
		t.Fatalf("raises = %d, want 1 (a re-raise while alarmed is not a new alarm)", got)
	}
	s.confirmTxIdle("test")
	calls := obs.snapshot()
	if len(calls) != 2 || calls[1].what != "clear" || calls[1].code != TxAlarmStillKeyed {
		t.Fatalf("calls = %+v, want one raise then one clear naming tx_still_keyed", calls)
	}
	if !calls[1].raisedAt.Equal(calls[0].at) {
		t.Errorf("clear raisedAt = %v, want the raise stamp %v", calls[1].raisedAt, calls[0].at)
	}
	// A second clear with no alarm standing reports nothing.
	s.confirmTxIdle("test")
	if got := obs.count("clear"); got != 1 {
		t.Errorf("clears = %d, want 1 — an idle confirmation with no alarm standing is not a clear", got)
	}
}

// A positive RX answer can arrive after the raise has latched but before its
// after-unlock publish/observer call finishes. Both events must still reach the
// hub and recorder in state-transition order: raise, then clear.
func TestAlarmObserver_ConcurrentClearFollowsTheLatchedRaise(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	obs := &gatedAlarmObserver{
		recordingAlarmObserver: &recordingAlarmObserver{},
		raiseEntered:           make(chan struct{}),
		releaseRaise:           make(chan struct{}),
	}
	s.SetAlarmObserver(obs)
	raiseDone := make(chan struct{})
	go func() {
		s.raiseTxAlarm(TxAlarmStillKeyed)
		close(raiseDone)
	}()
	<-obs.raiseEntered // the raise is latched; its observer call is paused
	clearDone := make(chan struct{})
	go func() {
		s.confirmTxIdle("concurrent RX answer")
		close(clearDone)
	}()
	waitFor(t, func() bool { return !s.TxAlarmActive() }, "the RX answer did not clear the alarm state")
	// An unguarded clear finishes before the paused raise, reversing the rows.
	// A guarded clear waits outside s.mu until the raise has been published.
	clearFinishedEarly := false
	select {
	case <-clearDone:
		clearFinishedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(obs.releaseRaise)
	<-raiseDone
	<-clearDone
	if clearFinishedEarly {
		t.Error("the clear finished before the earlier raise was reported")
	}
	calls := obs.snapshot()
	if len(calls) != 2 || calls[0].what != "raise" || calls[1].what != "clear" {
		t.Fatalf("observer calls = %+v, want raise then clear", calls)
	}
	if !calls[1].raisedAt.Equal(calls[0].at) {
		t.Errorf("clear raisedAt = %v, want %v", calls[1].raisedAt, calls[0].at)
	}
}

type gatedAlarmObserver struct {
	*recordingAlarmObserver
	raiseEntered chan struct{}
	releaseRaise chan struct{}
}

func (o *gatedAlarmObserver) TxAlarmRaised(code string, at time.Time) {
	close(o.raiseEntered)
	<-o.releaseRaise
	o.recordingAlarmObserver.TxAlarmRaised(code, at)
}

// The daemon drains the recorder after bridge.Stop. Stop must therefore wait
// for a raise already latched and now inside its after-lock observer call;
// otherwise that fact can arrive after the recorder has sealed admission.
func TestAlarmObserver_StopWaitsForAnInFlightRaiseReport(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	obs := &gatedAlarmObserver{
		recordingAlarmObserver: &recordingAlarmObserver{},
		raiseEntered:           make(chan struct{}),
		releaseRaise:           make(chan struct{}),
	}
	s.SetAlarmObserver(obs)
	raiseDone := make(chan struct{})
	go func() {
		s.raiseTxAlarm(TxAlarmStillKeyed)
		close(raiseDone)
	}()
	<-obs.raiseEntered
	stopDone := make(chan error, 1)
	go func() { stopDone <- s.Stop() }()
	stoppedBeforeReport := false
	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
		stoppedBeforeReport = true
	case <-time.After(100 * time.Millisecond):
	}
	close(obs.releaseRaise)
	<-raiseDone
	if !stoppedBeforeReport {
		if err := <-stopDone; err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
	if stoppedBeforeReport {
		t.Error("Stop returned before the already-latched alarm reached its observer")
	}
	if got := obs.count("raise"); got != 1 {
		t.Errorf("raise reports = %d, want 1", got)
	}
}

type gatedDriveObserver struct {
	*recordingAlarmObserver
	raiseEntered chan struct{}
	releaseRaise chan struct{}
}

func (o *gatedDriveObserver) DriveAlarmRaised(code string, at time.Time) {
	close(o.raiseEntered)
	<-o.releaseRaise
	o.recordingAlarmObserver.DriveAlarmRaised(code, at)
}

// The drive-silence timer is not a pipeline worker. If it latched an alarm
// before Stop, its after-lock observer call must finish before the recorder
// drains, just like the TX-confirm timeout's call.
func TestAlarmObserver_StopWaitsForAnInFlightDriveReport(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	obs := &gatedDriveObserver{
		recordingAlarmObserver: &recordingAlarmObserver{},
		raiseEntered:           make(chan struct{}),
		releaseRaise:           make(chan struct{}),
	}
	s.SetAlarmObserver(obs)
	s.mu.Lock()
	s.ft8TxActive = true
	s.ft8TxGen = 1
	s.driveWatchArmed = true
	s.driveSilence = time.Millisecond
	s.driveLastMeterAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	raiseDone := make(chan struct{})
	go func() {
		s.checkDriveSilence(1)
		close(raiseDone)
	}()
	<-obs.raiseEntered
	stopDone := make(chan error, 1)
	go func() { stopDone <- s.Stop() }()
	stoppedBeforeReport := false
	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
		stoppedBeforeReport = true
	case <-time.After(100 * time.Millisecond):
	}
	close(obs.releaseRaise)
	<-raiseDone
	if !stoppedBeforeReport {
		if err := <-stopDone; err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
	if stoppedBeforeReport {
		t.Error("Stop returned before the already-latched drive alarm reached its observer")
	}
	if got := obs.count("drive"); got != 1 {
		t.Errorf("drive reports = %d, want 1", got)
	}
}

// A later healthy slot can decide recovery while the earlier timer's raise
// report is still after the lock. The recorder must see drive raise first.
func TestAlarmObserver_DriveRecoveryFollowsAnInFlightRaise(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	obs := &gatedDriveObserver{
		recordingAlarmObserver: &recordingAlarmObserver{},
		raiseEntered:           make(chan struct{}),
		releaseRaise:           make(chan struct{}),
	}
	s.SetAlarmObserver(obs)
	s.mu.Lock()
	s.ft8TxActive = true
	s.ft8TxGen = 1
	s.driveWatchArmed = true
	s.driveSilence = time.Millisecond
	s.driveLastMeterAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	raiseDone := make(chan struct{})
	go func() {
		s.checkDriveSilence(1)
		close(raiseDone)
	}()
	<-obs.raiseEntered
	s.finishFt8Tx() // the alarming slot cannot be its own recovery

	// A later, healthy watched slot with a sealed 2 ms measurement and no
	// silence is positive recovery evidence. No rig command is needed here.
	s.mu.Lock()
	s.ft8TxActive = true
	s.driveWatchArmed = true
	s.driveAlarmed = false
	s.meterGapWindowAt = time.Now().Add(-2 * time.Millisecond)
	s.meterGapSealed = true
	s.meterGapKeyedFor = 2 * time.Millisecond
	s.meterGapMax = 0
	s.mu.Unlock()
	recoveryDone := make(chan struct{})
	go func() {
		s.finishFt8Tx()
		close(recoveryDone)
	}()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.driveAlarmStanding
	}, "the healthy slot did not decide recovery")
	recoveryFinishedEarly := false
	select {
	case <-recoveryDone:
		recoveryFinishedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(obs.releaseRaise)
	<-raiseDone
	<-recoveryDone
	if recoveryFinishedEarly {
		t.Error("recovery finished before the earlier drive raise was reported")
	}
	calls := obs.snapshot()
	if len(calls) != 2 || calls[0].what != "drive" || calls[1].what != "drive-ok" {
		t.Fatalf("observer calls = %+v, want drive raise then recovery", calls)
	}
}

// AC2, drive half: a silent keyed slot raises drive_no_output; a later healthy
// transmission reports its recovery with the same code.
func TestAlarmObserver_DriveAlarmRaisedThenRecovered(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	obs := &recordingAlarmObserver{}
	s.SetAlarmObserver(obs)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return obs.count("drive") == 1 }, "the drive alarm was not reported")
	s.finishFt8Tx()
	healthySlot(t, s, silence)
	waitFor(t, func() bool { return obs.count("drive-ok") == 1 }, "the drive recovery was not reported")

	calls := obs.snapshot()
	if calls[0].code != DriveAlarmNoOutput || calls[len(calls)-1].code != DriveAlarmNoOutput {
		t.Errorf("calls = %+v, want drive_no_output on both", calls)
	}
	if calls[0].at.IsZero() || calls[len(calls)-1].at.Before(calls[0].at) {
		t.Errorf("stamps out of order: %+v", calls)
	}
}
