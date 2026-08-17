package evidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Retention engine (§4.1 + the 2026-08-10 retention-slice rulings; criteria
// RT1–RT10 in retention_test.go). At cap pressure the writer purges instead
// of dropping — cloud-present rows first, then (full §4.1) oldest unsynced
// observations, current capture always winning — and every purge chunk
// commits its receipt in the SAME transaction as its deletions.
//
// The physics (measured 2026-08-10, modernc.org/sqlite v1.48.1): DELETE
// does not shrink the file — deleting 1500×2 KiB rows left evidence.db
// byte-identical with freelist_count=1509, and re-inserting 1500 rows grew
// it 4 KiB — so purging targets REUSABLE PAGES, capture writes into them,
// and the file stays bounded under the cap. VACUUM never runs on the live
// path; each chunk ends with a bounded TRUNCATE checkpoint.

// Ratified retention constants (operator, 2026-08-10) — package vars so
// tests can dial them; none are operator configuration.
var (
	// purgeChunkMaxRows is the MAXIMUM rows one purge transaction may
	// delete; queued slots take priority between chunks (RT8) because at
	// most ONE chunk runs per processed slot.
	purgeChunkMaxRows = 500
	// purgeFreelistTarget is the reusable-page goal of a purge episode,
	// clamped to half the watermark — NOT a promised file-size reduction.
	purgeFreelistTarget = int64(64 << 20)
	// metadataBudgetBytes bounds loss + retention metadata LOGICALLY: when
	// compaction cannot fit under it, no invisible purge occurs — capture
	// enters metadata-pressure drop-new (RT6/RT1).
	metadataBudgetBytes = int64(4 << 20)
	// compactionTrigger is the per-kind row count that invites compaction —
	// a trigger, not the hard bound (the budget is).
	compactionTrigger = 256
	// compactionMaxPreds bounds a summary's DIRECT predecessors.
	compactionMaxPreds = 64
	// receiptReserveBytes is the metadata room a purge chunk must have
	// BEFORE it runs, covering EVERY receipt it may insert (codex-P2 fix
	// 2026-08-10, widened by the ab9868cc review): an unsynced chunk
	// splits into up to three per-class loss receipts, so the reserve
	// covers three 128-byte logical rows, doubled for margin — checking
	// only existing usage lets the receipts themselves bust the budget.
	receiptReserveBytes = int64(3 * 256)
	// writeWalReserveBytes is the ceiling's margin below CapBytes: a write
	// authorized inside this band would append its WAL frames past the cap
	// (ab9868cc review P1). One slot transaction writes tens of KiB of WAL
	// at most (see headroomBytes); 64 KiB is generous — and trivial against
	// the production 16 MiB watermark band it is carved from.
	writeWalReserveBytes = int64(64 << 10)
	// slotWriteReserveBytes is the reusable room required to write through
	// at cap pressure: ONE page. A page only reaches the freelist when
	// entirely emptied, so even a large purge may free few pages on a small
	// archive — and one free page is real forward progress, because the db
	// file allocates from the freelist before extending while the write's
	// WAL churn rides the same reserved headroom as every other write.
	slotWriteReserveBytes = int64(4096)
)

// Pressure classes surfaced by Status.Retention.Pressure while dropping.
const (
	pressureCap      = "cap"
	pressureMetadata = "metadata"
)

// The quarantined-drop class (operator ruling 2026-08-10): permanent_reject
// is KNOWN remotely absent — never offered_unacknowledged.
const (
	remoteOfferedUnacked = "offered_unacknowledged"
	remoteRejected       = "rejected"
)

// RetentionStatus is the retention half of the honesty surface (RT9).
type RetentionStatus struct {
	PurgedObservations *int64 `json:"purged_observations"`
	PurgedCoverage     *int64 `json:"purged_coverage"`
	Records            *int64 `json:"records"`
	MetadataBytes      *int64 `json:"metadata_bytes"` // null when the archive could not be measured (L2)
	Pressure           string `json:"pressure,omitempty"`
}

// tryFreeSpace reports whether the current slot can be written at cap
// pressure. It runs at most ONE bounded purge chunk (RT8: queued slots take
// priority between chunks), then answers whether reusable pages exist for
// the write. Called without s.mu; all work is on the db handle.
func (s *Service) tryFreeSpace() (bool, error) {
	target := purgeFreelistTarget
	if half := (s.cfg.CapBytes - headroomBytes) / 2; target > half {
		target = half
	}
	fb, err := s.freelistBytes()
	if err != nil {
		return false, err
	}
	if fb < target {
		if err := s.purgeChunk(); err != nil {
			return false, err
		}
	}
	// Reusable pages authorize the write ONLY below the hard cap
	// (package-review P1, 2026-08-10): freelist reuse keeps the db file
	// flat, but the WAL still grows per write — and when checkpointing is
	// blocked, total physical usage climbs while the freelist keeps saying
	// yes. The cap is a ceiling; at or past it, capture drops, however
	// many pages are free inside the file. When the freelist itself was
	// the blocker, purgeChunk already recorded the specific pressure.
	fb, err = s.freelistBytes()
	if err != nil {
		return false, err
	}
	ok := fb >= slotWriteReserveBytes
	if ok {
		usage, err := s.physicalUsage()
		if err != nil {
			return false, err
		}
		if usage >= s.cfg.CapBytes-writeWalReserveBytes {
			ok = false
			s.setPressure(pressureCap)
		}
	}
	if ok {
		s.setPressure("")
	}
	return ok, nil
}

func (s *Service) setPressure(p string) {
	s.mu.Lock()
	s.pressure = p
	s.mu.Unlock()
}

func (s *Service) freelistBytes() (int64, error) {
	if err := measureFail("pragma_freelist_count"); err != nil {
		return 0, err
	}
	var pages, pageSize int64
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&pages); err != nil {
		return 0, measured("pragma_freelist_count", err)
	}
	if err := measureFail("pragma_page_size"); err != nil {
		return 0, err
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, measured("pragma_page_size", err)
	}
	return pages * pageSize, nil
}

