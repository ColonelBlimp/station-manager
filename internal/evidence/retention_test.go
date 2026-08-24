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
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/logging"
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
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
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
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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
	if u, _ := s2.physicalUsage(); u > cfg2.CapBytes {
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
	if st.Retention == nil || st.Retention.PurgedObservations == nil || st.Retention.Records == nil || *st.Retention.PurgedObservations == 0 || *st.Retention.Records == 0 {
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

// Package-review P1 (2026-08-10): after retention purging, the file sits
// at the watermark FOREVER (DELETE does not shrink SQLite) while reusable
// pages carry the real capacity — activation must read them like every
// slot write does, or the first restart after a purge permanently
// degrades profiles (every new row profile_error until the cap is raised
// or an external VACUUM).
func TestActivation_AcceptsReusablePagesAtWatermark(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	cfg.Antennas = []types.AntennaDecl{dxCommander()}
	ackedArchive(t, cfg, smc, 100) // boot 1: profiles active, all acked

	// Manufacture the post-purge state DIRECTLY (how it arises is the purge
	// engine's own test surface; the GATE must accept the state): delete the
	// OLDEST third — interior pages free, the newest rows pin the tail, so
	// the file keeps its size while the freelist grows. An organic fixture
	// kept dodging this: full purges free trailing pages and SQLite
	// truncates the file, collapsing usage below the watermark.
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	// VACUUM first (fixture prep only — the LIVE path never runs it): boot
	// WAL-fold slack otherwise collapses on the first in-test fold and
	// dissolves the pinned pressure. The MIDDLE deletion then builds the
	// freelist from pages that can never reach the file tail (post-VACUUM
	// physical order is table order), so no commit can truncate them away.
	if _, err := raw.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`DELETE FROM observations WHERE uuid IN
		   (SELECT uuid FROM observations ORDER BY uuid ASC LIMIT 100 OFFSET 100)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn // usage ≥ watermark by construction
	s3 := newRunning(t, cfg2)
	if fb, _ := s3.freelistBytes(); fb < slotWriteReserveBytes {
		s3.Stop()
		t.Fatal("fixture failure: no reusable pages in the manufactured state")
	}
	st := s3.Status()
	if st.Profiles == nil || st.Profiles.State != ProfilesActive {
		t.Fatalf("P1: profiles = %+v at watermark-with-freelist, want active — the gate must read reusable pages, not just file size", st.Profiles)
	}
	s3.Stop()
}

// Package-review P1-4a (2026-08-10): a receipt's range covers its LAST
// slot — end_utc = max slot start + the 15 s slot duration. Recording the
// start as the end makes a one-slot purge a zero-length interval and
// understates every range by one slot.
func TestReceipt_EndCoversTheLastSlot(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 64 * 1024
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	usage := ackedArchive(t, cfg, smc, 10) // slots at 0..135 s

	cfg2 := cfg
	cfg2.CapBytes = usage + 48*1024
	s := newRunning(t, cfg2)
	s.CaptureSlot(richSlot(slotAt(900 * 15)))
	drain(t, s)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var start, end string
	if err := db.QueryRow(
		`SELECT start_utc, end_utc FROM retention_records ORDER BY uuid ASC LIMIT 1`).
		Scan(&start, &end); err != nil {
		t.Fatalf("no receipt: %v", err)
	}
	wantEnd := slotAt(9 * 15).Add(slotDuration).UTC().Format(time.RFC3339)
	if end != wantEnd {
		t.Fatalf("P1: receipt range [%s, %s], want end %s — the last slot's 15 s belong to the interval", start, end, wantEnd)
	}
}

// Package-review P1-4b: compaction requires TEMPORAL adjacency —
// prev.end == next.start — not just agreeing keys; merging across a gap
// makes separated intervals look continuous, which is exactly the
// dishonesty the records exist to prevent.
func TestCompaction_RequiresTemporalAdjacency(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 64 * 1024
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 10; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()

	// 300 agreeing sealed loss rows: contiguous chains on either side of a
	// ONE-HOUR gap between #149 and #150.
	db := openRaw(t, cfg.Path)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	// The gap sits INSIDE the first compaction window (row 32 of the ≤64
	// run): the first summary either bridges it (the defect) or stops at it
	// (the rule) — later windows never form, because one compaction drops
	// the count below the 256 trigger.
	gap := 3600
	for i := 0; i < 300; i++ {
		off := i * 30
		if i >= 32 {
			off += gap
		}
		if _, err := tx.Exec(
			`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed)
			 VALUES (?, ?, ?, 2, 5, 'cap', 'never_offered', 14.074, 1)`,
			pad("gaploss", i), slotAt(0).Add(time.Duration(off)*time.Second).UTC().Format(time.RFC3339),
			slotAt(0).Add(time.Duration(off+30)*time.Second).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage + 48*1024
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

	gapLeftEnd := slotAt(0).Add(time.Duration(31*30+30) * time.Second).UTC().Format(time.RFC3339)
	gapRightStart := slotAt(0).Add(time.Duration(32*30+gap) * time.Second).UTC().Format(time.RFC3339)
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE supersedes IS NOT NULL AND start_utc <= ? AND end_utc >= ?`,
		gapLeftEnd, gapRightStart); n != 0 {
		t.Fatalf("P1: %d summaries bridge the one-hour gap — separated intervals became one continuous-looking range", n)
	}
}

