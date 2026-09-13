package bridge

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// The first "still keyed" answer of a confirmation cycle re-sends the stop and
// asks again BEFORE alarming (W-0011, operator ruling 2026-09-13): 14 logged
// occurrences since 2026-07-21 all cleared on the daemon's own re-sent stop
// within a second, and every one showed the operator a red banner for that
// second. The alarm still stands on a SECOND "1" or on the cycle's original
// confirm timeout — the safety bound is unchanged.

// drainTxAlarms collects every tx-alarm event published so far.
func drainTxAlarms(ch <-chan Event) []TxAlarmPayload {
	var out []TxAlarmPayload
	for {
		select {
		case ev := <-ch:
			if ev.Name == EventTxAlarm {
				out = append(out, ev.Payload.(TxAlarmPayload))
			}
		default:
			return out
		}
	}
}

// waitForReassert waits until the daemon has re-sent the stop AND re-asked the
// rig, in that order, after a first "still keyed" answer.
func waitForReassert(t *testing.T, fake *fakeSerial) {
	t.Helper()
	waitFor(t, func() bool {
		writes := fake.recordedWrites()
		stopAt := -1
		for i, w := range writes {
			if bytes.Equal(w, []byte("TX0;")) && stopAt < 0 {
				stopAt = i
			}
			if stopAt >= 0 && i > stopAt && bytes.Equal(w, []byte("TX;")) {
				return true
			}
		}
		return false
	}, "the first still-keyed answer did not re-send the stop and re-query the rig")
}

// answerStillKeyedTwice drives a rig that ignores BOTH the unkey and the
// pre-alarm re-sent stop: "1", the re-send, "1" again — the point at which the
// alarm and the stuck-TX burst begin, as they did on the first "1" before W-0011.
func answerStillKeyedTwice(t *testing.T, s *Service, fake *fakeSerial) {
	t.Helper()
	s.observeTxStatus("1")
	waitForReassert(t, fake)
	s.observeTxStatus("1")
}

func TestStillKeyed_FirstAnswerReassertsTheStopWithoutAlarming(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	ch, unsub := s.Subscribe()
	defer unsub()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, fake) // a real cycle: timer armed, generation set
	s.mu.Lock()
	gen := s.txConfirmGen
	s.mu.Unlock()

	s.observeTxStatus("1")
	waitForReassert(t, fake)

	if s.TxAlarmActive() {
		t.Fatal("the FIRST still-keyed answer raised the alarm; it must re-send the stop and ask again first")
	}
	if !s.TxUncertain() {
		t.Fatal("re-sending the stop must not relax tx-uncertainty — only the rig's RX answer may")
	}
	if got := drainTxAlarms(ch); len(got) != 0 {
		t.Fatalf("tx-alarm events published on the first still-keyed answer: %+v", got)
	}
	// The cycle is the SAME cycle: no new generation, no re-armed timeout. This
	// is what keeps the existing bound (alarm at latest confirmTimeout after
	// the original unkey) intact.
	s.mu.Lock()
	sameGen := s.txConfirmGen == gen
	pending := s.txConfirmDone != nil
	s.mu.Unlock()
	if !sameGen {
		t.Fatal("the re-sent stop started a new confirmation cycle, which re-arms the confirm timeout and extends the alarm bound")
	}
	if !pending {
		t.Fatal("the confirmation cycle resolved without any evidence — the release path would restore mode/power on an unconfirmed rig")
	}

	// The rig obeys the re-sent stop: positive RX confirmation, never an alarm.
	s.observeTxStatus("0")
	if s.TxUncertain() || s.TxAlarmActive() {
		t.Fatal("the rig's RX answer must confirm idle and leave no alarm")
	}
	if got := drainTxAlarms(ch); len(got) != 0 {
		t.Fatalf("an alarm event was published although the rig obeyed the re-sent stop: %+v", got)
	}
	if !s.waitTxConfirm() {
		t.Fatal("waitTxConfirm must report positive confirmation after the rig obeyed the re-sent stop")
	}
}

func TestStillKeyed_SecondAnswerAlarms(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	ch, unsub := s.Subscribe()
	defer unsub()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, fake)

	s.observeTxStatus("1")
	waitForReassert(t, fake)
	if s.TxAlarmActive() {
		t.Fatal("alarm raised on the first still-keyed answer, before the re-sent stop was given a chance")
	}
	s.observeTxStatus("1") // the rig ignored the re-sent stop too

	if !s.TxAlarmActive() {
		t.Fatal("a SECOND still-keyed answer must raise the alarm")
	}
	got := drainTxAlarms(ch)
	if len(got) != 1 || !got[0].Active || got[0].Code != TxAlarmStillKeyed {
		t.Fatalf("want exactly one active tx_still_keyed alarm event, got %+v", got)
	}
	// The stuck-TX retry burst runs as before: more stops go out.
	before := countUnkeys(fake)
	waitFor(t, func() bool { return countUnkeys(fake) > before },
		"the alarm did not start the stuck-TX re-unkey burst")
}