// metadataBytes is the LOGICAL loss+retention metadata estimate the 4 MiB
// budget bounds: a fixed per-row overhead plus the supersedes text — an
// estimate by design; the budget is a reserve, not an accounting claim.
// metadataBytes takes a ctx so the Status snapshot's aggregate deadline (LC-4) reaches its
// two reads; the writer/retention measurement callers pass context.Background() — that path
// is on LC-3's guaranteed-lossless drain and is deliberately non-cancellable (its shutdown
// bound is LC-2's budget + systemd, not a per-op context).
func (s *Service) metadataBytes(ctx context.Context) (int64, error) {
	var loss, ret int64
	if err := measureFail("metadata_loss"); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(128 + COALESCE(LENGTH(supersedes), 0)), 0) FROM loss_intervals`).Scan(&loss); err != nil {
		return 0, measured("metadata_loss", err)
	}
	if err := measureFail("metadata_retention"); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(128 + COALESCE(LENGTH(supersedes), 0)), 0) FROM retention_records`).Scan(&ret); err != nil {
		return 0, measured("metadata_retention", err)
	}
	return loss + ret, nil
}

// purgeChunk runs one bounded purge: cloud-present rows first, then (full
// §4.1) the oldest unsynced class. A chunk whose receipt cannot fit the
// metadata budget does NOT run — no invisible purge (RT6).
func (s *Service) purgeChunk() error {
	// The check RESERVES the incoming receipt's estimate (codex-P2 fix):
	// the purge may only run if its own receipt still fits the budget.
	// context.Background(): this runs on the writer/retention drain path, kept
	// non-cancellable by the LC-4 decline (see metadataBytes).
	mb, err := s.metadataBytes(context.Background())
	if err != nil {
		return err
	}
	if mb+receiptReserveBytes > metadataBudgetBytes {
		if err := s.compactOnce(); err != nil {
			return err
		}
		if mb, err = s.metadataBytes(context.Background()); err != nil {
			return err
		}
		if mb+receiptReserveBytes > metadataBudgetBytes {
			s.setPressure(pressureMetadata)
			return nil // genuine metadata pressure — not a measurement failure
		}
	}
	// A purge EXECUTION failure must not be misread as capacity: it means the
	// archive could not be operated, so it propagates (fail-closed) rather than
	// leaving the slot to be blamed on cap. The former per-chunk Warn is dropped —
	// the tracker's bounded degraded transition replaces "log every failed retry".
	purged, err := s.purgeAckedChunk()
	if err != nil {
		return measured("purge_acked", err)
	}
	if !purged {
		purged, err = s.purgeUnsyncedChunk()
		if err != nil {
			return measured("purge_unsynced", err)
		}
	}
	if !purged {
		s.setPressure(pressureCap)
		return nil // genuine cap — nothing purgeable
	}
	s.setPressure("")
	// Bounded checkpoint folds the chunk's WAL so freed pages are reusable and
	// physical usage reflects reality — never VACUUM on the live path. A failure
	// means usage measurement is unreliable, so it propagates (fail-closed);
	// busy != 0 (a reader blocked the fold) is NOT a failure, only a Debug trace.
	if err := measureFail("checkpoint_purge"); err != nil {
		return err
	}
	var busy, logFrames, checkpointed int64
	if err := s.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).
		Scan(&busy, &logFrames, &checkpointed); err != nil {
		return measured("checkpoint_purge", err)
	}
	if busy != 0 {
		s.log.DebugWith().Int64("wal_frames", logFrames).
			Msg("evidence: post-purge checkpoint blocked by a reader; WAL retained until it finishes")
	}
	return s.maybeCompact()
}