// Package-review P1-4c: retention receipts carry dial context (schema v5)
// and compaction never merges receipts across dials. Driven at the chunk /
// compaction seam directly — the pressure-driven path is RT1's surface,
// and on test-sized archives its file-truncation physics keeps dissolving
// the pressure this rule needs; the archive rows are the assertion surface
// either way.
func TestReceipt_DialContextRecordedAndSeparated(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldChunk := purgeChunkMaxRows
	purgeChunkMaxRows = 20
	defer func() { purgeChunkMaxRows = oldChunk }()
	smc := newFakeSMC(t)

	// Acked history on TWO dials, time-ordered: 20 slots on 80 m, then 20
	// on 20 m — 20-row chunks purge one dial's rows at a time.
	cfg := testConfig(t, true)
	c1 := cfg
	c1.Sync, c1.SyncURL, c1.SyncToken = true, smc.ts.URL, "test-token"
	s := newRunning(t, c1)
	for i := 0; i < 20; i++ {
		s.CaptureSlot(obsSlot(slotAt(i*15), 3.573, true))
	}
	for i := 20; i < 40; i++ {
		s.CaptureSlot(obsSlot(slotAt(i*15), 14.074, true))
	}
	drain(t, s)
	waitFor(t, "all acked", func() bool {
		dbw := openRaw(t, cfg.Path)
		return countRows(t, dbw, `SELECT COUNT(*) FROM observations WHERE synced = 0`) == 0
	})

	for i := 0; i < 2; i++ {
		purged, err := s.purgeAckedChunk()
		if err != nil || !purged {
			t.Fatalf("chunk %d: purged=%v err=%v", i, purged, err)
		}
	}
	db := openRaw(t, cfg.Path)
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records WHERE dial_mhz = 3.573`); n == 0 {
		t.Fatal("P1: no receipt carries the 80 m dial — receipts must record their dial context")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records WHERE dial_mhz = 14.074`); n == 0 {
		t.Fatal("P1: no receipt carries the 20 m dial")
	}

	// Compaction separation: 300 otherwise-agreeing, temporally CONTIGUOUS
	// receipts switching dial at #150 — the window crossing the boundary
	// (a 64-run starting at #128) must not merge across it.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	base := slotAt(5000)
	for i := 0; i < 300; i++ {
		dial := 3.573
		if i >= 150 {
			dial = 14.074
		}
		if _, err := tx.Exec(
			`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged, dial_mhz)
			 VALUES (?, ?, ?, 3, 1, 'cap', 1, ?)`,
			pad("dialret", i),
			base.Add(time.Duration(i*30)*time.Second).UTC().Format(time.RFC3339),
			base.Add(time.Duration((i+1)*30)*time.Second).UTC().Format(time.RFC3339), dial); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = s.compactOnce() // each pass consumes one ≤64 window
	}
	s.Stop()

	boundary := base.Add(time.Duration(150*30) * time.Second).UTC().Format(time.RFC3339)
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM retention_records WHERE supersedes IS NOT NULL AND start_utc < ? AND end_utc > ?`,
		boundary, boundary); n != 0 {
		t.Fatalf("P1: %d summaries merge receipts ACROSS the dial change — cross-band ranges must never look continuous", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records WHERE supersedes IS NOT NULL`); n == 0 {
		t.Fatal("fixture failure: compaction never fired on the seeded receipts")
	}
}