func TestStillKeyed_ReassertKeepsTheOriginalConfirmTimeout(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	s.confirmTimeout = 40 * time.Millisecond
	ch, unsub := s.Subscribe()
	defer unsub()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, fake)
	armed := time.Now()

	s.observeTxStatus("1")
	waitForReassert(t, fake)
	// Silence after the re-sent stop: the ORIGINAL cycle's timeout must still
	// raise tx_unconfirmed — the re-send bought no extra time.
	waitFor(t, s.TxAlarmActive, "no alarm after the confirm timeout elapsed with the rig silent")
	if elapsed := time.Since(armed); elapsed > s.confirmTimeout+150*time.Millisecond {
		t.Fatalf("alarm took %v; the re-sent stop must not extend the %v confirm bound", elapsed, s.confirmTimeout)
	}
	got := drainTxAlarms(ch)
	if len(got) != 1 || got[0].Code != TxAlarmUnconfirmed {
		t.Fatalf("want one tx_unconfirmed alarm from the original timeout, got %+v", got)
	}
}

func TestStillKeyed_ReassertWriteFailureAlarmsAtOnce(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	ch, unsub := s.Subscribe()
	defer unsub()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, fake)
	fake.setWriteErr(errors.New("port gone"))

	s.observeTxStatus("1")
	waitFor(t, s.TxAlarmActive, "a still-keyed rig whose re-sent stop cannot be written must alarm now")
	got := drainTxAlarms(ch)
	if len(got) != 1 || got[0].Code != TxAlarmStillKeyed {
		t.Fatalf("want one tx_still_keyed alarm when the re-sent stop cannot be written, got %+v", got)
	}
}

func TestStillKeyed_NoReassertWhileAlreadyAlarmed(t *testing.T) {
	s, fake := newAlarmProbeService(t, 1)
	s.raiseTxAlarm(TxAlarmUnconfirmed)
	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	before := countUnkeys(fake)

	s.observeTxStatus("1")
	// The standing alarm's own retry burst may write stops; what must NOT
	// happen is the pre-alarm reassert path treating this as a fresh cycle
	// and clearing anything.
	if !s.TxAlarmActive() {
		t.Fatal("a still-keyed answer while alarmed must leave the alarm standing")
	}
	waitFor(t, func() bool { return countUnkeys(fake) > before }, "no stop re-sent to a rig still keyed under a standing alarm")
}

// gatedStopClient holds the FIRST "TX0;" write open until gate closes, then
// either fails it (failAfterGate) or lets it through. It forces the interleaving
// from the 02823278 review: the reassert worker is mid-write while the rig's
// RX answer lands and the next key starts.
type gatedStopClient struct {
	*fakeSerial
	gate          chan struct{}
	failAfterGate error
	held          bool
	returned      chan struct{} // closed once the held write has returned
	releaseOnce   sync.Once
	mu            sync.Mutex
}

// release opens the gate once; safe to call after the test already did.
func (g *gatedStopClient) release() {
	g.releaseOnce.Do(func() { close(g.gate) })
}

func (g *gatedStopClient) WriteCommandBytes(ctx context.Context, cmd []byte) error {
	g.mu.Lock()
	first := bytes.Equal(cmd, []byte("TX0;")) && !g.held
	if first {
		g.held = true
	}
	g.mu.Unlock()
	if first {
		<-g.gate
		defer close(g.returned)
		if g.failAfterGate != nil {
			return g.failAfterGate
		}
	}
	return g.fakeSerial.WriteCommandBytes(ctx, cmd)
}

func newGatedReassertService(t *testing.T, failAfterGate error) (*Service, *fakeSerial, *gatedStopClient) {
	t.Helper()
	s, fake := newAlarmProbeService(t, 1)
	gated := &gatedStopClient{fakeSerial: fake, gate: make(chan struct{}), returned: make(chan struct{}), failAfterGate: failAfterGate}
	// Registered AFTER newAlarmProbeService's cleanup, so it runs BEFORE that
	// cleanup's wg.Wait (LIFO): a test that fails while the gate is still shut
	// must not leave the worker blocked in its write and hang the suite.
	t.Cleanup(func() { gated.release() })
	s.mu.Lock()
	s.activeClient = gated
	s.lastMode = "USB"
	s.lastPower = 100
	s.tuneMaxDuration = time.Hour
	s.tuneRestoreSettle = 5 * time.Millisecond
	s.mu.Unlock()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, gated)
	s.observeTxStatus("1") // worker starts and blocks inside its TX0 write
	waitFor(t, func() bool { gated.mu.Lock(); defer gated.mu.Unlock(); return gated.held },
		"the re-sent stop never reached the client")
	return s, fake, gated
}

