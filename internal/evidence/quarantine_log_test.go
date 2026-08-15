package evidence

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
)

// L8 delta on C1b — the quarantine Warn must break the total down BY EVIDENCE KIND,
// so an operator sees which kinds SM Cloud refused, not just how many rows. The
// bounded representative reason stays (per operator 2026-08-15: grouping by truncated
// raw reason would bound length, not cardinality, and imply a taxonomy that does not
// exist).
func TestQuarantine_LogsCountsByKind(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	// Reject EVERY row so the batch quarantines whatever kinds a rich slot produces,
	// and by_kind must decompose the total across them.
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: "digest_conflict"}
	}

	cfg := syncedConfig(t, smc.ts.URL)
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)
	s.CaptureSlot(richSlot(slotAt(0)))
	drain(t, s)

	waitFor(t, "quarantine committed", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE quarantine_reason IS NOT NULL`) >= 1
	})
	s.Stop()

	var line string
	for _, l := range defaultVisibleLines(&buf) {
		if strings.Contains(l, "quarantined") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no quarantine Warn; log:\n%s", buf.String())
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("quarantine line is not JSON: %v; %s", err, line)
	}
	byKind, ok := rec["by_kind"].(map[string]any)
	if !ok || len(byKind) == 0 {
		t.Fatalf("quarantine line has no non-empty by_kind breakdown: %s", line)
	}

	// Ground truth: the per-table quarantine counts in the DB. Comparing the logged
	// breakdown against this catches a total mislabelled under one kind — which
	// "sums-to-total + observation-present" alone would NOT (this batch spans >1 kind).
	db := openRaw(t, cfg.Path)
	expect := map[string]int{}
	for _, tbl := range []struct{ kind, table string }{
		{evidencewire.KindObservation, "observations"},
		{evidencewire.KindCoverage, "coverage"},
		{evidencewire.KindLossInterval, "loss_intervals"},
		{evidencewire.KindProfile, "profiles"},
		{evidencewire.KindRetention, "retention_records"},
	} {
		if n := countRows(t, db, `SELECT COUNT(*) FROM `+tbl.table+` WHERE quarantine_reason IS NOT NULL`); n > 0 {
			expect[tbl.kind] = n
		}
	}
	if len(expect) < 2 {
		t.Fatalf("fixture spans only %d quarantined kind(s); the test needs >1 to prove per-kind separation: %v", len(expect), expect)
	}
	if len(byKind) != len(expect) {
		t.Errorf("by_kind has %d kinds, want %d (DB): logged=%v db=%v", len(byKind), len(expect), byKind, expect)
	}
	for k, want := range expect {
		got, _ := byKind[k].(float64)
		if int(got) != want {
			t.Errorf("by_kind[%q] = %v, want %d (DB ground truth)", k, got, want)
		}
	}
}

// C1b — when SM Cloud PERMANENTLY rejects (quarantines) evidence rows, the SM operator
// must have a durable LOCAL trace. applyOutcomes wrote the per-row quarantine_reason
// column but logged nothing, so a refusal was invisible outside a DB query (the cloud
// server's own log is on the other side of the link). One bounded per-batch Warn now
// carries the count + a representative row.
func TestQuarantine_IsLoggedForTheOperator(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		if rec.Kind == evidencewire.KindObservation {
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: "digest_conflict"}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}

	cfg := syncedConfig(t, smc.ts.URL)
	var buf bytes.Buffer
	s := newRunningLogged(t, cfg, &buf)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)

	// Wait for the reject to commit locally before reading the (Stop-guarded) buffer.
	waitFor(t, "quarantine committed", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE quarantine_reason IS NOT NULL`) >= 1
	})
	s.Stop()

	var line string
	for _, l := range defaultVisibleLines(&buf) {
		if strings.Contains(l, "quarantined") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no quarantine Warn — a permanent reject must leave a durable local trace (C1b)\n%s", buf.String())
	}
	if !strings.Contains(line, `"level":"warn"`) {
		t.Errorf("quarantine line is not a Warn: %s", line)
	}
	if !strings.Contains(line, "digest_conflict") {
		t.Errorf("quarantine line must carry the reason (sample_reason): %s", line)
	}
}