// Package-review P2 (2026-08-10): unsynced purging is GLOBALLY
// oldest-first — the chunk takes the oldest rows regardless of class and
// splits the SELECTED set into per-class receipts. The guarded-against
// design picks the oldest row's class then deletes up to 500 of THAT
// class, dropping newer matching rows while older rows of another class
// survive.
func TestUnsyncedPurge_GloballyOldestFirst(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 64 * 1024
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	for i := 0; i < 20; i++ {
		s.CaptureSlot(obsSlot(slotAt(i*15), 14.074, true))
	}
	drain(t, s)
	// Alternate the class by time: even slots stay never-offered, odd slots
	// become offered-unacknowledged.
	db := openRaw(t, cfg.Path)
	for i := 1; i < 20; i += 2 {
		if _, err := db.Exec(
			`UPDATE observations SET offered_at = '2026-08-10T00:00:00.000000001Z'
			  WHERE slot_start_utc = ?`, slotAt(i*15).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}

	purged, err := s.purgeUnsyncedChunk()
	if err != nil || !purged {
		t.Fatalf("purged=%v err=%v", purged, err)
	}
	s.Stop()

	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != 0 {
		t.Fatalf("P2: %d observations survived a chunk with room for all 20 — the purge skipped older rows of the other class", n)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'never_offered' AND observations = 10`); n != 1 {
		t.Fatal("P2: want one never_offered receipt covering its 10 rows")
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'offered_unacknowledged' AND observations = 10`); n != 1 {
		t.Fatal("P2: want one offered_unacknowledged receipt covering its 10 rows")
	}
}

// Package-review P2: the SQLite pragmas are connection-scoped — every
// POOLED connection must carry them, or concurrent writers hit default
// (zero) busy_timeout and fail immediately with SQLITE_BUSY.
func TestPragmas_ApplyToEveryPooledConnection(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	defer s.Stop()

	ctx := context.Background()
	var conns []*sql.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		c, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c) // held open so each is a DISTINCT pooled conn
		var timeout int64
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 2000 {
			t.Fatalf("P2: pooled connection %d has busy_timeout %d, want 2000 — pragmas applied once via sql.DB reach one connection only", i, timeout)
		}
	}
}

// TestOpenRawWrite_WaitsOutHeldWriteLock is the deterministic core of the CI
// flake in TestReceipt_DialContextRecordedAndSeparated (W-0017 sub-item B): while
// one connection holds evidence.db's single WAL write lock, a raw connection with
// no busy handling fails immediately with SQLITE_BUSY, whereas one with a
// busy_timeout waits the lock out and succeeds once it is released. The contention
// is created deterministically — BEGIN IMMEDIATE takes the write lock up front, so
// it is provably held before the contended write is attempted — and the wait is
// bounded by ctx, with no sleep and no enlarged deadline standing in for the
// synchronization. Reverting openRaw's busy handling turns the contended write
// back into an immediate SQLITE_BUSY and reddens this test.
func TestOpenRawWrite_WaitsOutHeldWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.db")

	seed := openRaw(t, path)
	if _, err := seed.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Hold the single WAL write lock on a dedicated connection until we release
	// it. BEGIN IMMEDIATE acquires the write lock up front (no read-then-upgrade),
	// so the contention below is guaranteed, not timing-dependent.
	holder, err := seed.Conn(context.Background())
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := holder.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("acquire write lock: %v", err)
	}

	// A generous bound so a genuinely wedged wait fails the test instead of
	// hanging — the synchronization is the lock release below, not this deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	writer := openRaw(t, path) // busy handling after the fix; a bare connection before it
	// Pin and pre-open the writer's single connection so the contended write below
	// hits the lock immediately, instead of paying lazy connection setup first and
	// losing the race to the release — that setup cost is what makes a naive
	// version pass even without busy handling.
	writer.SetMaxOpenConns(1)
	if err := writer.PingContext(context.Background()); err != nil {
		t.Fatalf("open writer conn: %v", err)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(entered) // about to attempt the contended write on the warm connection
		_, err := writer.ExecContext(ctx, `INSERT INTO t (id) VALUES (2)`)
		done <- err
	}()

	// Let the writer reach its lock acquisition before releasing, so a
	// no-busy-handling writer has provably contended (and failed fast) first.
	<-entered
	runtime.Gosched()

	// Release the contention: a waiting busy-handling writer now proceeds; a bare
	// writer has already returned SQLITE_BUSY.
	if _, err := holder.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatalf("release write lock: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("contended write failed instead of waiting out the lock: %v", err)
	}
	if n := countRows(t, seed, `SELECT COUNT(*) FROM t`); n != 1 {
		t.Fatalf("rows = %d, want 1 — the writer's insert must land after the lock releases", n)
	}
}

