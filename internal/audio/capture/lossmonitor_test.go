package capture

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func lmLines(buf *bytes.Buffer, needle string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l != "" && strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

// A shared fake clock drives BOTH the EpisodeLoss duration and the monitor's idle
// check, so episode_seconds and the recovery moment are deterministic.
func newMonitor(buf *bytes.Buffer, clk *time.Time) (*lossMonitor, *atomic.Int64) {
	var dropped atomic.Int64
	loss := logging.NewEpisodeLoss(logging.NewForWriter(buf),
		"audio: capture buffer full; chunk dropped (consumer too slow)",
		"audio: capture buffer recovered", "audio_queue_full",
		func() time.Time { return *clk })
	m := &lossMonitor{
		dropped:  &dropped,
		depthCap: func() (int, int) { return 60, 64 },
		loss:     loss,
		idle:     5 * time.Second,
	}
	return m, &dropped
}

// L3 audio: a chunk dropped in the real-time callback (atomic increment only) must
// surface as a bounded warn (episode total + channel depth/capacity, reason
// audio_queue_full), and recovery must be declared only after the idle window with
// no new drops — the confusable state being a brief overload reported as permanently
// active. Operator decision (2026-08-14): idle = 5 s.
func TestLossMonitor_WarnsOnDropAndRecoversAfterIdle(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	m, dropped := newMonitor(&buf, &clk)

	m.step(clk) // no drops yet → silent
	if m.loss.Active() {
		t.Fatal("no episode should start before a drop")
	}

	dropped.Store(1)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // first drop → warn at 1, episode starts

	dropped.Store(4)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // total 4 (below the next threshold) → no new warn, idle clock resets

	clk = clk.Add(4 * time.Second)
	m.step(clk) // only 4 s quiet → must NOT declare recovery yet
	if !m.loss.Active() {
		t.Fatal("recovery declared before the idle window elapsed")
	}

	clk = clk.Add(1 * time.Second)
	m.step(clk) // 5 s quiet → recovery

	warns := lmLines(&buf, "capture buffer full")
	if len(warns) != 1 || !strings.Contains(warns[0], `"reason":"audio_queue_full"`) ||
		!strings.Contains(warns[0], `"dropped":1`) ||
		!strings.Contains(warns[0], `"queue_depth":60`) || !strings.Contains(warns[0], `"queue_capacity":64`) {
		t.Fatalf("want one warn carrying reason + depth/capacity, got: %q", buf.String())
	}
	rec := lmLines(&buf, "capture buffer recovered")
	if len(rec) != 1 || !strings.Contains(rec[0], `"total_dropped":4`) || !strings.Contains(rec[0], `"episode_seconds":5`) {
		t.Fatalf("want one recovery summary (total 4, ~5 s), got: %q", buf.String())
	}
}

// The idle window is measured from the LAST drop, not the first: a fresh drop must
// reset it, so a run of sporadic drops is not declared recovered mid-overload.
func TestLossMonitor_NewDropResetsIdleWindow(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	m, dropped := newMonitor(&buf, &clk)

	dropped.Store(1)
	clk = clk.Add(500 * time.Millisecond)
	m.step(clk) // episode starts

	clk = clk.Add(4 * time.Second)
	m.step(clk) // 4 s quiet → not yet recovered

	dropped.Store(2)
	clk = clk.Add(100 * time.Millisecond)
	m.step(clk) // new drop → idle window resets

	clk = clk.Add(4 * time.Second) // 4 s since the SECOND drop (but >8 s since the first)
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
