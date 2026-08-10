package evidence

/*
   Retention-slice acceptance criteria RT1–RT10 (spot-network §4.1 + the
   2026-08-10 retention-slice rulings, recorded as the dated §4.1
   amendment; SMC halves E11–E13 in internal/cloud/store/evidence_test.go).
   Each criterion names its nearest confusable state. Written RED before
   the implementation (ATDD).

   RT1  Capture outlives the cap when acked history AND metadata capacity
        exist: at the watermark with cloud-present rows and retention-
        metadata reserve available, capture CONTINUES — purging frees
        REUSABLE PAGES (measured 2026-08-10, modernc v1.48.1: deleting
        1500×2 KiB rows left the file byte-identical with freelist=1509
        pages; re-inserting 1500 grew it 4 KiB — DELETE does not shrink
        SQLite) and new slots write into them, the file bounded under the
        cap — distinguishable from cap-pressure drop-new (nothing
        purgeable) and from metadata-pressure drop-new (RT6's budget
        exhausted; status names which).
   RT2  Every purge is recorded, atomically: each purge chunk commits its
        deletions AND its local-retention record (time range, counts by
        kind, reason, acknowledged status) in ONE transaction — a crash
        between them would be exactly the invisible gap this machinery
        exists to prevent. One record per chunk: records are immutable
        from birth (RT10), so no running record is ever updated.
   RT3  Coverage never orphans backwards: a slot's coverage row is never
        purged while that slot's observations remain; coverage purges
        only once its observations are gone AND the coverage row itself
        is cloud-present. The reverse orphan (decoded coverage whose
        observations were purged) is legitimate and self-explaining via
        the retention record.
   RT4  Unsynced drops stay honest (full §4.1 ruling: current capture
        wins once acked rows are exhausted): dropped never-offered rows
        record remote_status never_offered; offered-but-unacknowledged
        rows record offered_unacknowledged (the §5 offered_at
        distinction); QUARANTINED rows record `rejected` — known remotely
        absent, never offered_unacknowledged.
   RT5  Profiles are never purged: every remaining row still resolves its
        profile version locally (§4.2's promise survives purging).
   RT6  Metadata is bounded: adjacent same-kind loss/retention rows with
        agreeing reason/status/dial-context compact into a summary (new
        UUIDv7, ≤ 64 DIRECT predecessors, exact totals, outer time range)
        — summary insert + predecessor delete is one transaction; no
        metadata-about-metadata. 256 rows/kind is the compaction TRIGGER;
        the hard bound is the 4 MiB LOGICAL budget: when compaction
        cannot fit within it, NO invisible purge occurs — capture enters
        metadata-pressure drop-new and status says why.
   RT7  Retention records are first-class synced rows: the reserved
        `retention` wire kind activates end to end under the unchanged
        six-outcome contract. SMC supersession is tombstone-then-delete
        in one transaction (E12); a summary may itself become a
        compaction predecessor only after its own accepted /
        already_present — its supersession is known applied at SMC before
        it is locally replaced.
   RT8  Nothing waits: purge chunks are bounded (≤ 500 rows per
        transaction), queued slots take priority between chunks, WAL is
        folded with bounded checkpoints, and VACUUM never runs on the
        live path.
   RT9  Status stays honest: the operator can tell "capturing because
        purging" (retention stats) from cap-pressure drop-new from
        metadata-pressure drop-new.
   RT10 Offered means immutable: an OPEN loss accumulator is not
        sync-eligible — sealing freezes it, and no offered or synced UUID
        may subsequently change content. (Closes a latent §5 defect: the
        open accumulator's refreshed row was selectable for sync, and a
        content change after an offer would digest-conflict its own
        legitimate row into quarantine.)

   Schema v4 pins (V4 tests below): per-row terminal `sync_outcome`
   (synced=1 alone cannot distinguish cloud-present accepted /
   already_present — the ONLY purge-eligible class — from future
   tombstoned/suppressed, which are terminal but NOT present remotely);
   loss `sealed` (RT10) + `supersedes`; the retention_records table; a v3
   archive migrates additively (3→4), chaining from v1/v2.

   Constants (ratified 2026-08-10): 500 = MAXIMUM rows per purge
   transaction · 64 MiB reusable-page target clamped to half the
   watermark (NOT a file-size promise) · 256 compaction trigger · 64
   direct predecessors · 4 MiB logical metadata budget.
*/

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ackedArchive builds an archive whose rows are all CLOUD-PRESENT: boot 1
// with sync on + accept-all, fill, wait synced, stop. Returns final usage.
func ackedArchive(t *testing.T, cfg Config, smc *fakeSMC, slots int) int64 {
	t.Helper()
	c := cfg
	c.Sync, c.SyncURL, c.SyncToken = true, smc.ts.URL, "test-token"
	s := newRunning(t, c)
	for i := 0; i < slots; i++ {
		s.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s)
	waitFor(t, "archive fully acked", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE synced = 0`) == 0 &&
			countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 0`) == 0
	})
	s.Stop()
	return statUsage(t, cfg.Path)
}