// Package-review P2: a 200 response is decoded through a byte cap, and a
// quarantine reason is bounded before persisting — an erroneous or
// compromised endpoint must not consume daemon memory or write megabytes
// into the archive.
func TestSyncResponse_Bounded(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 30*time.Millisecond, 100*time.Millisecond)
	smc := newFakeSMC(t)
	hugeReason := strings.Repeat("r", 100_000)
	smc.script = func(rec evidencewire.Record) evidencewire.RowOutcome {
		if rec.Kind == evidencewire.KindObservation {
			return evidencewire.RowOutcome{Outcome: evidencewire.OutcomePermanentReject, Reason: hugeReason}
		}
		return evidencewire.RowOutcome{Outcome: evidencewire.OutcomeAccepted}
	}

	cfg := syncedConfig(t, smc.ts.URL)
	s := newRunning(t, cfg)
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	drain(t, s)
	waitFor(t, "quarantine recorded", func() bool {
		db := openRaw(t, cfg.Path)
		return countRows(t, db, `SELECT COUNT(*) FROM observations WHERE quarantine_reason IS NOT NULL`) == 1
	})
	db := openRaw(t, cfg.Path)
	var stored string
	if err := db.QueryRow(
		`SELECT quarantine_reason FROM observations WHERE quarantine_reason IS NOT NULL`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) > 512 {
		t.Fatalf("P2: quarantine_reason persisted %d bytes — an unbounded remote reason must be truncated", len(stored))
	}

	// The body cap itself: a response padded past the cap truncates
	// mid-document, fails the decode, and consumes NOTHING — the
	// guarded-against decoder reads it whole, skips the unknown padding
	// field, and consumes the rows.
	smc.mu.Lock()
	smc.script = nil
	smc.respond = func(recs []evidencewire.Record) (int, any) {
		resp := map[string]any{"outcomes": []evidencewire.RowOutcome{}, "padding": strings.Repeat("x", 2<<20)}
		outs := []evidencewire.RowOutcome{}
		for _, r := range recs {
			outs = append(outs, evidencewire.RowOutcome{UUID: r.UUID, Outcome: evidencewire.OutcomeAccepted})
		}
		resp["outcomes"] = outs
		return http.StatusOK, resp
	}
	smc.mu.Unlock()
	s.CaptureSlot(obsSlot(slotAt(15), 14.074, true))
	drain(t, s)
	waitFor(t, "over-cap response attempted", func() bool { return smc.requestCount() >= 2 })
	time.Sleep(60 * time.Millisecond)
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE synced = 1`); n != 0 {
		t.Fatalf("P2: %d rows consumed from an over-cap response body — the cap must invalidate it", n)
	}
	s.Stop()
}

// Schema v5: a v4 archive (retention_records without dial_mhz) migrates
// additively — the column arrives with DEFAULT 0 (unattributed, honest for
// pre-v5 receipts) and every row survives. v4 is DEPLOYED (dogfood,
// 2026-08-10), so this is a real migration, not an in-place edit.
func TestMigration_V4ToV5AdditivePreservation(t *testing.T) {
	cfg := testConfig(t, true)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v2SchemaForTest); err != nil {
		t.Fatal(err)
	}
	// The 2→3 and 3→4 steps frozen VERBATIM (must never track schema.go).
	if _, err := raw.Exec(`
ALTER TABLE observations ADD COLUMN offered_at TEXT;
ALTER TABLE observations ADD COLUMN quarantine_reason TEXT;
ALTER TABLE coverage ADD COLUMN offered_at TEXT;
ALTER TABLE coverage ADD COLUMN quarantine_reason TEXT;
ALTER TABLE loss_intervals ADD COLUMN offered_at TEXT;
ALTER TABLE loss_intervals ADD COLUMN quarantine_reason TEXT;
ALTER TABLE profiles ADD COLUMN offered_at TEXT;
ALTER TABLE profiles ADD COLUMN quarantine_reason TEXT;
ALTER TABLE observations ADD COLUMN sync_outcome TEXT;
ALTER TABLE coverage ADD COLUMN sync_outcome TEXT;
ALTER TABLE loss_intervals ADD COLUMN sync_outcome TEXT;
ALTER TABLE loss_intervals ADD COLUMN sealed INTEGER NOT NULL DEFAULT 1;
ALTER TABLE loss_intervals ADD COLUMN supersedes TEXT;
ALTER TABLE profiles ADD COLUMN sync_outcome TEXT;
CREATE TABLE retention_records (
	uuid TEXT PRIMARY KEY, start_utc TEXT NOT NULL, end_utc TEXT NOT NULL,
	observations INTEGER NOT NULL, coverage INTEGER NOT NULL,
	reason TEXT NOT NULL, acknowledged INTEGER NOT NULL, supersedes TEXT,
	synced INTEGER NOT NULL DEFAULT 0, offered_at TEXT,
	quarantine_reason TEXT, sync_outcome TEXT
);
UPDATE schema_meta SET v = '4' WHERE k = 'schema_version';`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged)
		 VALUES ('v4-ret-a', '2026-08-10T10:00:00Z', '2026-08-10T11:00:00Z', 12, 24, 'cap', 1)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s := newRunning(t, cfg)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "5" {
		t.Fatalf("V5: schema_version = %q (err %v), want \"5\"", ver, err)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM retention_records WHERE uuid = 'v4-ret-a' AND observations = 12 AND dial_mhz = 0`); n != 1 {
		t.Fatal("V5: the v4 receipt must survive with dial_mhz 0 — unattributed is honest for a pre-v5 receipt")
	}
}