// indexOfWrite finds the first write CONTAINING want: the tune key goes out as
// one batched line ("MD09;PC020;TX1;"), so an exact match would miss it.
func indexOfWrite(writes [][]byte, want string) int {
	for i, w := range writes {
		if bytes.Contains(w, []byte(want)) {
			return i
		}
	}
	return -1
}

// A key must not overtake a re-sent stop still in flight: the worker passed
// its checks, the rig's RX answer then cleared uncertainty, the release
// finished and a new key started — the stale TX0 would cut the new carrier.
func TestStillKeyed_KeyWaitsForTheInFlightReassertedStop(t *testing.T) {
	s, fake, gated := newGatedReassertService(t, nil)
	s.observeTxStatus("0") // RX confirmed while the stop is still mid-write
	if s.TxUncertain() {
		t.Fatal("precondition: the RX answer must clear uncertainty")
	}

	done := make(chan error, 1)
	go func() { done <- s.StartTune(context.Background()) }()
	time.Sleep(40 * time.Millisecond)
	if indexOfWrite(fake.recordedWrites(), "TX1;") >= 0 {
		t.Fatal("a key was written while the re-sent stop was still in flight — the stale TX0 would cut the new carrier")
	}
	gated.release()
	if err := <-done; err != nil {
		t.Fatalf("StartTune after the stop landed: %v", err)
	}
	writes := fake.recordedWrites()
	stop, key := indexOfWrite(writes, "TX0;"), indexOfWrite(writes, "TX1;")
	if stop < 0 || key < 0 || stop > key {
		t.Fatalf("want the re-sent stop before the key, got writes %q", writes)
	}
}

// A delayed failure of the re-sent stop belongs to the cycle that sent it: once
// the rig has confirmed RX (or a new cycle began) it must not raise an alarm
// or start a retry burst against whatever client is active now.
func TestStillKeyed_StaleReassertFailureDoesNotAlarm(t *testing.T) {
	s, fake, gated := newGatedReassertService(t, errors.New("port gone"))
	ch, unsub := s.Subscribe()
	defer unsub()
	s.observeTxStatus("0") // cycle resolved before the write fails
	gated.release()
	<-gated.returned                  // the failed write has returned to the worker
	time.Sleep(30 * time.Millisecond) // and the worker has had time to react to it

	if s.TxAlarmActive() || s.TxUncertain() {
		t.Fatal("a stale re-send failure alarmed a cycle the rig had already confirmed idle")
	}
	if got := drainTxAlarms(ch); len(got) != 0 {
		t.Fatalf("alarm events from a stale re-send failure: %+v", got)
	}
	if n := countUnkeys(fake); n != 0 {
		t.Fatalf("a stale re-send failure started a retry burst (%d stops written)", n)
	}
}

// At most one re-send worker is in flight, so the completion channel always
// has one owner (ae8cb9a5 review P2): a NEW cycle (defensive recovery's
// beginTxConfirmIfUncertain resets the per-cycle flag) followed by another "1"
// while the first worker is still mid-write must not start a second worker
// that overwrites the channel — the first worker's cleanup would then close
// the wrong channel and a key waiting on the orphaned one would hang under
// keyMu. The second "1" takes the alarm path instead.
func TestStillKeyed_OneReassertWorkerInFlight(t *testing.T) {
	s, _, gated := newGatedReassertService(t, nil)
	s.mu.Lock()
	first := s.txReassertDone
	s.mu.Unlock()
	if first == nil {
		t.Fatal("precondition: a worker is in flight")
	}

	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, gated) // a fresh cycle while worker A is still mid-write
	s.observeTxStatus("1")       // would start worker B before the fix

	s.mu.Lock()
	current := s.txReassertDone
	s.mu.Unlock()
	if current != first {
		t.Fatal("a second re-send worker replaced the in-flight worker's completion channel")
	}
	if !s.TxAlarmActive() {
		t.Fatal("a still-keyed answer while a re-sent stop is already in flight must take the alarm path")
	}

	// A key that waited on the first channel is released by worker A's exit —
	// and by nothing else.
	released := make(chan struct{})
	go func() { <-first; close(released) }()
	select {
	case <-released:
		t.Fatal("the completion channel closed before the in-flight stop landed")
	case <-time.After(30 * time.Millisecond):
	}
	gated.release()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("worker A's exit did not close its own completion channel")
	}
}
