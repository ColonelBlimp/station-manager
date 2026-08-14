package evidence

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// --- helpers ---------------------------------------------------------------

// syncBuf is a concurrency-safe log sink: the monitor goroutine writes to it while a
// test polls it (newRunningLogged's plain buffer is read-after-Stop only).
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func sbLines(b *syncBuf, needle string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if l != "" && strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

// setLossTunables dials the writer + loss-monitor package vars and restores them via
// t.Cleanup. Call it BEFORE the service is created: cleanups run LIFO, so the
// service's Stop (registered later) runs first and the goroutines are gone before the
// vars are restored — otherwise the restore races the still-running writer's read of
// writerDelay.
func setLossTunables(t *testing.T, q int, wd, poll, idle time.Duration) {
	t.Helper()
	oq, owd, op, oi := writerQueueSize, writerDelay, evidenceLossPollInterval, evidenceLossIdle
	writerQueueSize, writerDelay = q, wd
	evidenceLossPollInterval, evidenceLossIdle = poll, idle
	t.Cleanup(func() {
		writerQueueSize, writerDelay = oq, owd
		evidenceLossPollInterval, evidenceLossIdle = op, oi
	})
}

func newRunningSyncLogged(t *testing.T, cfg Config, sb *syncBuf) *Service {
	t.Helper()
	s := New(cfg, logging.NewForWriter(sb))
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s
}

// newQLM builds a queueLossMonitor over a manual counter + fake clock (the clock also
// drives EpisodeLoss's duration, so episode_seconds is deterministic).
func newQLM(buf *bytes.Buffer, clk *time.Time) (*queueLossMonitor, *atomic.Int64) {
	var dropped atomic.Int64
	loss := logging.NewEpisodeLoss(logging.NewForWriter(buf),
		"evidence: writer queue full; slot dropped (backpressure)",
		"evidence: writer queue recovered", lossReasonQueueFull,
		func() time.Time { return *clk })
	m := &queueLossMonitor{
		dropped:  &dropped,
		depthCap: func() (int, int) { return 40, 64 },
		loss:     loss,
		idle:     5 * time.Second,
	}
	return m, &dropped
}

// --- durable classification (deterministic, independent of the monitor) ----

// L3: a slot dropped because the WRITER QUEUE is full is backpressure, not a write
// failure. The durable loss record must read evidence_queue_full (never writer_error,
// since no write was attempted) — the confusable state being a backed-up writer vs a
// failing one. This is decided inline under s.mu and does not depend on the monitor.
func TestCaptureSlot_QueueFull_RecordedAsBackpressure(t *testing.T) {
	setLossTunables(t, 1, 50*time.Millisecond, evidenceLossPollInterval, evidenceLossIdle)

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	for i := 0; i < 12; i++ {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeNoDecode})
	}
	drain(t, s)
	if s.Status().DroppedSlots == 0 {
		t.Fatal("fixture: no drops occurred")
	}

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
		t.Fatalf("queue-full drop must be recorded as %q; got %v", lossReasonQueueFull, reasons)
	}
	if reasons[lossReasonWriter] {
		t.Errorf("no write was attempted — a queue-full drop must not be recorded as %q", lossReasonWriter)
	}
}

// --- monitor step logic (deterministic, fake clock) ------------------------

// The monitor reports newly-observed drops as bounded warns (reason + queue
// depth/capacity) and declares recovery only after the idle window with no new drops —
// the confusable state being a brief overload reported as permanently active.
// Operator decision (2026-08-14): idle = 5 s, matching audio.
func TestQueueLossMonitor_WarnsWithReasonAndRecoversAfterIdle(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	m, dropped := newQLM(&buf, &clk)

	m.step(clk) // no drops → silent
	if m.loss.Active() {
		t.Fatal("no episode before a drop")
	}

	dropped.Store(1)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // first drop → warn at 1

	dropped.Store(4)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // total 4 (below next threshold) → no new warn; idle clock resets

	clk = clk.Add(4 * time.Second)
	m.step(clk) // 4 s quiet → not yet recovered
	if !m.loss.Active() {
		t.Fatal("recovery declared before the idle window elapsed")
	}

	clk = clk.Add(1 * time.Second)
	m.step(clk) // 5 s quiet → recovery

	warns := rhLines(&buf, "writer queue full")
	if len(warns) != 1 || !strings.Contains(warns[0], `"reason":"evidence_queue_full"`) ||
		!strings.Contains(warns[0], `"dropped":1`) ||
		!strings.Contains(warns[0], `"queue_depth":40`) || !strings.Contains(warns[0], `"queue_capacity":64`) {
		t.Fatalf("want one warn carrying reason + depth/capacity, got: %q", buf.String())
	}
	rec := rhLines(&buf, "writer queue recovered")
	if len(rec) != 1 || !strings.Contains(rec[0], `"total_dropped":4`) || !strings.Contains(rec[0], `"episode_seconds":5`) {
		t.Fatalf("want one recovery summary (total 4, ~5 s), got: %q", buf.String())
	}
}

