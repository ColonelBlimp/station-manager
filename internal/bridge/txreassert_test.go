package bridge

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
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

// waitForTxAlarm blocks until the next tx-alarm EVENT arrives, or fails after a
// second. This is the barrier for an alarm raised on another goroutine (the
// confirm timer, the reassert worker): the alarm FLAG is set under s.mu and the
// event is published after the lock is released, so a test that polls the flag
// and then drains the channel non-blockingly can observe the flag before the
// event has landed (15 failures in 150 runs under -race, 2026-09-17). The event
// is published only after the flag, so receiving it proves both.
func waitForTxAlarm(t *testing.T, ch <-chan Event, msg string) TxAlarmPayload {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("event subscription closed before a tx-alarm event arrived")
			}
			if ev.Name == EventTxAlarm {
				return ev.Payload.(TxAlarmPayload)
			}
		case <-deadline:
			t.Fatal(msg)
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
	// raise tx_unconfirmed — the re-send bought no extra time. Wait for the
	// EVENT, which the timer publishes after setting the flag (see waitForTxAlarm).
	got := waitForTxAlarm(t, ch, "no alarm after the confirm timeout elapsed with the rig silent")
	if elapsed := time.Since(armed); elapsed > s.confirmTimeout+150*time.Millisecond {
		t.Fatalf("alarm took %v; the re-sent stop must not extend the %v confirm bound", elapsed, s.confirmTimeout)
	}
	if !got.Active || got.Code != TxAlarmUnconfirmed {
		t.Fatalf("want an active tx_unconfirmed alarm from the original timeout, got %+v", got)
	}
	if !s.TxAlarmActive() {
		t.Fatal("the alarm event was published but the alarm flag is not set")
	}
	if extra := drainTxAlarms(ch); len(extra) != 0 {
		t.Fatalf("want exactly one alarm event, got another after it: %+v", extra)
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
	// Raised on the reassert worker's goroutine: wait for the event, not the flag.
	got := waitForTxAlarm(t, ch, "a still-keyed rig whose re-sent stop cannot be written must alarm now")
	if !got.Active || got.Code != TxAlarmStillKeyed {
		t.Fatalf("want an active tx_still_keyed alarm when the re-sent stop cannot be written, got %+v", got)
	}
	if !s.TxAlarmActive() {
		t.Fatal("the alarm event was published but the alarm flag is not set")
	}
	if extra := drainTxAlarms(ch); len(extra) != 0 {
		t.Fatalf("want exactly one alarm event, got another after it: %+v", extra)
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

// newReassertLogService is newAlarmProbeService with a REAL logger writing to a
// buffer, so the record the absorbed event leaves in smd.log can be asserted on.
func newReassertLogService(t *testing.T) (*Service, *fakeSerial, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, logging.NewForWriter(buf))
	fake := newFakeSerial()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.activeClient = fake
	s.identityConfirmed = true
	s.runCtx = ctx
	s.mu.Unlock()
	t.Cleanup(func() {
		cancel()
		s.wg.Wait()
	})
	return s, fake, buf
}

// The absorbed event's record (operator ruling 2026-09-16, W-0011): the two
// on-air occurrences could not tell whether the rig obeyed the FIRST stop late
// or the RE-SENT one, and the old wording asserted the first — a causal claim
// the evidence does not establish. The line now describes what was observed and
// carries the three timestamps that were missing from the analysis (unkey
// written, still-keyed answer, stop re-sent) with the millisecond spans between
// them and the idle confirmation, so the next occurrence is read straight from
// the log. The stamps narrow the two explanations; they are not proof of either.
func TestStillKeyed_AbsorbedEventIsLoggedNeutrallyWithTimings(t *testing.T) {
	s, fake, buf := newReassertLogService(t)
	def, _ := cat.Lookup("yaesu-ftdx10")
	before := time.Now()
	s.beginTxConfirmAfterUnkey(def, fake, time.Now())
	s.observeTxStatus("1")
	waitForReassert(t, fake)
	s.observeTxStatus("0")
	after := time.Now()

	recs := matching(t, buf, "confirmed idle")
	if len(recs) != 1 {
		t.Fatalf("want exactly one confirmed-idle record, got %d: %v", len(recs), recs)
	}
	rec := recs[0]
	msg, _ := rec["message"].(string)
	if rec["level"] != "warn" || rec["stop_reasserted"] != true {
		t.Fatalf("the absorbed event must stay a warn with stop_reasserted=true: %v", rec)
	}
	if bytes.Contains([]byte(msg), []byte("did not obey")) {
		t.Fatalf("the record asserts the rig ignored the first stop, which the evidence does not establish: %q", msg)
	}
	for _, want := range []string{"still-keyed to the first query", "idle to a later one", "no alarm"} {
		if !bytes.Contains([]byte(msg), []byte(want)) {
			t.Fatalf("record %q does not describe the observation %q", msg, want)
		}
	}
	// Whether the re-sent stop's write-return STAMP existed when the idle
	// answer was handled is a field, not a clause of the message: the idle
	// answer can be handled before the stamp lands (next test).
	if rec["resend_stamped_before_idle"] != true {
		t.Fatalf("the re-send was stamped before the idle answer here; want resend_stamped_before_idle=true: %v", rec)
	}

	// The three stamps, in order, all inside the test's own window.
	stamps := []string{"unkey_written_at", "still_keyed_answer_at", "stop_resent_at"}
	var prev time.Time
	for i, key := range stamps {
		raw, _ := rec[key].(string)
		ts, err := time.Parse(logging.TimeFieldFormat, raw)
		if err != nil {
			t.Fatalf("%s = %q is not a log timestamp: %v", key, raw, err)
		}
		if ts.Before(before.Truncate(time.Millisecond)) || ts.After(after) {
			t.Fatalf("%s = %s lies outside the test window [%s, %s]", key, ts, before, after)
		}
		if i > 0 && ts.Before(prev) {
			t.Fatalf("%s (%s) precedes %s (%s)", key, ts, stamps[i-1], prev)
		}
		prev = ts
	}
	// The spans a reader would otherwise compute by hand, all non-negative.
	for _, key := range []string{"answer_after_unkey_ms", "resend_after_answer_ms", "idle_after_unkey_ms", "idle_after_resend_ms"} {
		v, ok := rec[key].(float64)
		if !ok || v < 0 {
			t.Fatalf("%s missing or negative on the record: %v", key, rec[key])
		}
	}
}

// The idle answer can be handled while the re-sent stop is still MID-WRITE (the
// interleaving TestStillKeyed_KeyWaitsForTheInFlightReassertedStop guards for
// the key path). The record must then not read as "idle after the re-sent
// stop": the re-send stamp is absent, its spans are -1, and the flag says the
// stamp did not exist yet — the two status answers are all that was observed.
// (The flag is about the STAMP: a write that returned but was not yet stamped
// reads the same way, which is why it is not named "landed".)
func TestStillKeyed_IdleBeforeTheResentStopIsStampedIsRecordedAsSuch(t *testing.T) {
	s, fake, buf := newReassertLogService(t)
	gated := &gatedStopClient{fakeSerial: fake, gate: make(chan struct{}), returned: make(chan struct{})}
	t.Cleanup(func() { gated.release() }) // LIFO: before the service cleanup's wg.Wait
	s.mu.Lock()
	s.activeClient = gated
	s.mu.Unlock()
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirmAfterUnkey(def, gated, time.Now())
	s.observeTxStatus("1") // the worker starts and blocks inside its TX0 write
	waitFor(t, func() bool { gated.mu.Lock(); defer gated.mu.Unlock(); return gated.held },
		"the re-sent stop never reached the client")
	s.observeTxStatus("0") // idle while the stop is still mid-write
	gated.release()
	<-gated.returned

	recs := matching(t, buf, "confirmed idle")
	if len(recs) != 1 {
		t.Fatalf("want exactly one confirmed-idle record, got %d: %v", len(recs), recs)
	}
	rec := recs[0]
	msg, _ := rec["message"].(string)
	if rec["level"] != "warn" || rec["stop_reasserted"] != true {
		t.Fatalf("still the absorbed event's warn: %v", rec)
	}
	if bytes.Contains([]byte(msg), []byte("after the re-sent stop")) {
		t.Fatalf("the record claims idle followed the re-sent stop, which had not been stamped: %q", msg)
	}
	if rec["resend_stamped_before_idle"] != false {
		t.Fatalf("want resend_stamped_before_idle=false: %v", rec)
	}
	if rec["stop_resent_at"] != "" {
		t.Fatalf("want an empty stop_resent_at, got %v", rec["stop_resent_at"])
	}
	for _, key := range []string{"resend_after_answer_ms", "idle_after_resend_ms"} {
		if v, _ := rec[key].(float64); v != -1 {
			t.Fatalf("%s = %v, want -1 (no re-send stamp)", key, rec[key])
		}
	}
	// The stamps that WERE observed are still there.
	for _, key := range []string{"unkey_written_at", "still_keyed_answer_at"} {
		if raw, _ := rec[key].(string); raw == "" {
			t.Fatalf("%s missing on the record: %v", key, rec)
		}
	}
}

// unkey_written_at is the tx_off write's RETURN, captured at the call site and
// handed in with the cycle. A cycle opened WITHOUT an unkey write (the encode
// failure paths; here, the bare beginTxConfirm) has no such moment, and the
// record must say so — an empty stamp and -1 spans — rather than substitute the
// cycle's own opening for it.
func TestStillKeyed_CycleWithoutAnUnkeyWriteCarriesNoUnkeyStamp(t *testing.T) {
	s, fake, buf := newReassertLogService(t)
	def, _ := cat.Lookup("yaesu-ftdx10")
	s.beginTxConfirm(def, fake)
	s.observeTxStatus("1")
	waitForReassert(t, fake)
	s.observeTxStatus("0")

	recs := matching(t, buf, "confirmed idle")
	if len(recs) != 1 {
		t.Fatalf("want exactly one confirmed-idle record, got %d: %v", len(recs), recs)
	}
	rec := recs[0]
	if rec["unkey_written_at"] != "" {
		t.Fatalf("no unkey was written, yet unkey_written_at = %v", rec["unkey_written_at"])
	}
	for _, key := range []string{"answer_after_unkey_ms", "idle_after_unkey_ms"} {
		if v, _ := rec[key].(float64); v != -1 {
			t.Fatalf("%s = %v, want -1 (no unkey stamp)", key, rec[key])
		}
	}
}

// The production tune release hands the cycle the write's return: a tune keyed
// and released through the real path, whose rig answers still-keyed once, leaves
// a record whose unkey_written_at lies between the release call and the
// still-keyed answer.
func TestStillKeyed_TuneReleaseStampsTheUnkeyWriteReturn(t *testing.T) {
	s, fake, buf := newReassertLogService(t)
	s.mu.Lock()
	s.lastMode = "USB"
	s.lastPower = 100
	s.tuneMaxDuration = time.Hour
	s.tuneRestoreSettle = 5 * time.Millisecond
	s.mu.Unlock()
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	beforeRelease := time.Now()
	released := make(chan error, 1)
	go func() { released <- s.StopTune(context.Background()) }() // holds keyMu across waitTxConfirm
	// Wait for the cycle to be ARMED, not merely for the unkey to appear in the
	// fake's writes: the unkey write returns to the fake before the cycle sets
	// txUncertain, and a "1" injected in that gap is ignored. The status query
	// ("TX;") is written only after the cycle armed under the lock, so its
	// appearance is the barrier.
	waitFor(t, func() bool { return indexOfWrite(fake.recordedWrites(), "TX;") >= 0 && s.TxUncertain() },
		"the release never armed its confirmation cycle")
	s.observeTxStatus("1")
	waitForReassert(t, fake)
	s.observeTxStatus("0")
	if err := <-released; err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	recs := matching(t, buf, "confirmed idle")
	if len(recs) != 1 {
		t.Fatalf("want exactly one confirmed-idle record, got %d: %v", len(recs), recs)
	}
	rec := recs[0]
	raw, _ := rec["unkey_written_at"].(string)
	ts, err := time.Parse(logging.TimeFieldFormat, raw)
	if err != nil {
		t.Fatalf("unkey_written_at = %q is not a log timestamp: %v", raw, err)
	}
	answerRaw, _ := rec["still_keyed_answer_at"].(string)
	answerAt, _ := time.Parse(logging.TimeFieldFormat, answerRaw)
	if ts.Before(beforeRelease.Truncate(time.Millisecond)) || ts.After(answerAt) {
		t.Fatalf("unkey_written_at %s is not between the release call %s and the still-keyed answer %s", ts, beforeRelease, answerAt)
	}
}
