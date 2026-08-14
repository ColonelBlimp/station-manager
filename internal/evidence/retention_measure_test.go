package evidence

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L2 acceptance: evidence retention/capacity decisions must not silently convert a
// measurement failure into a valid-looking zero and then blame capacity. The
// confusable state these break: "archive genuinely full" (cap) vs "archive could
// not be measured" (measurement_error). Operator decisions (2026-08-12) are
// authoritative: fail-closed, class measurement_error, status usage UNKNOWN (null),
// edge + write-driven heartbeat, tracker driven only by write attempts.

// resetMeasureHook clears the package-global injector after a test. Toggled only
// while the writer is drained (idle on its channel), so the change is ordered
// before the next slot's read via the channel send/receive.
func resetMeasureHook() { measureFailHook = nil }

// A measurement REQUIRED to authorize a slot write, when unknown, must fail the
// write closed as measurement_error (not cap, not writer_error), keep decoding,
// report usage as null, log ONE degraded transition (bounded, not per slot), and
// recover on the first healthy measurement.
func TestRetention_MeasurementFailure_FailsClosedAndRecovers(t *testing.T) {
	defer resetMeasureHook()
	var buf bytes.Buffer
	cfg := testConfig(t, true)
	s := newRunningLogged(t, cfg, &buf)

	// Baseline: a normal slot writes (decoding + evidence healthy).
	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)
	baseObs := countRows(t, openRaw(t, cfg.Path), `SELECT COUNT(*) FROM observations`)
	if baseObs == 0 {
		t.Fatal("baseline slot did not write")
	}

	// Inject a main-db stat failure: the write cannot be authorized.
	measureFailHook = func(op string) error {
		if op == "stat_db" {
			return stderrors.New("stat: input/output error")
		}
		return nil
	}
	// Several failing slots — decoding keeps handing them over; each is dropped.
	for i := 1; i <= 5; i++ {
		s.CaptureSlot(richSlot(slotAt(15 * i)))
		drain(t, s)
	}

	// ONE degraded transition, naming the granular operation — not one per slot.
	deg := rhLines(&buf, "degraded (fail-closed)")
	if len(deg) != 1 || !strings.Contains(deg[0], `"operation":"stat_db"`) {
		t.Fatalf("want exactly one degraded Warn naming stat_db, got %d: %q", len(deg), buf.String())
	}
	if beats := rhLines(&buf, "still degraded"); len(beats) != 0 {
		t.Fatalf("no heartbeat should fire within the interval, got %d", len(beats))
	}
	// Status reports usage as NULL while unmeasurable — never a false zero.
	if st := s.Status(); st.UsageBytes != nil {
		t.Errorf("status usage = %d, want null while measurement is degraded", *st.UsageBytes)
	}
	// Decoding continued: no new observations were written during the outage.
	if now := countRows(t, openRaw(t, cfg.Path), `SELECT COUNT(*) FROM observations`); now != baseObs {
		t.Errorf("observations = %d during outage, want unchanged %d (writes stop, decoding continues)", now, baseObs)
	}

	// Recovery: clear the failure; the next healthy measurement writes, records the
	// recovery, and persists the accumulated measurement_error interval.
	measureFailHook = nil
	s.CaptureSlot(richSlot(slotAt(15 * 6)))
	drain(t, s)
	if rec := rhLines(&buf, "measurement recovered"); len(rec) != 1 {
		t.Fatalf("want one recovery line after a healthy measurement, got %d: %q", len(rec), buf.String())
	}
	db := openRaw(t, cfg.Path)
	var reason string
	var slots int64
	if err := db.QueryRow(
		`SELECT reason, slots FROM loss_intervals ORDER BY start_utc DESC LIMIT 1`).Scan(&reason, &slots); err != nil {
		t.Fatalf("loss row: %v", err)
	}
	if reason != lossReasonMeasurement {
		t.Fatalf("drop reason = %q, want %q — a measurement failure must not be blamed on cap", reason, lossReasonMeasurement)
	}
	if slots != 5 {
		t.Errorf("measurement_error interval covered %d slots, want 5 (each drop counted once)", slots)
	}
}