// RT1/RT2/RT3/RT5 — at cap pressure with cloud-present history, capture
// CONTINUES: old acked observations purge oldest-first, each chunk writing
// its receipt atomically; coverage never orphans backwards; profiles are
// untouched; the file stays bounded under the cap.
func TestRT1_CaptureOutlivesTheCapByPurgingAcked(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	cfg.Antennas = []types.AntennaDecl{dxCommander()}
	usage := ackedArchive(t, cfg, smc, 30)

	db := openRaw(t, cfg.Path)
	obsBefore := countRows(t, db, `SELECT COUNT(*) FROM observations`)
	profBefore := countRows(t, db, `SELECT COUNT(*) FROM profiles`)

	// Boot 2: watermark pinned BELOW usage — every slot arrives at cap
	// pressure. With cloud-present history, they must LAND, not drop.
	cfg2 := cfg
	cfg2.Sync, cfg2.SyncURL, cfg2.SyncToken = true, smc.ts.URL, "test-token"
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	for j := 0; j < 4; j++ {
		s2.CaptureSlot(richSlot(slotAt((500 + j) * 15)))
	}
	drain(t, s2)

	for j := 0; j < 4; j++ {
		slotUTC := slotAt((500 + j) * 15).UTC().Format("2006-01-02T15:04:05Z")
		if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`, slotUTC); n != 1 {
			t.Fatalf("RT1: cap-pressure slot %d was DROPPED (no coverage row) — with acked history it must land by purging", j)
		}
	}
	if got := s2.Status().State; got != StateCapturing {
		t.Fatalf("RT1: state = %q under successful purge, want %q (capturing BECAUSE purging)", got, StateCapturing)
	}
	if u := s2.physicalUsage(); u > cfg2.CapBytes {
		t.Fatalf("RT1: physical usage %d exceeds the cap %d — purging must keep the file bounded", u, cfg2.CapBytes)
	}
	obsAfter := countRows(t, db, `SELECT COUNT(*) FROM observations`)
	if obsAfter >= obsBefore+4*3 {
		t.Fatalf("RT1: observations %d → %d — nothing was purged", obsBefore, obsAfter)
	}
	// RT2: receipts exist and their totals EXACTLY cover the deleted rows.
	var receiptObs, receiptCov int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(observations),0), COALESCE(SUM(coverage),0) FROM retention_records WHERE acknowledged = 1`).
		Scan(&receiptObs, &receiptCov); err != nil {
		t.Fatalf("RT2: read receipts: %v", err)
	}
	covNow := countRows(t, db, `SELECT COUNT(*) FROM coverage`)
	newRows := 4 * 3 // 4 slots × 3 rich decodes
	if receiptObs != obsBefore+newRows-obsAfter {
		t.Fatalf("RT2: receipts count %d purged observations; the archive lost %d — every purge must be receipted",
			receiptObs, obsBefore+newRows-obsAfter)
	}
	_ = covNow
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records WHERE acknowledged != 1`); n != 0 {
		t.Fatalf("RT2: %d receipts claim non-acknowledged purges in the acked fixture", n)
	}
	// RT3: no slot keeps observations while its coverage row is gone.
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations o WHERE NOT EXISTS
		   (SELECT 1 FROM coverage c WHERE c.slot_start_utc = o.slot_start_utc)`); n != 0 {
		t.Fatalf("RT3: %d observations orphaned of their coverage row", n)
	}
	// RT5: profiles are never purged.
	if n := countRows(t, db, `SELECT COUNT(*) FROM profiles`); n != profBefore {
		t.Fatalf("RT5: profiles %d → %d — profiles must never purge", profBefore, n)
	}
	// RT9: the retention surface reports the purging.
	st := s2.Status()
	if st.Retention == nil || st.Retention.PurgedObservations == 0 || st.Retention.Records == 0 {
		t.Fatalf("RT9: Status.Retention = %+v, want purge stats visible", st.Retention)
	}
	// RT7 (client half): the receipts are first-class synced rows — boot 2's
	// sync lane offers them to SMC like any other kind.
	waitFor(t, "retention receipts synced", func() bool {
		dbw := openRaw(t, cfg.Path)
		total := countRows(t, dbw, `SELECT COUNT(*) FROM retention_records`)
		return total > 0 && countRows(t, dbw, `SELECT COUNT(*) FROM retention_records WHERE synced = 1`) == total
	})
	s2.Stop()
}

