package logging

import (
	"bytes"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// L1 acceptance criteria (internal-codebase-logging-gaps.md). The finding: the
// logging system cannot report failure of its OWN output. io.MultiWriter stops at
// the first failing writer, the file target is probed only at Init, and a runtime
// disk-full / rotation / permission failure surfaces as nothing more than
// zerolog's generic per-write stderr line — so "logging healthy" is
// indistinguishable from "structured records are being lost after startup".
//
// Each criterion names the two nearest confusable states and the field that tells
// them apart, and is asserted on OBSERVABLE output (the delivered bytes, the
// non-recursive fallback line), never on internal wiring.
//
//   AC1 Isolation — a durable-writer failure must not stop delivery to another
//        target. Confusable: "record reached the console" vs "record dropped at
//        the file failure" (today's short-circuit).
//   AC2 Degradation announced ONCE, with cause — confusable: "logging healthy"
//        (no line) vs "durable writer failing" (one line, carrying the error);
//        and vs zerolog's generic per-write spam.
//   AC3 Recovery announced — confusable: "still degraded" (no line) vs
//        "recovered" (a recovered line), so "is the log still broken?" is
//        answerable from journald alone.
//   AC4 Bounded volume — confusable: a 1000-record outage vs 1000 fallback lines.
//   Heartbeat — while still degraded, re-warn at most once per interval.
//   AC6 Accounting — degraded()/failures()/lastError() reflect the true state.

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// errWriter always fails — a durable sink that has gone bad (disk full, etc.).
type errWriter struct{ err error }

func (e errWriter) Write(p []byte) (int, error) { return 0, e.err }

// toggleWriter fails while `fail` is set, else buffers — used to drive recovery.
type toggleWriter struct {
	mu   sync.Mutex
	fail bool
	buf  bytes.Buffer
}

func (w *toggleWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, stderrors.New("disk full")
	}
	return w.buf.Write(p)
}
func (w *toggleWriter) setFail(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail = v
}

// fallbackLines returns the non-empty JSON fallback lines emitted so far.
func fallbackLines(buf *bytes.Buffer) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(l, `"event":`) {
			out = append(out, l)
		}
	}
	return out
}

func TestHealthWriter_AC1_DeliversToConsoleEvenWhenFileFails(t *testing.T) {
	var console, fb bytes.Buffer
	clk := newFakeClock()
	// durable file target first (errors), best-effort console second.
	hw := newHealthWriter([]healthTarget{
		{name: "file", w: errWriter{err: stderrors.New("no space left on device")}, durable: true},
		{name: "console", w: &console, durable: false},
	}, &fb, defaultDegradedHeartbeat, clk.now)

	n, err := hw.Write([]byte("hello record\n"))
	if err != nil {
		t.Fatalf("Write returned error to zerolog (would trigger recursive spam): %v", err)
	}
	if n != len("hello record\n") {
		t.Fatalf("Write n = %d, want full length", n)
	}
	if got := console.String(); got != "hello record\n" {
		t.Errorf("console did not receive the record despite the file failing: %q — records are lost at the first failing writer (AC1)", got)
	}
}

func TestHealthWriter_AC2_AnnouncesDegradationOnceWithCause(t *testing.T) {
	var fb bytes.Buffer
	clk := newFakeClock()
	hw := newHealthWriter([]healthTarget{
		{name: "file", w: errWriter{err: stderrors.New("no space left on device")}, durable: true},
	}, &fb, defaultDegradedHeartbeat, clk.now)

	// A healthy (non-durable-failing) writer would emit nothing; here the very
	// first durable-failing write is the healthy->failing edge → exactly one line.
	_, _ = hw.Write([]byte("r1\n"))
	lines := fallbackLines(&fb)
	if len(lines) != 1 {
		t.Fatalf("want exactly one degraded line, got %d: %q", len(lines), fb.String())
	}
	if !strings.Contains(lines[0], `"event":"degraded"`) {
		t.Errorf("first line is not a degraded event: %s", lines[0])
	}
	if !strings.Contains(lines[0], "no space left on device") {
		t.Errorf("degraded line must carry the cause: %s", lines[0])
	}
}

