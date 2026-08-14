package logging

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func elLines(buf *bytes.Buffer, needle string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l != "" && strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

func TestEpisodeLoss_WarnsAtPowersOfTenAndRecovers(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	el := NewEpisodeLoss(NewForWriter(&buf), "queue full", "queue recovered",
		"evidence_queue_full", func() time.Time { return clk })

	for i := 0; i < 100; i++ {
		el.Add(1, 3, 4) // 100 single drops, depth 3 / capacity 4
	}
	warns := elLines(&buf, "queue full")
	if len(warns) != 3 {
		t.Fatalf("want 3 warns (episode totals 1, 10, 100), got %d: %q", len(warns), buf.String())
	}
	for i, wantTotal := range []string{`"dropped":1`, `"dropped":10`, `"dropped":100`} {
		if !strings.Contains(warns[i], wantTotal) {
			t.Errorf("warn %d missing %s: %s", i, wantTotal, warns[i])
		}
	}
	if !strings.Contains(warns[0], `"reason":"evidence_queue_full"`) ||
		!strings.Contains(warns[0], `"queue_depth":3`) || !strings.Contains(warns[0], `"queue_capacity":4`) {
		t.Errorf("warn must carry reason + queue depth/capacity: %s", warns[0])
	}

	clk = clk.Add(7 * time.Second)
	el.Recover()
	rec := elLines(&buf, "queue recovered")
	if len(rec) != 1 {
		t.Fatalf("want one recovery summary, got %d", len(rec))
	}
	if !strings.Contains(rec[0], `"total_dropped":100`) || !strings.Contains(rec[0], `"episode_seconds":7`) {
		t.Errorf("recovery must carry total lost + episode duration: %s", rec[0])
	}

	// Schedule resets: a fresh drop after recovery warns at total 1 again.
	el.Add(1, 1, 4)
	if got := len(elLines(&buf, "queue full")); got != 4 {
		t.Fatalf("after recovery a new episode must warn at 1 again; total warns = %d", got)
	}
}

func TestEpisodeLoss_BatchedAddCrossingThresholdsLogsOnce(t *testing.T) {
	var buf bytes.Buffer
	el := NewEpisodeLoss(NewForWriter(&buf), "full", "recovered", "audio_queue_full", nil)
	el.Add(1, 0, 8)   // warn at 1
	el.Add(500, 2, 8) // total 501 — crosses 10 and 100 in one call → a single warn at 501
	warns := elLines(&buf, `"reason":"audio_queue_full"`)
	if len(warns) != 2 {
		t.Fatalf("want 2 warns (1, then 501), got %d: %q", len(warns), buf.String())
	}
	if !strings.Contains(warns[1], `"dropped":501`) {
		t.Errorf("a batched crossing should report the current total 501: %s", warns[1])
	}
}

func TestEpisodeLoss_RecoverInactiveIsNoop(t *testing.T) {
	var buf bytes.Buffer
	el := NewEpisodeLoss(NewForWriter(&buf), "full", "recovered", "audio_queue_full", nil)
	el.Recover()
	if buf.Len() != 0 {
		t.Errorf("Recover with no active episode emitted output: %q", buf.String())
	}
	if el.Active() {
		t.Errorf("no episode should be active")
	}
}