// RT3 — coverage never orphans backwards, exercised where it can actually
// fail: every coverage row is cloud-present while every observation is
// still unsynced (the server acked coverage and left observations
// pending), so an uncoupled coverage purge would strip explanations from
// slots that still hold data. The guarded-against SELECT takes any
// cloud-present coverage; the correct one requires the slot's
// observations to be gone first.
func TestRT3_CoverageNeverOrphansBackwards(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		if rec.Kind == evidencewire.KindObservation {
			return evidencewire.RowOutcome{Outcome: "left_pending_by_test"}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}

	cfg := testConfig(t, true)
	c1 := cfg
	c1.Sync, c1.SyncURL, c1.SyncToken = true, smc.ts.URL, "test-token"
	s1 := newRunning(t, c1)
	for i := 0; i < 15; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	waitFor(t, "coverage acked, observations pending", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 0`) == 0 &&
			countRows(t, db, `SELECT COUNT(*) FROM observations WHERE synced = 1`) == 0
	})
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg // sync off: classes stand still while purging runs
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	db := openRaw(t, cfg.Path)
	for j := 0; j < 6; j++ {
		s2.CaptureSlot(richSlot(slotAt((800 + j) * 15)))
		drain(t, s2)
		// The invariant must hold after EVERY chunk, not just at the end.
		if n := countRows(t, db,
			`SELECT COUNT(*) FROM observations o WHERE NOT EXISTS
			   (SELECT 1 FROM coverage c WHERE c.slot_start_utc = o.slot_start_utc)`); n != 0 {
			t.Fatalf("RT3: %d observations lost their coverage row after chunk %d — coverage purged while its slot still held data", n, j)
		}
	}
	s2.Stop()
}

// RT6/RT9 — when the metadata budget cannot admit a receipt, NO invisible
// purge happens: capture enters metadata-pressure drop-new and status says
// exactly why — distinguishable from cap-pressure (nothing purgeable) and
// from healthy purging (state stays capturing).
func TestRT6_MetadataPressureRefusesInvisiblePurge(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0 // no receipt can ever fit
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 15; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	db := openRaw(t, cfg.Path)
	obsBefore := countRows(t, db, `SELECT COUNT(*) FROM observations`)

	cfg2 := cfg
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	s2.CaptureSlot(richSlot(slotAt(700 * 15)))
	drain(t, s2)

	st := s2.Status()
	if st.State != StateDropNew {
		t.Fatalf("RT6: state = %q with a zero receipt budget, want %q — an unreceipted purge must never happen", st.State, StateDropNew)
	}
	if st.Retention == nil || st.Retention.Pressure != pressureMetadata {
		t.Fatalf("RT6/RT9: Retention = %+v, want pressure %q — the operator must see WHY capture stopped", st.Retention, pressureMetadata)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != obsBefore {
		t.Fatalf("RT6: observations %d → %d under a zero budget — that purge left no receipt", obsBefore, n)
	}
	s2.Stop()
}

// RT4 — dropping UNSYNCED observations records the honest remote_status:
// never-offered rows (sync was off) drop as never_offered.
func TestRT4_NeverOfferedDropsRecordHonestly(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true) // sync OFF: rows never offered
	s1 := newRunning(t, cfg)
	for i := 0; i < 25; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	for j := 0; j < 3; j++ {
		s2.CaptureSlot(richSlot(slotAt((500 + j) * 15)))
	}
	drain(t, s2)
	s2.Stop()

	db := openRaw(t, cfg.Path)
	for j := 0; j < 3; j++ {
		slotUTC := slotAt((500 + j) * 15).UTC().Format("2006-01-02T15:04:05Z")
		if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`, slotUTC); n != 1 {
			t.Fatalf("RT4: current slot %d dropped — full §4.1: current capture wins over old unacked history", j)
		}
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'never_offered' AND sealed = 1 AND observations > 0`); n == 0 {
		t.Fatal("RT4: unsynced never-offered drops must be receipted as never_offered loss intervals")
	}
}

// RT4 — offered-but-unacknowledged rows drop as offered_unacknowledged;
// QUARANTINED rows drop as `rejected` (known remotely absent), never
// offered_unacknowledged.
func TestRT4_OfferedAndRejectedClassesRecordHonestly(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 30*time.Millisecond, 100*time.Millisecond)
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)
	var mu sync.Mutex
	var poisoned string
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		mu.Lock()
		defer mu.Unlock()
		if rec.Kind == evidencewire.KindObservation {
			if poisoned == "" {
				poisoned = rec.UUID
			}
			if rec.UUID == poisoned {
				return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: "digest_conflict"}
			}
		}
		// Nothing else ever succeeds: every offered row stays unacknowledged.
		return evidencewire.RowOutcome{Outcome: "unrecognized_outcome_left_pending"}
	}

	cfg := testConfig(t, true)
	c1 := cfg
	c1.Sync, c1.SyncURL, c1.SyncToken = true, smc.ts.URL, "test-token"
	s1 := newRunning(t, c1)
	for i := 0; i < 25; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	waitFor(t, "rows offered and one quarantined", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE quarantine_reason IS NOT NULL`) == 1 &&
			countRows(t, db, `SELECT COUNT(*) FROM observations WHERE offered_at IS NOT NULL`) > 10
	})
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg // sync OFF in boot 2: classes are already stamped
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	// Enough current slots to chew through the whole unsynced backlog.
	j := 0
	waitFor(t, "the quarantined row purged", func() bool {
		s2.CaptureSlot(richSlot(slotAt((500 + j) * 15)))
		j++
		drain(t, s2)
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE quarantine_reason IS NOT NULL`) == 0
	})
	s2.Stop()

	db := openRaw(t, cfg.Path)
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'offered_unacknowledged' AND observations > 0`); n == 0 {
		t.Fatal("RT4: offered-unacked drops must be receipted as offered_unacknowledged")
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'rejected' AND observations = 1`); n != 1 {
		t.Fatal("RT4: the quarantined drop must be receipted as `rejected` (known remotely absent), never offered_unacknowledged")
	}
}

// RT6 — compaction: >trigger agreeing rows compact into summaries with
// EXACT totals and ≤ 64 direct predecessors, atomically.
func TestRT6_CompactionPreservesExactTotals(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 10; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()

	// 300 sealed, agreeing, PLAIN loss rows (mass fixture).
	db := openRaw(t, cfg.Path)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		if _, err := tx.Exec(
			`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed)
			 VALUES (?, ?, ?, 2, 5, 'cap', 'never_offered', 14.074, 1)`,
			// Zero-padded so lexical uuid order matches time order.
			pad("loss", i), slotAt(i*30).UTC().Format("2006-01-02T15:04:05Z"),
			slotAt(i*30+30).UTC().Format("2006-01-02T15:04:05Z")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	usage := statUsage(t, cfg.Path)

	// Cap pressure drives purge chunks, whose receipt path runs compaction
	// past the trigger.
	cfg2 := cfg
	cfg2.CapBytes = usage + headroomBytes/2
	s2 := newRunning(t, cfg2)
	j := 0
	waitFor(t, "compaction fired", func() bool {
		s2.CaptureSlot(richSlot(slotAt((900 + j) * 15)))
		j++
		drain(t, s2)
		dbw := openRaw(t, cfg.Path)
		return countRows(t, dbw, `SELECT COUNT(*) FROM loss_intervals WHERE supersedes IS NOT NULL`) > 0
	})
	s2.Stop()

	var slots, obs int
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(slots),0), COALESCE(SUM(observations),0) FROM loss_intervals WHERE remote_status = 'never_offered'`).
		Scan(&slots, &obs); err != nil {
		t.Fatal(err)
	}
	// Totals are EXACT through compaction: 300 × (2 slots, 5 observations),
	// plus nothing else under this remote_status... any cap drops in boot 2
	// would add rows, so assert ≥ and the summary shape instead.
	if slots < 600 || obs < 1500 {
		t.Fatalf("RT6: totals lost through compaction: slots=%d (≥600) obs=%d (≥1500)", slots, obs)
	}
	var supersedes string
	if err := db.QueryRow(
		`SELECT supersedes FROM loss_intervals WHERE supersedes IS NOT NULL LIMIT 1`).Scan(&supersedes); err != nil {
		t.Fatal(err)
	}
	var preds []string
	if err := json.Unmarshal([]byte(supersedes), &preds); err != nil || len(preds) == 0 || len(preds) > 64 {
		t.Fatalf("RT6: summary supersedes %d direct predecessors (err %v), want 1..64", len(preds), err)
	}
	// The superseded predecessors are GONE (atomic replacement).
	if n := countRows(t, db, `SELECT COUNT(*) FROM loss_intervals WHERE uuid = ?`, preds[0]); n != 0 {
		t.Fatal("RT6: a superseded predecessor survived its summary")
	}
}

