package serial

// L13 — oversized serial frames must not disappear silently.
// (docs/reviews/internal-codebase-logging-gaps.md; operator rulings 2026-08-16.)
//
// The reader drops any frame exceeding maxLineSize (4096) and resumes at the next delimiter.
// It must now count each dropped frame EXACTLY ONCE — regardless of whether the delimiter
// lands mid-buffer (spanning reads) or in the same chunk — and notify an injected callback
// with only (threshold_bytes, dropped_total). No raw frame bytes cross the callback (the
// signature carries none). The callback is invoked at a bounded cadence (first drop
// immediately, then at most once per oversizeWarnInterval); the running total resets per
// reader session. Exactly 4096 bytes is VALID (drop is strictly > maxLineSize).

import (
	"context"
	"sync"
	"testing"
	"time"
)

// oversizeRec captures callback invocations race-safely (the reader goroutine calls it).
type oversizeRec struct {
	mu    sync.Mutex
	calls [][2]int // {threshold, total}
}

func (r *oversizeRec) cb(threshold, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, [2]int{threshold, total})
}

func (r *oversizeRec) snapshot() [][2]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][2]int, len(r.calls))
	copy(out, r.calls)
	return out
}

// feedN sends `total` 'A' bytes with no delimiter, in <=defaultBufSize chunks (the mockPort
// truncates a Read to the reader's buffer, so chunks must not exceed it).
func feedN(readCh chan<- []byte, total int) {
	for total > 0 {
		n := defaultBufSize
		if total < n {
			n = total
		}
		chunk := make([]byte, n)
		for i := range chunk {
			chunk[i] = 'A'
		}
		readCh <- chunk
		total -= n
	}
}

// The rate-limit decision, in isolation with a fake clock: the first drop fires immediately,
// further drops within the interval are suppressed (but counted), and the first drop past the
// interval fires again carrying the ACCUMULATED total.
func TestOversizeThrottle_FirstThenBoundedCadence(t *testing.T) {
	clk := time.Unix(1_000_000, 0)
	th := &oversizeThrottle{now: func() time.Time { return clk }, interval: 60 * time.Second}

	step := func(wantTotal int, wantFire bool) {
		t.Helper()
		total, fire := th.record()
		if total != wantTotal || fire != wantFire {
			t.Fatalf("record() = (total %d, fire %v), want (%d, %v)", total, fire, wantTotal, wantFire)
		}
	}
	step(1, true)  // first drop → fire
	step(2, false) // within interval → suppressed, counted
	step(3, false)
	clk = clk.Add(60 * time.Second)
	step(4, true)  // interval elapsed → fire with the accumulated total
	step(5, false) // new interval → suppressed again
}

// C1 + count-once: a single oversized frame spanning many reads is dropped, notified ONCE
// (not per chunk), and framing resumes so the next good line is delivered.
func TestReaderLoop_OversizeSpanningFrame_NotifiedOnce(t *testing.T) {
	rec := &oversizeRec{}
	o := newOverflowPort()
	cfg := Config{PortName: "mock", BaudRate: 9600, LineDelimiter: ';', OnOversizeFrame: rec.cb}
	c := newPort(o, cfg)

	feedOverflow(o.readCh) // > maxLineSize, no delimiter (spans many reads)
	o.readCh <- []byte(";OK;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if resp, err := c.ReadResponseBytes(ctx); err != nil || string(resp) != "OK" {
		t.Fatalf("framing did not resume after the drop: got %q err %v", resp, err)
	}
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("oversize callbacks = %d, want exactly 1 (count once per frame, not per chunk): %v", len(calls), calls)
	}
	if calls[0] != [2]int{maxLineSize, 1} {
		t.Errorf("callback = %v, want {threshold %d, total 1}", calls[0], maxLineSize)
	}
}

// Ruling 5: an oversized frame whose delimiter arrives IN THE SAME chunk (lineBuf never
// exceeded the cap on its own) must ALSO be dropped + counted — never emitted. lineBuf reaches
// exactly 4096 (valid so far), then "A;" makes the frame 4097 > cap when the delimiter is found.
func TestReaderLoop_OversizeWithInChunkDelimiter_AlsoDropped(t *testing.T) {
	rec := &oversizeRec{}
	o := newOverflowPort()
	cfg := Config{PortName: "mock", BaudRate: 9600, LineDelimiter: ';', OnOversizeFrame: rec.cb}
	c := newPort(o, cfg)

	feedN(o.readCh, maxLineSize) // 4096 bytes, no delimiter — lineBuf == cap, not yet dropped
	o.readCh <- []byte("A;")     // frame = 4097 with the delimiter here → drop, do NOT emit
	o.readCh <- []byte("OK;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if resp, err := c.ReadResponseBytes(ctx); err != nil || string(resp) != "OK" {
		t.Fatalf("the 4097-byte frame must be dropped, not emitted: got %q err %v", resp, err)
	}
	calls := rec.snapshot()
	if len(calls) != 1 || calls[0] != [2]int{maxLineSize, 1} {
		t.Fatalf("callbacks = %v, want exactly one {%d, 1} — the in-chunk-delimiter oversized frame counts", calls, maxLineSize)
	}
}

// Ruling 5 boundary: exactly 4096 bytes is a VALID frame — emitted, not dropped.
func TestReaderLoop_Exactly4096IsValid(t *testing.T) {
	rec := &oversizeRec{}
	o := newOverflowPort()
	cfg := Config{PortName: "mock", BaudRate: 9600, LineDelimiter: ';', OnOversizeFrame: rec.cb}
	c := newPort(o, cfg)

	feedN(o.readCh, maxLineSize) // 4096 bytes
	o.readCh <- []byte(";")      // delimiter → frame is exactly 4096 → emit

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(resp) != maxLineSize {
		t.Fatalf("frame len = %d, want %d (exactly the cap is valid)", len(resp), maxLineSize)
	}
	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("a valid 4096-byte frame must not notify a drop: %v", calls)
	}
}

// C2 at the reader: many drops inside one interval collapse to a SINGLE callback (the total
// keeps climbing internally, but the throttle test owns the accumulation-fires-later half).
func TestReaderLoop_MultipleDropsWithinInterval_NotifiedOnce(t *testing.T) {
	rec := &oversizeRec{}
	o := newOverflowPort()
	cfg := Config{PortName: "mock", BaudRate: 9600, LineDelimiter: ';', OnOversizeFrame: rec.cb}
	c := newPort(o, cfg)

	for i := 0; i < 3; i++ { // three separate oversized frames, all within the 60s interval
		feedOverflow(o.readCh)
		o.readCh <- []byte(";")
	}
	o.readCh <- []byte("OK;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if resp, err := c.ReadResponseBytes(ctx); err != nil || string(resp) != "OK" {
		t.Fatalf("framing did not resume: got %q err %v", resp, err)
	}
	if calls := rec.snapshot(); len(calls) != 1 {
		t.Fatalf("oversize callbacks = %d, want 1 (rate-limited within the interval): %v", len(calls), calls)
	}
}