type purgeSet struct {
	uuids      []string
	slots      map[string]bool
	minSlot    string
	maxSlot    string
	sameDial   bool
	dial       float64
	haveDial   bool
	classCount int
}

func (p *purgeSet) add(uuid, slot string, dial float64) {
	p.uuids = append(p.uuids, uuid)
	p.slots[slot] = true
	if p.minSlot == "" || slot < p.minSlot {
		p.minSlot = slot
	}
	if slot > p.maxSlot {
		p.maxSlot = slot
	}
	if !p.haveDial {
		p.dial, p.haveDial = dial, true
	} else if dial != p.dial {
		p.sameDial = false
	}
}

func scanPurgeRows(rs *sql.Rows) (*purgeSet, error) {
	p := &purgeSet{slots: map[string]bool{}, sameDial: true}
	for rs.Next() {
		var uuid, slot string
		var dial float64
		if err := rs.Scan(&uuid, &slot, &dial); err != nil {
			_ = rs.Close()
			return nil, err
		}
		p.add(uuid, slot, dial)
	}
	if err := rs.Err(); err != nil {
		_ = rs.Close()
		return nil, fmt.Errorf("iterate purge selection: %w", err)
	}
	if err := rs.Close(); err != nil {
		return nil, fmt.Errorf("close purge selection: %w", err)
	}
	return p, nil
}

func (p *purgeSet) dialOrZero() float64 {
	if p.sameDial && p.haveDial {
		return p.dial
	}
	return 0 // spans more than one dial context; unattributed is honest
}

// legacy_synced joins the class (codex-P1 fix): a v3-era synced row is
// cloud-present by inference — see legacySyncedOutcome in schema.go.
const cloudPresent = `sync_outcome IN ('accepted', 'already_present', 'legacy_synced')`