// The idle window is measured from the LAST drop, not the first: a fresh drop resets
// it, so a run of sporadic drops is not declared recovered mid-overload.
func TestQueueLossMonitor_NewDropResetsIdleWindow(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	m, dropped := newQLM(&buf, &clk)

	dropped.Store(1)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // episode starts

	clk = clk.Add(4 * time.Second)
	m.step(clk) // 4 s quiet

	dropped.Store(2)
	clk = clk.Add(100 * time.Millisecond)
	m.step(clk) // new drop → idle resets

	clk = clk.Add(4 * time.Second) // 4 s since the SECOND drop (>8 s since the first)
	m.step(clk)
	if !m.loss.Active() {
		t.Fatal("idle window must reset on a new drop, not run from the first")
	}

	clk = clk.Add(1 * time.Second) // 5 s since the second drop
	m.step(clk)
	if m.loss.Active() {
		t.Fatal("recovery must be declared 5 s after the last drop")
	}
}

// The monitor baselines at the count as of its Start, so a counter already non-zero
// (a fresh Service starts at 0, but the baseline discipline is explicit) does not
// replay pre-baseline drops as a phantom episode.
func TestQueueLossMonitor_BaselineExcludesPreExisting(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	m, dropped := newQLM(&buf, &clk)

	dropped.Store(100)
	m.lastSeen = 100

	m.step(clk)
	if m.loss.Active() || buf.Len() != 0 {
		t.Fatalf("pre-baseline drops must not open an episode: active=%v log=%q", m.loss.Active(), buf.String())
	}

	dropped.Store(101)
	m.step(clk)
	if !m.loss.Active() {
		t.Fatal("a drop past the baseline must open an episode")
	}
}

// --- wiring (the monitor is actually started and drives both logs) ---------

// The monitor (a ticker) — not the producer or the write path — owns the L3
// backpressure warn and recovery. Proves it is wired to the running service: a burst
// of queue-full drops produces a warn (reason + queue capacity), and after the quiet
// window with no new drops a recovery summary is logged WHILE the service still runs
// (so recovery is the monitor's, not Stop's flush).
func TestQueueLoss_MonitorWarnsThenRecoversOnQuietWindow(t *testing.T) {
	setLossTunables(t, 1, 50*time.Millisecond, 5*time.Millisecond, 60*time.Millisecond)

	sb := &syncBuf{}
	cfg := testConfig(t, true)
	s := newRunningSyncLogged(t, cfg, sb)

	for i := 0; i < 12; i++ {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeNoDecode})
	}

	waitFor(t, "queue-full warn", func() bool { return len(sbLines(sb, "writer queue full")) > 0 })
	warns := sbLines(sb, "writer queue full")
	if !strings.Contains(warns[0], `"reason":"evidence_queue_full"`) || !strings.Contains(warns[0], `"queue_capacity":1`) {
		t.Fatalf("warn must carry reason + queue capacity: %q", warns[0])
	}

	// No new drops → recovery on the quiet window, while the service is STILL running.
	waitFor(t, "quiet-window recovery", func() bool { return len(sbLines(sb, "writer queue recovered")) > 0 })
	rec := sbLines(sb, "writer queue recovered")
	if !strings.Contains(rec[0], `"total_dropped"`) || !strings.Contains(rec[0], `"episode_seconds"`) {
		t.Fatalf("recovery must carry total + duration: %q", rec[0])
	}
}

// Drops in the window between the monitor's last poll and Stop must not vanish from
// the record: a short overload immediately before shutdown must still produce the
// first-loss warn AND a recovery summary. Poll interval is set longer than the test,
// so the monitor NEVER polls before Stop — the only path that can log the drops is the
// quit-time flush.
func TestQueueLoss_FlushesDropsOnStop(t *testing.T) {
	setLossTunables(t, 1, 50*time.Millisecond, time.Hour, time.Hour)

	sb := &syncBuf{}
	cfg := testConfig(t, true)
	s := newRunningSyncLogged(t, cfg, sb)

	for i := 0; i < 12; i++ {
		s.CaptureSlot(SlotCapture{SlotStart: slotAt(15 * i), Outcome: OutcomeNoDecode})
	}
	if s.queueDropped.Load() == 0 {
		t.Fatal("fixture: no drops occurred before shutdown")
	}

	s.Stop() // joins the monitor; the quit flush must sample + record the drops

	if len(sbLines(sb, "writer queue full")) == 0 {
		t.Fatalf("drops before shutdown must be flushed as a first-loss warn: %q", sb.String())
	}
	rec := sbLines(sb, "writer queue recovered")
	if len(rec) == 0 || !strings.Contains(rec[0], `"total_dropped"`) {
		t.Fatalf("shutdown must emit a recovery summary carrying the total: %q", sb.String())
	}
}