// physicalUsage: a MISSING optional -wal/-shm is a legitimate 0 (success); the main
// db and any non-ENOENT stat failure are fail-closed, each tagged with its op.
func TestPhysicalUsage_OptionalWALSHMMissingIsSuccessOthersFailClosed(t *testing.T) {
	defer resetMeasureHook()
	path := filepath.Join(t.TempDir(), "evidence.db")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil { // 10-byte main db, no -wal/-shm
		t.Fatal(err)
	}
	s := &Service{cfg: Config{Path: path}}

	got, err := s.physicalUsage()
	if err != nil {
		t.Fatalf("missing -wal/-shm must succeed (counted as 0), got error: %v", err)
	}
	if got != 10 {
		t.Fatalf("usage = %d, want 10 (main db only; -wal/-shm absent)", got)
	}

	for _, op := range []string{"stat_db", "stat_wal", "stat_shm"} {
		measureFailHook = func(o string) error {
			if o == op {
				return stderrors.New("stat: permission denied")
			}
			return nil
		}
		if _, err := s.physicalUsage(); err == nil {
			t.Errorf("a %s failure must fail-closed (error), got nil", op)
		} else if opOf(err) != op {
			t.Errorf("op = %q, want %q", opOf(err), op)
		}
	}
}

// Every measurement/compaction probe propagates its failure tagged with a stable,
// granular operation name — the count query, PRAGMAs, metadata aggregates and the
// compaction commit are no longer silently swallowed.
func TestMeasurementProbes_PropagateGranularOps(t *testing.T) {
	defer resetMeasureHook()
	s := newRunning(t, testConfig(t, true))

	cases := []struct {
		op   string
		call func() error
	}{
		{"pragma_freelist_count", func() error { _, e := s.freelistBytes(); return e }},
		{"pragma_page_size", func() error { _, e := s.freelistBytes(); return e }},
		{"metadata_loss", func() error { _, e := s.metadataBytes(); return e }},
		{"metadata_retention", func() error { _, e := s.metadataBytes(); return e }},
		{"compaction_count", func() error { return s.maybeCompact() }},
		{"compaction_query", func() error { return s.compactOnce() }},
		{"checkpoint_purge", func() error { return s.checkpointTruncate("checkpoint_purge") }},
	}
	for _, c := range cases {
		measureFailHook = func(o string) error {
			if o == c.op {
				return stderrors.New("injected")
			}
			return nil
		}
		err := c.call()
		if err == nil {
			t.Errorf("%s: probe swallowed its failure (nil), want a tagged error", c.op)
			continue
		}
		if opOf(err) != c.op {
			t.Errorf("op = %q, want %q", opOf(err), c.op)
		}
	}
}

// A post-decision checkpoint failure, discovered AFTER a slot is already justified
// as a capacity drop, must not reclassify or double-count that slot: it is counted
// ONCE as cap, and only the tracker's health flips (operation checkpoint_drop).
func TestRetention_CheckpointFailureAfterCapDrop_SingleCount(t *testing.T) {
	defer resetMeasureHook()
	// Dial the headroom (and cap just above it) below a fresh archive's size, so the
	// tiny watermark is already crossed: the first slot is a genuine cap-drop, and
	// usage past cap-minus-reserve makes it a deferPersist drop (the checkpoint runs).
	oldHeadroom := headroomBytes
	headroomBytes = 32 * 1024
	defer func() { headroomBytes = oldHeadroom }()
	var buf bytes.Buffer
	cfg := testConfig(t, true)
	cfg.CapBytes = 40 * 1024
	s := newRunningLogged(t, cfg, &buf)

	// Fail the drop-path checkpoint (post-decision), nothing else.
	measureFailHook = func(op string) error {
		if op == "checkpoint_drop" {
			return stderrors.New("wal_checkpoint: database is locked")
		}
		return nil
	}
	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)

	// Counted exactly once.
	if st := s.Status(); st.DroppedSlots != 1 {
		t.Fatalf("dropped = %d, want 1 (a checkpoint failure must not double-count the cap drop)", st.DroppedSlots)
	}
	// Classified as cap — NOT reclassified to measurement_error by the checkpoint failure.
	s.mu.Lock()
	haveLoss := s.loss != nil
	var reason string
	if haveLoss {
		reason = s.loss.reason
	}
	s.mu.Unlock()
	if !haveLoss || reason != lossReasonCap {
		t.Fatalf("loss reason = %q (have=%v), want %q — the checkpoint failure must not reclassify the cap drop", reason, haveLoss, lossReasonCap)
	}
	// The checkpoint failure IS reported to the tracker, tagged, once.
	deg := rhLines(&buf, "degraded (fail-closed)")
	if len(deg) != 1 || !strings.Contains(deg[0], `"operation":"checkpoint_drop"`) {
		t.Fatalf("want one degraded Warn naming checkpoint_drop, got %d: %q", len(deg), buf.String())
	}
}