// purgeAckedChunk deletes the oldest cloud-present observations (then
// coverage whose slot holds no remaining observations), receipting them as
// ONE retention record in the same transaction (RT2). Coverage eligibility
// is evaluated AFTER the observation deletes, so a slot fully purged in
// this chunk releases its coverage row too — never the reverse (RT3).
func (s *Service) purgeAckedChunk() (changed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer txutil.Rollback(tx, &err)

	rs, err := tx.Query(
		`SELECT uuid, slot_start_utc, dial_mhz FROM observations
		  WHERE `+cloudPresent+` ORDER BY uuid ASC LIMIT ?`, purgeChunkMaxRows)
	if err != nil {
		return false, err
	}
	obs, err := scanPurgeRows(rs)
	if err != nil {
		return false, err
	}
	if len(obs.uuids) > 0 {
		if err := deleteByUUID(tx, "observations", obs.uuids); err != nil {
			return false, err
		}
	}

	var cov *purgeSet
	remaining := purgeChunkMaxRows - len(obs.uuids)
	if remaining > 0 {
		rs, err := tx.Query(
			`SELECT uuid, slot_start_utc, dial_mhz FROM coverage c
			  WHERE `+cloudPresent+` AND NOT EXISTS
			        (SELECT 1 FROM observations o WHERE o.slot_start_utc = c.slot_start_utc)
			  ORDER BY uuid ASC LIMIT ?`, remaining)
		if err != nil {
			return false, err
		}
		if cov, err = scanPurgeRows(rs); err != nil {
			return false, err
		}
		if len(cov.uuids) > 0 {
			if err := deleteByUUID(tx, "coverage", cov.uuids); err != nil {
				return false, err
			}
		}
	} else {
		cov = &purgeSet{}
	}

	if len(obs.uuids) == 0 && len(cov.uuids) == 0 {
		return false, nil
	}

	start, end := obs.minSlot, obs.maxSlot
	if start == "" || (cov.minSlot != "" && cov.minSlot < start) {
		if cov.minSlot != "" {
			start = cov.minSlot
		}
	}
	if cov.maxSlot > end {
		end = cov.maxSlot
	}
	// The range covers the LAST slot (package-review P1-4a): end is that
	// slot's start plus the slot duration, or a one-slot purge would claim
	// a zero-length interval.
	if t, err := time.Parse(time.RFC3339, end); err == nil {
		end = t.Add(slotDuration).Format(time.RFC3339)
	}
	// Dial context (P1-4c): agreement across every deleted row, 0 = mixed.
	dial := obs.dialOrZero()
	if len(obs.uuids) == 0 {
		dial = cov.dialOrZero()
	} else if len(cov.uuids) > 0 && (!cov.sameDial || !obs.sameDial || cov.dial != obs.dial) {
		dial = 0
	}
	// The receipt commits WITH the deletions — RT2's whole point.
	if _, err := tx.Exec(
		`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged, dial_mhz)
		 VALUES (?, ?, ?, ?, ?, 'cap', 1, ?)`,
		utils.NewUUIDv7At(time.Now()), start, end, len(obs.uuids), len(cov.uuids), dial); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// purgeUnsyncedChunk implements the full-§4.1 ruling: once nothing
// cloud-present remains, the GLOBALLY OLDEST unsynced observations drop so
// current capture wins (package-review P2: selection is oldest-first
// across every class — picking one class first would drop newer rows of
// that class while older rows of another survive). The selected set splits
// into per-class receipts, each a sealed loss interval whose remote_status
// is honest: never_offered (offered_at NULL), offered_unacknowledged
// (offered, no ack), or rejected (quarantined — KNOWN remotely absent).
func (s *Service) purgeUnsyncedChunk() (changed bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer txutil.Rollback(tx, &err)

	rs, err := tx.Query(
		`SELECT uuid, slot_start_utc, dial_mhz,
		        offered_at IS NOT NULL, quarantine_reason IS NOT NULL
		   FROM observations WHERE synced = 0 ORDER BY uuid ASC LIMIT ?`, purgeChunkMaxRows)
	if err != nil {
		return false, err
	}
	classes := map[string]*purgeSet{}
	var all []string
	for rs.Next() {
		var uuid, slot string
		var dial float64
		var offered, quarantined bool
		if err := rs.Scan(&uuid, &slot, &dial, &offered, &quarantined); err != nil {
			_ = rs.Close()
			return false, err
		}
		status := remoteNeverOffered
		switch {
		case quarantined:
			status = remoteRejected
		case offered:
			status = remoteOfferedUnacked
		}
		set := classes[status]
		if set == nil {
			set = &purgeSet{slots: map[string]bool{}, sameDial: true}
			classes[status] = set
		}
		set.add(uuid, slot, dial)
		all = append(all, uuid)
	}
	if err := rs.Err(); err != nil {
		_ = rs.Close()
		return false, fmt.Errorf("iterate unsynced purge selection: %w", err)
	}
	if err := rs.Close(); err != nil {
		return false, fmt.Errorf("close unsynced purge selection: %w", err)
	}
	if len(all) == 0 {
		return false, nil
	}
	if err := deleteByUUID(tx, "observations", all); err != nil {
		return false, err
	}
	for _, status := range []string{remoteRejected, remoteOfferedUnacked, remoteNeverOffered} {
		set := classes[status]
		if set == nil {
			continue
		}
		end, err := time.Parse(time.RFC3339, set.maxSlot)
		if err != nil {
			end = time.Now().UTC()
		} else {
			end = end.Add(slotDuration)
		}
		if _, err := tx.Exec(
			`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed)
			 VALUES (?, ?, ?, ?, ?, 'cap', ?, ?, 1)`,
			utils.NewUUIDv7At(time.Now()), set.minSlot, end.Format(time.RFC3339),
			len(set.slots), len(set.uuids), status, set.dialOrZero()); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func deleteByUUID(tx *sql.Tx, table string, uuids []string) error {
	q := `DELETE FROM ` + table + ` WHERE uuid IN (` + placeholders(len(uuids)) + `)`
	args := make([]any, len(uuids))
	for i, u := range uuids {
		args[i] = u
	}
	_, err := tx.Exec(q, args...)
	return err
}

// maybeCompact runs compaction when either metadata kind passes the
// trigger.
func (s *Service) maybeCompact() error {
	for _, table := range []string{"loss_intervals", "retention_records"} {
		if err := measureFail("compaction_count"); err != nil {
			return err
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			return measured("compaction_count", err)
		}
		if n > compactionTrigger {
			return s.compactOnce()
		}
	}
	return nil
}

// compactRow is one metadata row during compaction run-building.
type compactRow struct {
	uuid, start, end string
	a, b             int64  // loss: slots/observations · retention: observations/coverage
	key              string // agreement key
}

// compactOnce merges ONE adjacent-agreeing run per kind into a summary —
// insert + predecessor delete in one transaction, exact totals, outer time
// range, ≤ 64 DIRECT predecessors (RT6). Eligibility: sealed plain rows
// always; a SUMMARY only after its own accepted/already_present, so its
// earlier supersession is known applied at SMC before it is replaced
// (operator ruling 3).
func (s *Service) compactOnce() error {
	if err := s.compactKind("loss_intervals",
		`SELECT uuid, start_utc, end_utc, slots, observations,
		        reason || '|' || remote_status || '|' || CAST(dial_mhz AS TEXT)
		   FROM loss_intervals
		  WHERE sealed = 1 AND (supersedes IS NULL OR `+cloudPresent+`)
		  ORDER BY start_utc ASC, uuid ASC LIMIT 512`); err != nil {
		return err
	}
	return s.compactKind("retention_records",
		`SELECT uuid, start_utc, end_utc, observations, coverage,
		        reason || '|' || CAST(acknowledged AS TEXT) || '|' || CAST(dial_mhz AS TEXT)
		   FROM retention_records
		  WHERE supersedes IS NULL OR `+cloudPresent+`
		  ORDER BY start_utc ASC, uuid ASC LIMIT 512`)
}

func (s *Service) compactKind(table, query string) (err error) {
	if err := measureFail("compaction_query"); err != nil {
		return err
	}
	rs, err := s.db.Query(query)
	if err != nil {
		return measured("compaction_query", err)
	}
	var rows []compactRow
	for rs.Next() {
		var r compactRow
		if err := rs.Scan(&r.uuid, &r.start, &r.end, &r.a, &r.b, &r.key); err != nil {
			_ = rs.Close()
			return measured("compaction_scan", err)
		}
		rows = append(rows, r)
	}
	if err := rs.Err(); err != nil {
		_ = rs.Close()
		return measured("compaction_rows", err)
	}
	_ = rs.Close()

	// The first adjacent run of ≥2 agreeing rows, capped at the direct-
	// predecessor bound. Adjacency is TEMPORAL as well as key-wise
	// (package-review P1-4b): each member must begin where the previous
	// ended, or separated intervals would merge into one
	// continuous-looking summary — the exact dishonesty these records
	// exist to prevent.
	runStart := -1
	for i := 0; i < len(rows); i++ {
		n := 1
		for i+n < len(rows) && rows[i+n].key == rows[i].key &&
			rows[i+n].start == rows[i+n-1].end && n < compactionMaxPreds {
			n++
		}
		if n >= 2 {
			runStart = i
			rows = rows[i : i+n]
			break
		}
		i += n - 1
	}
	if runStart < 0 {
		return nil // no adjacent-agreeing run to merge — not a failure
	}

	var sumA, sumB int64
	preds := make([]string, len(rows))
	minStart, maxEnd := rows[0].start, rows[0].end
	for i, r := range rows {
		preds[i] = r.uuid
		sumA += r.a
		sumB += r.b
		if r.start < minStart {
			minStart = r.start
		}
		if r.end > maxEnd {
			maxEnd = r.end
		}
	}
	supersedes, err := json.Marshal(preds)
	if err != nil {
		return measured("compaction_marshal", err)
	}

	if err := measureFail("compaction_begin"); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return measured("compaction_begin", err)
	}
	defer txutil.Rollback(tx, &err)
	summary := utils.NewUUIDv7At(time.Now())
	if table == "loss_intervals" {
		parts := splitKey(rows[0].key)
		if _, err := tx.Exec(
			`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed, supersedes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			summary, minStart, maxEnd, sumA, sumB, parts[0], parts[1], parts[2], string(supersedes)); err != nil {
			return measured("compaction_insert", err)
		}
	} else {
		parts := splitKey(rows[0].key)
		if _, err := tx.Exec(
			`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged, dial_mhz, supersedes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			summary, minStart, maxEnd, sumA, sumB, parts[0], parts[1], parts[2], string(supersedes)); err != nil {
			return measured("compaction_insert", err)
		}
	}
	if err := deleteByUUID(tx, table, preds); err != nil {
		return measured("compaction_delete", err)
	}
	if err := measureFail("compaction_commit"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return measured("compaction_commit", err)
	}
	return nil
}

func splitKey(key string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	return append(parts, key[start:])
}

// fillRetentionCounts adds the database-derived halves to a
// Status.Retention snapshot. Runs WITHOUT s.mu (package-review P1: status
// aggregates must never stall CaptureSlot).
func (s *Service) fillRetentionCounts(ctx context.Context, rs *RetentionStatus) error {
	if s.db == nil {
		return fmt.Errorf("retention counts: database is not open")
	}
	var observations, coverage, records int64
	if err := statusQueryFault("retention_total"); err != nil {
		return fmt.Errorf("count retention records: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(observations), 0), COALESCE(SUM(coverage), 0), COUNT(*) FROM retention_records`).
		Scan(&observations, &coverage, &records); err != nil {
		return fmt.Errorf("count retention records: %w", err)
	}
	// Status poll: report metadata bytes as null when unmeasurable, but do NOT
	// drive the write-driven tracker — recovery/heartbeat belong to write attempts.
	if err := statusQueryFault("retention_metadata"); err != nil {
		return fmt.Errorf("measure retention metadata: %w", err)
	}
	mb, err := s.metadataBytes(ctx)
	if err != nil {
		return fmt.Errorf("measure retention metadata: %w", err)
	}
	rs.PurgedObservations, rs.PurgedCoverage, rs.Records = &observations, &coverage, &records
	rs.MetadataBytes = &mb
	return nil
}

var _ = evidencewire.KindRetention // wire kind used by sync.go's table map