func pad(prefix string, i int) string {
	return fmt.Sprintf("%s-%06d", prefix, i)
}

// Schema v4 — the exact terminal outcome persists with synced=1: the purge
// eligibility class is accepted|already_present (cloud-present), and
// synced alone cannot carry that distinction once tombstoned/suppressed
// exist (§8).
func TestV4_TerminalOutcomePersisted(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	smc := newFakeSMC(t)
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		if rec.Kind == evidencewire.KindCoverage {
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAlreadyPresent}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}
	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	waitFor(t, "slot synced", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE synced = 1`) == 1
	})
	db := openRaw(t, cfg.Path)
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE synced = 1 AND sync_outcome = 'accepted'`); n != 1 {
		t.Fatalf("V4: accepted observation must persist sync_outcome='accepted' (got %d)", n)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM coverage WHERE synced = 1 AND sync_outcome = 'already_present'`); n != 1 {
		t.Fatalf("V4: already_present coverage must persist its EXACT outcome, not a boolean (got %d)", n)
	}
}

// RT10 — an OPEN loss accumulator is not sync-eligible; sealing freezes it.
// The guarded-against §5 defect: the open row is refreshed IN PLACE under
// one UUID while it grows, so offering it while open means its next
// refresh changes offered content — and the eventual re-offer would
// digest-conflict a legitimate row into quarantine.
func TestRT10_OpenLossAccumulatorIsNotSyncEligible(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 4096
	defer func() { headroomBytes = oldHeadroom }()
	// The retention engine would PURGE instead of dropping (full §4.1);
	// this fixture needs guaranteed drops, so the zero metadata budget
	// refuses purging (no receipt capacity → metadata-pressure drop-new).
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0
	defer func() { metadataBudgetBytes = oldBudget }()
	smc := newFakeSMC(t)

	// Boot 1, sync OFF: build real bulk so boot 2 can pin usage ABOVE the
	// watermark permanently — dropped slots write nothing that frees space,
	// so no resume (and no legitimate seal) can occur mid-fixture. An
	// earlier fixture let usage oscillate at the boundary; a transient dip
	// sealed the interval LEGITIMATELY and the test misread correct
	// behaviour as the defect.
	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 20; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.Sync, cfg2.SyncURL, cfg2.SyncToken = true, smc.ts.URL, "test-token"
	cfg2.CapBytes = usage // watermark = usage − headroom: permanently exceeded
	s2 := newRunning(t, cfg2)
	for j := 0; j < 5; j++ {
		s2.CaptureSlot(richSlot(slotAt((100 + j) * 15))) // every one DROPS
	}
	drain(t, s2)

	// The LIVE accumulator's identity, from the service itself.
	s2.mu.Lock()
	if s2.loss == nil {
		s2.mu.Unlock()
		t.Fatal("fixture failure: no open loss accumulator after guaranteed drops")
	}
	lossUUID := s2.loss.uuid
	s2.mu.Unlock()

	// Several sync periods pass; the OPEN row must never leave the machine.
	time.Sleep(150 * time.Millisecond)
	db := openRaw(t, cfg.Path)
	if n := smc.sawUUID(lossUUID); n != 0 {
		var sealed, synced int
		_ = db.QueryRow(`SELECT sealed, synced FROM loss_intervals WHERE uuid = ?`, lossUUID).Scan(&sealed, &synced)
		t.Fatalf("RT10: the OPEN loss accumulator was offered %d time(s) (row sealed=%d synced=%d) — its content still changes under that UUID", n, sealed, synced)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE uuid = ? AND sealed = 0 AND synced = 0`, lossUUID); n != 1 {
		t.Fatalf("fixture: the accumulator row must exist open (sealed=0, synced=0)")
	}

	// Stop SEALS the accumulator (closeLossLocked); the next boot's backlog
	// lane offers the now-frozen row — sealing freezes, it does not bury.
	s2.Stop()
	cfg3 := cfg2
	cfg3.CapBytes = 8 << 20 // roomy again; only sync matters now
	s3 := newRunning(t, cfg3)
	waitFor(t, "sealed loss row synced", func() bool {
		dbw := openRaw(t, cfg.Path)
		return countRows(t, dbw, `SELECT COUNT(*) FROM loss_intervals WHERE uuid = ? AND synced = 1`, lossUUID) == 1
	})
	s3.Stop()
	if n := smc.sawUUID(lossUUID); n == 0 {
		t.Fatal("RT10: the SEALED loss row must sync")
	}
}