// Package-review P1 (2026-08-10): reusable pages must never authorize a
// write AT or PAST the hard cap — freelist reuse keeps the db file from
// growing, but the WAL still grows per write, and if checkpointing is
// blocked the total climbs while freelist pages keep saying yes. The cap
// is a ceiling, not a suggestion.
func TestCap_HardCeilingRefusesWritesEvenWithFreelist(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 48 * 1024 // real vacuumed bytes: shm-width headroom; the 64K write reserve is the cap margin
	defer func() { headroomBytes = oldHeadroom }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	ackedArchive(t, cfg, smc, 100)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	// VACUUM first (fixture prep only — the LIVE path never runs it): boot
	// WAL-fold slack otherwise collapses on the first in-test fold and
	// dissolves the pinned pressure. The MIDDLE deletion then builds the
	// freelist from pages that can never reach the file tail (post-VACUUM
	// physical order is table order), so no commit can truncate them away.
	if _, err := raw.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`DELETE FROM observations WHERE uuid IN
		   (SELECT uuid FROM observations ORDER BY uuid ASC LIMIT 100 OFFSET 100)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	usage := statUsage(t, cfg.Path)

	// Purging must not be able to fix the overage, or it legitimately
	// brings usage under the cap and the ceiling never faces its test: a
	// zero receipt budget refuses every purge.
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg2 := cfg
	cfg2.CapBytes = usage - usage/10 // usage is ALREADY past the cap; freelist exists
	s := newRunning(t, cfg2)
	if fb, _ := s.freelistBytes(); fb < slotWriteReserveBytes {
		s.Stop()
		t.Fatal("fixture failure: no reusable pages")
	}
	s.CaptureSlot(richSlot(slotAt(900 * 15)))
	drain(t, s)

	db := openRaw(t, cfg.Path)
	slotUTC := slotAt(900 * 15).UTC().Format("2006-01-02T15:04:05Z")
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`, slotUTC); n != 0 {
		t.Fatal("P1: a slot was WRITTEN at/past the hard cap — freelist pages must not override the ceiling")
	}
	if st := s.Status(); st.State != StateDropNew {
		t.Fatalf("P1: state = %q past the hard cap, want %q", st.State, StateDropNew)
	}
	s.Stop()
}

// Review P1 on ab9868cc: the ceiling RESERVES one transaction's WAL
// growth — a write authorized one byte below the cap still appends its
// frames past it. Within the reserve of the cap, capture drops.
func TestCap_CeilingReservesWriteGrowth(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 96 * 1024 // ≥ write reserve, so the watermark sits at/below usage and the CEILING trips
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0 // purging must not dissolve the boundary
	defer func() { metadataBudgetBytes = oldBudget }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	ackedArchive(t, cfg, smc, 100)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	// VACUUM first (fixture prep only — the LIVE path never runs it): boot
	// WAL-fold slack otherwise collapses on the first in-test fold and
	// dissolves the pinned pressure. The MIDDLE deletion then builds the
	// freelist from pages that can never reach the file tail (post-VACUUM
	// physical order is table order), so no commit can truncate them away.
	if _, err := raw.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`DELETE FROM observations WHERE uuid IN
		   (SELECT uuid FROM observations ORDER BY uuid ASC LIMIT 100 OFFSET 100)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	// Runtime usage (db + 32 KiB shm + WAL) sits BELOW this cap but inside
	// the write-growth reserve — the exact band the un-fixed check passed.
	cfg2.CapBytes = usage + 64*1024
	s := newRunning(t, cfg2)
	if fb, _ := s.freelistBytes(); fb < slotWriteReserveBytes {
		s.Stop()
		t.Fatal("fixture failure: no reusable pages")
	}
	s.CaptureSlot(richSlot(slotAt(900 * 15)))
	drain(t, s)

	db := openRaw(t, cfg.Path)
	slotUTC := slotAt(900 * 15).UTC().Format("2006-01-02T15:04:05Z")
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`, slotUTC); n != 0 {
		t.Fatal("P1: a slot was written INSIDE the cap's write-growth reserve — its WAL frames land past the ceiling")
	}
	s.Stop()
}

