package evidence

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
)

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
