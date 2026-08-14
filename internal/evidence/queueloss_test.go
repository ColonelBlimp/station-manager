package evidence

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// L3: a slot dropped because the WRITER QUEUE is full is backpressure, not a write
// failure. The confusable state this breaks: a backed-up writer vs a failing one.
// The durable loss record must read evidence_queue_full (never writer_error, since
// no write was attempted), and the operator must see a bounded warn (episode total +
// queue depth/capacity) plus a recovery summary once the writer catches up.
func TestCaptureSlot_QueueFull_ClassifiedAndLoggedAsBackpressure(t *testing.T) {
	oldQ, oldD := writerQueueSize, writerDelay
	writerQueueSize, writerDelay = 1, 50*time.Millisecond // stall the writer so the queue fills
	defer func() { writerQueueSize, writerDelay = oldQ, oldD }()

	var buf bytes.Buffer
	cfg := testConfig(t, true)
	s := newRunningLogged(t, cfg, &buf)

	for i := 0; i < 12; i++ { // burst faster than the writer drains → drops
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeNoDecode})
	}
	drain(t, s)
	if s.Status().DroppedSlots == 0 {
		t.Fatal("fixture: no drops occurred")
	}

	// Durable record: backpressure, not a write failure.
	db := openRaw(t, cfg.Path)
	reasons := map[string]bool{}
	rows, err := db.Query(`SELECT DISTINCT reason FROM loss_intervals`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		reasons[r] = true
	}
	_ = rows.Close()
	if !reasons[lossReasonQueueFull] {
		t.Fatalf("queue-full drop must be recorded as %q; got reasons %v", lossReasonQueueFull, reasons)
	}
	if reasons[lossReasonWriter] {
		t.Errorf("no write was attempted — a queue-full drop must not be recorded as %q", lossReasonWriter)
	}

	// Bounded backpressure warn carrying reason + queue depth/capacity.
	warns := rhLines(&buf, "writer queue full")
	if len(warns) == 0 || !strings.Contains(warns[0], `"reason":"evidence_queue_full"`) ||
		!strings.Contains(warns[0], `"queue_capacity":1`) {
		t.Fatalf("want a queue-full warn with reason + capacity, got: %q", buf.String())
	}
	// Recovery summary once the writer caught up.
	rec := rhLines(&buf, "writer queue recovered")
	if len(rec) == 0 || !strings.Contains(rec[0], `"total_dropped"`) || !strings.Contains(rec[0], `"episode_seconds"`) {
		t.Fatalf("want a recovery summary with total dropped + duration, got: %q", buf.String())
	}
}

// L3 recovery is DRAINAGE-based, not "any successful persist": under sustained
// overload the writer keeps persisting slots while the queue stays full and
// producers keep dropping. A persist that leaves slots still queued must NOT end the
// episode — otherwise the exponential schedule resets and re-warns at 1 every slot,
// and a spurious "recovered" is logged mid-overload. The confusable state: writer
// making progress vs overload actually over.
//
// The writer is serial, so when processSlot(A) runs its recovery check any slot
// queued behind A keeps len(s.ch) > 0; waiting for the channel to drain to empty is
// the deterministic signal that the writer has TAKEN the next in-flight slot (and so
// has fully finished the previous one, recovery check included).
func TestQueueLoss_PersistWithNonEmptyQueueDoesNotRecover(t *testing.T) {
	oldQ, oldD := writerQueueSize, writerDelay
	writerQueueSize, writerDelay = 1, 100*time.Millisecond
	defer func() { writerQueueSize, writerDelay = oldQ, oldD }()

	var buf bytes.Buffer
	cfg := testConfig(t, true)
	s := newRunningLogged(t, cfg, &buf)

	// waitTaken blocks until the writer has received the in-flight slot (channel
	// empty), i.e. it is now inside processSlot for it and asleep on writerDelay.
	waitTaken := func() {
		deadline := time.Now().Add(2 * time.Second)
		for len(s.ch) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("writer did not take the in-flight slot")
			}
			time.Sleep(time.Millisecond)
		}
	}
	feed := func(sec int) {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(sec), Outcome: OutcomeNoDecode})
	}

	feed(0)     // slot0 → writer takes it, sleeps
	waitTaken() // writer asleep on slot0
	feed(15)    // slot1 → queued behind slot0 (channel full)
	feed(30)    // slot2 → DROPPED (queue full) → episode starts, warn at 1
	if !s.queueLoss.Active() || s.Status().DroppedSlots == 0 {
		t.Fatalf("fixture: expected an active episode with a drop; active=%v dropped=%d",
			s.queueLoss.Active(), s.Status().DroppedSlots)
	}

	waitTaken() // writer took slot1 ⇒ slot0 fully processed, recovery check ran

	// slot0 persisted while slot1 was still queued: the episode must remain open.
	if !s.queueLoss.Active() {
		t.Fatal("a persist that left the queue non-empty must not end the backpressure episode")
	}

	drain(t, s)
	// The whole overload was ONE episode: exactly one warn at total 1, and recovery
	// only after the queue truly drained.
	if got := len(rhLines(&buf, `"dropped":1`)); got != 1 {
		t.Fatalf("sustained overload must be one episode (single warn at 1), got %d: %q", got, buf.String())
	}
	if got := len(rhLines(&buf, "writer queue recovered")); got != 1 {
		t.Fatalf("want exactly one recovery once drained, got %d: %q", got, buf.String())
	}
}