func TestHealthWriter_AC3_AnnouncesRecovery(t *testing.T) {
	var fb bytes.Buffer
	clk := newFakeClock()
	tw := &toggleWriter{fail: true}
	hw := newHealthWriter([]healthTarget{{name: "file", w: tw, durable: true}}, &fb, defaultDegradedHeartbeat, clk.now)

	_, _ = hw.Write([]byte("r1\n")) // fails → degraded
	tw.setFail(false)
	_, _ = hw.Write([]byte("r2\n")) // succeeds → recovered

	lines := fallbackLines(&fb)
	if len(lines) != 2 {
		t.Fatalf("want degraded + recovered, got %d lines: %q", len(lines), fb.String())
	}
	if !strings.Contains(lines[1], `"event":"recovered"`) {
		t.Errorf("second line is not a recovery event: %s", lines[1])
	}
	if hw.degraded() {
		t.Errorf("degraded() still true after a successful durable write")
	}
}

func TestHealthWriter_AC4_BoundedUnderSustainedFailure(t *testing.T) {
	var fb bytes.Buffer
	clk := newFakeClock() // clock does NOT advance → no heartbeat
	hw := newHealthWriter([]healthTarget{
		{name: "file", w: errWriter{err: stderrors.New("disk full")}, durable: true},
	}, &fb, defaultDegradedHeartbeat, clk.now)

	for i := 0; i < 1000; i++ {
		_, _ = hw.Write([]byte("spam\n"))
	}
	if lines := fallbackLines(&fb); len(lines) != 1 {
		t.Fatalf("1000 failing writes produced %d fallback lines, want exactly 1 (bounded) — AC4", len(lines))
	}
	if hw.failures() != 1000 {
		t.Errorf("failures() = %d, want 1000 (every failure counted even though only one line emitted)", hw.failures())
	}
}

func TestHealthWriter_Heartbeat_ReWarnsPerInterval(t *testing.T) {
	var fb bytes.Buffer
	clk := newFakeClock()
	hw := newHealthWriter([]healthTarget{
		{name: "file", w: errWriter{err: stderrors.New("disk full")}, durable: true},
	}, &fb, 5*time.Minute, clk.now)

	_, _ = hw.Write([]byte("r1\n")) // degraded edge (line 1)
	for i := 0; i < 50; i++ {       // still within the interval → no new line
		_, _ = hw.Write([]byte("x\n"))
	}
	if lines := fallbackLines(&fb); len(lines) != 1 {
		t.Fatalf("before the interval elapsed: %d lines, want 1", len(lines))
	}

	clk.advance(5 * time.Minute)
	_, _ = hw.Write([]byte("r2\n")) // interval elapsed → heartbeat (line 2)
	lines := fallbackLines(&fb)
	if len(lines) != 2 {
		t.Fatalf("after 5m elapsed: %d lines, want 2 (one heartbeat)", len(lines))
	}
	if !strings.Contains(lines[1], `"event":"still_degraded"`) {
		t.Errorf("heartbeat line is not a still_degraded event: %s", lines[1])
	}
}

func TestHealthWriter_AC6_Accounting(t *testing.T) {
	var fb bytes.Buffer
	clk := newFakeClock()
	tw := &toggleWriter{fail: false}
	hw := newHealthWriter([]healthTarget{{name: "file", w: tw, durable: true}}, &fb, defaultDegradedHeartbeat, clk.now)

	if hw.degraded() || hw.failures() != 0 || hw.lastError() != "" {
		t.Fatalf("fresh writer must be healthy: degraded=%v failures=%d last=%q", hw.degraded(), hw.failures(), hw.lastError())
	}
	tw.setFail(true)
	_, _ = hw.Write([]byte("r1\n"))
	if !hw.degraded() {
		t.Errorf("degraded() = false after a durable write failed")
	}
	if hw.lastError() != "disk full" {
		t.Errorf("lastError() = %q, want the cause", hw.lastError())
	}
}