const v3SchemaProbe = `SELECT sealed, supersedes, sync_outcome FROM loss_intervals LIMIT 0`

func TestMigration_V3ToV4AdditivePreservation(t *testing.T) {
	cfg := testConfig(t, true)

	// A genuine v3 archive: boot once on the CURRENT build is impossible
	// (it would create v4), so freeze the v3 shape via the v2 fixture + the
	// real 2→3 path… simplest honest fixture: create v2 verbatim, let the
	// old chain bring it to v3? The chain now lands at v4. So build the v3
	// archive DIRECTLY: v2SchemaForTest + the frozen 2→3 ALTERs below.
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v2SchemaForTest); err != nil {
		t.Fatal(err)
	}
	// The v2→v3 ALTERs frozen VERBATIM (must never track schema.go).
	if _, err := raw.Exec(`
ALTER TABLE observations ADD COLUMN offered_at TEXT;
ALTER TABLE observations ADD COLUMN quarantine_reason TEXT;
ALTER TABLE coverage ADD COLUMN offered_at TEXT;
ALTER TABLE coverage ADD COLUMN quarantine_reason TEXT;
ALTER TABLE loss_intervals ADD COLUMN offered_at TEXT;
ALTER TABLE loss_intervals ADD COLUMN quarantine_reason TEXT;
ALTER TABLE profiles ADD COLUMN offered_at TEXT;
ALTER TABLE profiles ADD COLUMN quarantine_reason TEXT;
UPDATE schema_meta SET v = '3' WHERE k = 'schema_version';`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, synced)
		 VALUES ('v3-loss-a', '2026-08-01T12:00:00Z', '2026-08-01T12:01:00Z', 4, 9, 'cap', 'never_offered', 14.074, 1)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s := newRunning(t, cfg)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "4" {
		t.Fatalf("V4: schema_version = %q (err %v), want \"4\"", ver, err)
	}
	// The new columns exist and the v3 row survived with them NULL/sealed.
	if _, err := db.Query(v3SchemaProbe); err != nil {
		t.Fatalf("V4: loss_intervals is missing v4 columns: %v", err)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE uuid = 'v3-loss-a' AND synced = 1
		   AND sealed = 1 AND supersedes IS NULL AND sync_outcome IS NULL`); n != 1 {
		t.Fatal("V4: the v3 loss row must survive SEALED (pre-migration rows are frozen by definition), un-superseded, outcome unknown")
	}
	for _, table := range []string{"observations", "coverage", "profiles", "retention_records"} {
		if !hasColumn(t, db, table, "sync_outcome") {
			t.Fatalf("V4: %s.sync_outcome missing", table)
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records`); n != 0 {
		t.Fatalf("V4: fresh retention_records must be empty, has %d rows", n)
	}
}
