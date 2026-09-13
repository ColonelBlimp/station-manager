package bridge

import (
	"bytes"
	"errors"
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
