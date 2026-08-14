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
