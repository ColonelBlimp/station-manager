package evidence

import (
	"bytes"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// L2 acceptance criteria for the retention-measurement degraded tracker
// (internal-codebase-logging-gaps.md). The finding: evidence retention/capacity
// decisions silently swallow SQL/filesystem failures — a measurement error is
// converted to a valid-looking zero and the eventual log blames capacity. The
// confusable state this tracker breaks: "archive genuinely full" vs "archive
// could not be measured". Cadence per operator decision (2026-08-12): edge +
// 5-min write-driven heartbeat; recovery after the first complete success;
// heartbeat/recovery carry outage duration + affected measurement + dropped-slot
// counts. These assert the OBSERVABLE log records, not internal fields.

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

func rhLines(buf *bytes.Buffer, needle string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

func TestRetentionHealth_EdgeBoundedHeartbeatRecovery(t *testing.T) {
	var buf bytes.Buffer
	clk := newFakeClock()
	h := newRetentionHealth(logging.NewForWriter(&buf), "/var/lib/sm/evidence.db", 5*time.Minute, clk.now)

	// Edge: first failure announces degradation once, naming the measurement,
	// the db path and the cause.
	h.dropped()
	h.fail("freelist_count", stderrors.New("database is locked"))
	edge := rhLines(&buf, "retention measurement degraded")
	if len(edge) != 1 {
		t.Fatalf("want exactly one degraded edge line, got %d: %q", len(edge), buf.String())
	}
	if !strings.Contains(edge[0], `"operation":"freelist_count"`) ||
		!strings.Contains(edge[0], `"db_path":"/var/lib/sm/evidence.db"`) ||
		!strings.Contains(edge[0], `"error":"database is locked"`) {
		t.Errorf("edge line missing operation/db_path/error: %s", edge[0])
	}

	// Bounded: many more failures within the interval add no new line.
	for i := 0; i < 100; i++ {
		h.dropped()
		h.fail("freelist_count", stderrors.New("database is locked"))
	}
	if got := len(rhLines(&buf, "retention measurement degraded")) + len(rhLines(&buf, "still degraded")); got != 1 {
		t.Fatalf("within the interval: %d degraded lines, want 1 (bounded)", got)
	}

	// Heartbeat: after the interval elapses, one re-warn carrying outage
	// duration + dropped counts.
	clk.advance(5 * time.Minute)
	h.dropped()
	h.fail("freelist_count", stderrors.New("database is locked"))
	beats := rhLines(&buf, "still degraded")
	if len(beats) != 1 {
		t.Fatalf("want one heartbeat after 5m, got %d: %q", len(beats), buf.String())
	}
	if !strings.Contains(beats[0], `"outage_seconds":300`) {
		t.Errorf("heartbeat missing outage duration: %s", beats[0])
	}
	if !strings.Contains(beats[0], `"dropped_total":102`) {
		t.Errorf("heartbeat missing/incorrect dropped_total (want 102): %s", beats[0])
	}
	// "since last notice" restarts at the edge: 100 (loop) + 1 (after advance) = 101,
	// NOT 102 — the edge-triggering drop belongs to dropped_total, not this interval
	// (codex 468a9ad1 P2).
	if !strings.Contains(beats[0], `"dropped_since_notice":101`) {
		t.Errorf("dropped_since_notice must exclude the edge-triggering drop (want 101): %s", beats[0])
	}

	// Recovery: a complete success announces recovery once with the incident total.
	h.ok()
	rec := rhLines(&buf, "retention measurement recovered")
	if len(rec) != 1 {
		t.Fatalf("want one recovery line, got %d: %q", len(rec), buf.String())
	}
	if !strings.Contains(rec[0], `"level":"info"`) || !strings.Contains(rec[0], `"dropped_total":102`) {
		t.Errorf("recovery line wrong level or total: %s", rec[0])
	}
	if h.isDegraded() {
		t.Errorf("isDegraded() still true after recovery")
	}
}

func TestRetentionHealth_LastErrorRefreshedBetweenHeartbeats(t *testing.T) {
	var buf bytes.Buffer
	clk := newFakeClock()
	h := newRetentionHealth(logging.NewForWriter(&buf), "/db", 5*time.Minute, clk.now)

	h.fail("physical_usage", stderrors.New("first cause"))  // edge
	h.fail("physical_usage", stderrors.New("second cause")) // within interval, no emit
	clk.advance(5 * time.Minute)
	h.fail("physical_usage", stderrors.New("second cause")) // heartbeat carries the latest cause
	beats := rhLines(&buf, "still degraded")
	if len(beats) != 1 || !strings.Contains(beats[0], `"error":"second cause"`) {
		t.Fatalf("heartbeat must carry the most recent cause: %q", buf.String())
	}
}

func TestRetentionHealth_OkWhenHealthyIsNoop(t *testing.T) {
	var buf bytes.Buffer
	clk := newFakeClock()
	h := newRetentionHealth(logging.NewForWriter(&buf), "/db", 5*time.Minute, clk.now)
	h.ok()
	if buf.Len() != 0 {
		t.Errorf("ok() on a healthy tracker emitted output: %q", buf.String())
	}
}