// Review P1 on 85f1481a: the drop-path checkpoint must not hold s.mu — a
// reader can block a truncating checkpoint up to the 2 s busy_timeout, and
// CaptureSlot needs s.mu to stamp, so a checkpoint under the lock stalls
// the decode path (the same class as TestStatus_NeverBlocksCapture).
func TestDropCheckpoint_NeverBlocksCapture(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 48 * 1024
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0 // no purge dissolves the pressure
	defer func() { metadataBudgetBytes = oldBudget }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	ackedArchive(t, cfg, smc, 100)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`DELETE FROM observations WHERE uuid IN
		   (SELECT uuid FROM observations ORDER BY uuid ASC LIMIT 100 OFFSET 100)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage - usage/10 // usage past the cap → the drop path defers + checkpoints
	s := newRunning(t, cfg2)

	entered := make(chan struct{})
	var once sync.Once
	checkpointHook = func() {
		once.Do(func() { close(entered) })
		time.Sleep(300 * time.Millisecond)
	}
	defer func() { checkpointHook = nil }()

	s.CaptureSlot(richSlot(slotAt(900 * 15))) // dropped → the writer enters the checkpoint
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the writer never reached the drop checkpoint — the fixture did not enter the deferred band")
	}

	// While the writer sits in the checkpoint, a concurrent CaptureSlot's
	// stamp must acquire s.mu freely.
	start := time.Now()
	s.CaptureSlot(richSlot(slotAt(901 * 15)))
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("P1: CaptureSlot blocked %v while the drop checkpoint ran — the checkpoint holds s.mu across the decode path", elapsed)
	}
	s.Stop()
}

// Review P1 on 8efbb2fe: drop-new itself must not consume the reserve —
// each dropped slot refreshes the loss accumulator's row, and unfolded,
// those WAL frames cross the cap within a handful of drops. §4.1's answer
// is already written: under pressure the accumulator extends IN MEMORY
// (documented crash-limit) and the WAL folds; the record persists with
// priority at recovery/Stop.
func TestCap_SustainedDropsStayUnderTheCap(t *testing.T) {
	dialSync(t, 10*time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, time.Second)
	oldHeadroom := headroomBytes
	headroomBytes = 96 * 1024 // ≥ write reserve, so the watermark sits at/below usage and the ceiling trips
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0 // purging never dissolves the pressure
	defer func() { metadataBudgetBytes = oldBudget }()
	smc := newFakeSMC(t)

	cfg := testConfig(t, true)
	ackedArchive(t, cfg, smc, 100)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	// VACUUM first (fixture prep only — the LIVE path never runs it): boot
	// WAL-fold slack otherwise collapses on the first in-test fold and
	// dissolves the pinned pressure. The MIDDLE deletion then builds the
	// freelist from pages that can never reach the file tail (post-VACUUM
	// physical order is table order), so no commit can truncate them away.
	if _, err := raw.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`DELETE FROM observations WHERE uuid IN
		   (SELECT uuid FROM observations ORDER BY uuid ASC LIMIT 100 OFFSET 100)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage + 40*1024 // ceiling = cap−64K sits below usage; usage in the reserve band
	s := newRunning(t, cfg2)

	// A sustained run of drops: the loss refreshes must never carry usage
	// past the hard cap. The SOLE P1 invariant is that ceiling — capture may
	// legitimately oscillate (an in-band WAL fold genuinely frees a slot's
	// room, so a later slot writes, then re-drops); what it must NEVER do is
	// exceed the cap on its own loss bookkeeping.
	for j := 0; j < 30; j++ {
		s.CaptureSlot(richSlot(slotAt((900 + j) * 15)))
		drain(t, s)
		if u, _ := s.physicalUsage(); u > cfg2.CapBytes {
			t.Fatalf("P1: usage %d exceeded the cap %d after %d sustained drops — the loss refreshes consumed the reserve", u, cfg2.CapBytes, j+1)
		}
	}
	dropped := s.Status().DroppedSlots
	if dropped == 0 {
		t.Fatal("fixture: no slots dropped — the ceiling never engaged")
	}
	// The deferred accumulator survives to Stop, which persists it with
	// priority: every dropped slot is counted in sealed cap loss.
	s.Stop()
	db := openRaw(t, cfg.Path)
	var persisted int64
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(slots), 0) FROM loss_intervals WHERE reason = 'cap' AND remote_status = 'never_offered' AND sealed = 1`).
		Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != dropped {
		t.Fatalf("P1: %d dropped slots but %d persisted in cap loss — the deferred accumulator must survive to Stop", dropped, persisted)
	}
}

// Review P2 on ab9868cc: a mixed-class unsynced chunk inserts up to THREE
// receipts; the budget gate must reserve for all of them, or a chunk
// passing the one-receipt gate busts the bound with the other two.
func TestRT6_BudgetReservesEveryClassReceipt(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 256 * 1024
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 300 // fits ONE receipt's reserve, not three receipts
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	for i := 0; i < 12; i++ {
		s.CaptureSlot(obsSlot(slotAt(i*15), 14.074, true))
	}
	drain(t, s)
	db := openRaw(t, cfg.Path)
	// All three classes present, interleaved by time.
	for i := 1; i < 12; i += 3 {
		if _, err := db.Exec(`UPDATE observations SET offered_at = '2026-08-10T00:00:00.000000001Z'
			 WHERE slot_start_utc = ?`, slotAt(i*15).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 2; i < 12; i += 3 {
		if _, err := db.Exec(`UPDATE observations SET quarantine_reason = 'digest_conflict'
			 WHERE slot_start_utc = ?`, slotAt(i*15).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	obsBefore := countRows(t, db, `SELECT COUNT(*) FROM observations`)

	_ = s.purgeChunk()
	got, _ := s.metadataBytes(context.Background())
	s.Stop()

	if got > metadataBudgetBytes {
		t.Fatalf("P2: metadata %d B exceeds the %d B budget — the gate reserved one receipt and the chunk wrote three", got, metadataBudgetBytes)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != obsBefore {
		t.Fatalf("P2: %d→%d observations purged under a budget with no room for the chunk's receipts", obsBefore, n)
	}
}

// Package-review P1 (2026-08-10): Status must never hold s.mu across its
// database aggregates — CaptureSlot needs that mutex SYNCHRONOUSLY on the
// FT8 decode goroutine, so a status poll over a cap-sized archive would
// stall decoding. The seam (statusQueryDelay) sleeps WITH the queries:
// wherever they run, the delay runs — if that is inside the lock, capture
// blocks and this test sees it.
func TestStatus_NeverBlocksCapture(t *testing.T) {
	oldDelay := statusQueryDelay
	statusQueryDelay = 300 * time.Millisecond
	defer func() { statusQueryDelay = oldDelay }()

	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	done := make(chan struct{})
	go func() {
		_ = s.Status()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // let Status reach its query phase

	start := time.Now()
	s.CaptureSlot(obsSlot(slotAt(0), 14.074, true))
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("P1: CaptureSlot took %v while Status ran its queries — the status poll stalls the decode path", elapsed)
	}
	<-done
	drain(t, s)
}

// buildV3SyncedArchive creates a v3-shaped archive whose observation and
// coverage rows are all synced=1 (as a §5-era client left them): the
// codex-P1 fixture — under a NULL backfill these rows belong to NEITHER
// purge class and the archive wedges at the watermark.
func buildV3SyncedArchive(t *testing.T, path string, rows int) int64 {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v2SchemaForTest); err != nil {
		t.Fatal(err)
	}
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
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		slot := slotAt(i * 15).UTC().Format("2006-01-02T15:04:05Z")
		if _, err := tx.Exec(
			`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked, freq_hz,
				dt_sec, snr, payload, parse_status, text, prov_algorithm,
				metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
				metric_blocks, metric_hard_errors, metric_dmin, decoder_build, profile_uuid, synced, offered_at)
			 VALUES (?, ?, 14.074, 1, 1200, 0.1, -5, zeroblob(2048), 'parsed', NULL, 'bp',
				1, 1, 1, 1, 1, 0, 1, 'v3', NULL, 1, '2026-08-10T00:00:00Z')`,
			pad("v3obs", i), slot); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(
			`INSERT INTO coverage (uuid, slot_start_utc, outcome, dial_mhz, dial_tracked, decode_count, synced, offered_at)
			 VALUES (?, ?, 'decoded', 14.074, 1, 1, 1, '2026-08-10T00:00:00Z')`,
			pad("v3cov", i), slot); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	return statUsage(t, path)
}

// codex-P1 fix — an upgraded v3 archive full of synced rows must still
// purge at the watermark: legacy_synced is cloud-present.
func TestV4_LegacySyncedRowsStayPurgeable(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	buildV3SyncedArchive(t, cfg.Path, 40)
	// Migrate 3→4 under the roomy default cap first — the backfill is
	// itself gated near the cap (TestV4_MigrationRefusedNearCap pins that);
	// the purge phase then boots against the pinned watermark.
	sm := newRunning(t, cfg)
	sm.Stop()
	usage := statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
	s := newRunning(t, cfg2)         // v4 archive at cap pressure
	s.CaptureSlot(richSlot(slotAt(900 * 15)))
	drain(t, s)

	db := openRaw(t, cfg.Path)
	slotUTC := slotAt(900 * 15).UTC().Format("2006-01-02T15:04:05Z")
	if n := countRows(t, db, `SELECT COUNT(*) FROM coverage WHERE slot_start_utc = ?`, slotUTC); n != 1 {
		t.Fatal("P1: the cap-pressure slot dropped on an upgraded all-synced archive — legacy rows stranded outside every purge class")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM retention_records WHERE acknowledged = 1`); n == 0 {
		t.Fatal("P1: no acked receipt — the legacy purge must be receipted like any other")
	}
	s.Stop()
}

