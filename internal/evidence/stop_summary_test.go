package evidence

// H2 (docs/reviews/internal-codebase-logging-gaps.md) — evidence shutdown was silent: Stop
// drained the writer + sync loops, closed the archive, and left no completion record, so a
// clean drain was indistinguishable from one that dropped slots, and the archive close error
// had no summary context. Operator rulings 2026-08-16:
//   1. Info for a clean stop; Error when the archive close fails. dropped_total is informational.
//   2. Report sync_state only — no shutdown-time quarantine query/count.
//   3. Fold the close-error line into ONE completion record; close_error present only on failure.
//   Fields: dropped_total, pending, sync_state, run_duration_seconds, optional close_error.
//   Emitted after worker/sync drainage AND the archive close attempt.

import (
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const stopMsg = "evidence: stopped"

func stopRecord(t *testing.T, out string) map[string]any {
	t.Helper()
	var found map[string]any
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["message"] == stopMsg {
			found = rec
			n++
		}
	}
	if n != 1 {
		t.Fatalf("evidence-stopped records = %d, want exactly 1\n%s", n, out)
	}
	return found
}

// Unit level: the completion record's fields + level logic, with closeErr controlled directly
// (a real *sql.DB.Close() failure is not deterministically triggerable).
func TestLogStopSummary_FieldsAndLevel(t *testing.T) {
	t.Run("clean stop is Info with no close_error", func(t *testing.T) {
		buf := &syncBuf{}
		s := &Service{cfg: Config{Path: "/var/lib/sm/evidence.db"}, log: logging.NewForWriter(buf)}

		s.logStopSummary(7, 2, syncStateBackoff, 90*time.Second, nil)

		rec := stopRecord(t, buf.String())
		if rec["level"] != "info" {
			t.Errorf("level = %v, want info", rec["level"])
		}
		if d, _ := rec["dropped_total"].(float64); int64(d) != 7 {
			t.Errorf("dropped_total = %v, want 7", rec["dropped_total"])
		}
		if p, _ := rec["pending"].(float64); int64(p) != 2 {
			t.Errorf("pending = %v, want 2", rec["pending"])
		}
		if rec["sync_state"] != "backoff" {
			t.Errorf("sync_state = %v, want backoff", rec["sync_state"])
		}
		if rd, _ := rec["run_duration_seconds"].(float64); int64(rd) != 90 {
			t.Errorf("run_duration_seconds = %v, want 90", rec["run_duration_seconds"])
		}
		if _, ok := rec["close_error"]; ok {
			t.Errorf("clean stop must not carry close_error: %v", rec["close_error"])
		}
	})

	t.Run("archive close failure is Error with close_error", func(t *testing.T) {
		buf := &syncBuf{}
		s := &Service{cfg: Config{Path: "/x"}, log: logging.NewForWriter(buf)}

		s.logStopSummary(0, 0, syncStateIdle, time.Second, stderrors.New("disk gone"))

		rec := stopRecord(t, buf.String())
		if rec["level"] != "error" {
			t.Errorf("level = %v, want error on a close failure", rec["level"])
		}
		if rec["close_error"] != "disk gone" {
			t.Errorf("close_error = %v, want %q", rec["close_error"], "disk gone")
		}
	})
}

// Integration: a real Start → Stop emits exactly one completion record, at Info for a clean
// drain, carrying the drained state + the run duration. Proves Stop is wired to the summary.
func TestStop_EmitsCompletionSummary(t *testing.T) {
	sb := &syncBuf{}
	cfg := testConfig(t, true)
	s := New(cfg, logging.NewForWriter(sb))
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Backdate the start so run_duration_seconds has a known floor.
	s.mu.Lock()
	s.startedAt = time.Now().Add(-3 * time.Second)
	s.mu.Unlock()

	s.Stop()

	rec := stopRecord(t, sb.String())
	if rec["level"] != "info" {
		t.Errorf("clean stop level = %v, want info", rec["level"])
	}
	if d, _ := rec["dropped_total"].(float64); int64(d) != 0 {
		t.Errorf("dropped_total = %v, want 0 for a clean run", rec["dropped_total"])
	}
	if _, ok := rec["sync_state"]; !ok {
		t.Errorf("summary carries no sync_state: %v", rec)
	}
	if rd, _ := rec["run_duration_seconds"].(float64); rd < 3 {
		t.Errorf("run_duration_seconds = %v, want >= 3 (backdated start)", rec["run_duration_seconds"])
	}
	if _, ok := rec["close_error"]; ok {
		t.Errorf("a clean archive close must not carry close_error: %v", rec["close_error"])
	}
}