// The 3→4 backfill dirties ~every synced page, so — like 1→2 — a near-cap
// v3 archive refuses migration on the pre-write projection rather than
// blowing the cap.
func TestV4_MigrationRefusedNearCap(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
	defer func() { headroomBytes = oldHeadroom }()

	cfg := testConfig(t, true)
	usage := buildV3SyncedArchive(t, cfg.Path, 600)
	cfg.CapBytes = usage + usage/2

	s := New(cfg, logging.Noop())
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("P1: a near-cap v3→v4 backfill must refuse like 1→2 — its WAL peak is ~the whole archive")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Fatalf("the refusal must name the cap; got: %v", err)
	}
	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "3" {
		t.Fatalf("refused migration must leave the archive at v3 (got %q, err %v)", ver, err)
	}
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
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
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
	for i := 0; i < 40; i++ {
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

	cfg2 := cfg                      // sync off: classes stand still while purging runs
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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

// codex-P2 fix (2026-08-10) — the budget check RESERVES the incoming
// receipt's estimate: a budget with room for existing metadata but NOT for
// the receipt the purge would insert refuses the purge, instead of
// committing a receipt that busts the promised bound by up to one row.
func TestRT6_BudgetReservesTheIncomingReceipt(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 200 // above current usage (0), below one receipt's reserve
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 40; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	db := openRaw(t, cfg.Path)
	obsBefore := countRows(t, db, `SELECT COUNT(*) FROM observations`)

	cfg2 := cfg
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
	s2 := newRunning(t, cfg2)
	s2.CaptureSlot(richSlot(slotAt(700 * 15)))
	drain(t, s2)

	if n := countRows(t, db, `SELECT COUNT(*) FROM observations`); n != obsBefore {
		t.Fatalf("P2: observations %d → %d under a budget with no room for the receipt — that purge's receipt busts the bound", obsBefore, n)
	}
	st := s2.Status()
	if st.Retention == nil || st.Retention.Pressure != pressureMetadata {
		t.Fatalf("P2: Retention = %+v, want metadata pressure — the refusal must be visible", st.Retention)
	}
	s2.Stop()
}

// RT6/RT9 — when the metadata budget cannot admit a receipt, NO invisible
// purge happens: capture enters metadata-pressure drop-new and status says
// exactly why — distinguishable from cap-pressure (nothing purgeable) and
// from healthy purging (state stays capturing).
func TestRT6_MetadataPressureRefusesInvisiblePurge(t *testing.T) {
	oldHeadroom := headroomBytes
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
	defer func() { headroomBytes = oldHeadroom }()
	oldBudget := metadataBudgetBytes
	metadataBudgetBytes = 0 // no receipt can ever fit
	defer func() { metadataBudgetBytes = oldBudget }()

	cfg := testConfig(t, true)
	s1 := newRunning(t, cfg)
	for i := 0; i < 40; i++ {
		s1.CaptureSlot(richSlot(slotAt(i * 15)))
	}
	drain(t, s1)
	s1.Stop()
	usage := statUsage(t, cfg.Path)

	db := openRaw(t, cfg.Path)
	obsBefore := countRows(t, db, `SELECT COUNT(*) FROM observations`)

	cfg2 := cfg
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
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
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
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

	cfg2 := cfg                      // sync OFF in boot 2: classes are already stamped
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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
	headroomBytes = 320 * 1024 // hosts shm + boot-2 WAL churn + the write-growth reserve
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
	cfg2.CapBytes = usage + 256*1024 // over the watermark; margin−reserve hosts the working churn
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
	headroomBytes = 320 * 1024 // drops persist the open row only OUTSIDE the write reserve — give the band room
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

	// Tight bytes: no foldable slack that a mid-test checkpoint could
	// collapse into resumed capacity.
	rawv, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawv.Exec(`VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	_ = rawv.Close()
	usage = statUsage(t, cfg.Path)

	cfg2 := cfg
	cfg2.Sync, cfg2.SyncURL, cfg2.SyncToken = true, smc.ts.URL, "test-token"
	cfg2.CapBytes = usage + 256*1024 // watermark = usage−64K: exceeded; ceiling = usage+192K: refreshes persist
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
	// The CURRENT version — the chain continues past v4; the terminal pin
	// lives with the newest migration's own test (V4→V5).
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != schemaVersion {
		t.Fatalf("V4: schema_version = %q (err %v), want %q", ver, err, schemaVersion)
	}
	// The new columns exist and the v3 row survived with them NULL/sealed.
	if _, err := db.Query(v3SchemaProbe); err != nil {
		t.Fatalf("V4: loss_intervals is missing v4 columns: %v", err)
	}
	// codex-P1 fix (2026-08-10): a v3 row already synced=1 backfills
	// sync_outcome='legacy_synced' — the legacy_unprofiled precedent: the
	// exact outcome was never recorded, but v3 clients could only ever
	// receive accepted/already_present (SMC had no tombstones then), so
	// the CLOUD-PRESENT class is a sound inference and the row stays
	// purge-eligible. NULL would strand it in neither purge class and an
	// upgraded synced archive would wedge in drop_new at the watermark.
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM loss_intervals WHERE uuid = 'v3-loss-a' AND synced = 1
		   AND sealed = 1 AND supersedes IS NULL AND sync_outcome = 'legacy_synced'`); n != 1 {
		t.Fatal("V4: the v3 synced loss row must survive SEALED with sync_outcome='legacy_synced' — NULL strands it outside every purge class")
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
