package evidence

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
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
	// BEFORE it runs, covering the receipt it will insert (codex-P2 fix
	// 2026-08-10): checking only existing usage lets the receipt itself
	// bust the budget by up to one row. 256 = the 128-byte logical row
	// overhead doubled — receipts carry no supersedes text.
	receiptReserveBytes = int64(256)
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
	PurgedObservations int64  `json:"purged_observations"`
	PurgedCoverage     int64  `json:"purged_coverage"`
	Records            int64  `json:"records"`
	MetadataBytes      int64  `json:"metadata_bytes"`
	Pressure           string `json:"pressure,omitempty"`
}

// tryFreeSpace reports whether the current slot can be written at cap
// pressure. It runs at most ONE bounded purge chunk (RT8: queued slots take
// priority between chunks), then answers whether reusable pages exist for
// the write. Called without s.mu; all work is on the db handle.
func (s *Service) tryFreeSpace() bool {
	target := purgeFreelistTarget
	if half := (s.cfg.CapBytes - headroomBytes) / 2; target > half {
		target = half
	}
	if s.freelistBytes() < target {
		s.purgeChunk()
	}
	ok := s.freelistBytes() >= slotWriteReserveBytes
	if ok {
		s.setPressure("")
	}
	return ok
}

func (s *Service) setPressure(p string) {
	s.mu.Lock()
	s.pressure = p
	s.mu.Unlock()
}

func (s *Service) freelistBytes() int64 {
	var pages, pageSize int64
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&pages); err != nil {
		return 0
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0
	}
	return pages * pageSize
}

// metadataBytes is the LOGICAL loss+retention metadata estimate the 4 MiB
// budget bounds: a fixed per-row overhead plus the supersedes text — an
// estimate by design; the budget is a reserve, not an accounting claim.
func (s *Service) metadataBytes() int64 {
	var loss, ret int64
	_ = s.db.QueryRow(
		`SELECT COALESCE(SUM(128 + COALESCE(LENGTH(supersedes), 0)), 0) FROM loss_intervals`).Scan(&loss)
	_ = s.db.QueryRow(
		`SELECT COALESCE(SUM(128 + COALESCE(LENGTH(supersedes), 0)), 0) FROM retention_records`).Scan(&ret)
	return loss + ret
}

// purgeChunk runs one bounded purge: cloud-present rows first, then (full
// §4.1) the oldest unsynced class. A chunk whose receipt cannot fit the
// metadata budget does NOT run — no invisible purge (RT6).
func (s *Service) purgeChunk() {
	// The check RESERVES the incoming receipt's estimate (codex-P2 fix):
	// the purge may only run if its own receipt still fits the budget.
	if s.metadataBytes()+receiptReserveBytes > metadataBudgetBytes {
		s.compactOnce()
		if s.metadataBytes()+receiptReserveBytes > metadataBudgetBytes {
			s.setPressure(pressureMetadata)
			return
		}
	}
	purged, err := s.purgeAckedChunk()
	if err != nil {
		s.log.WarnWith().Err(err).Msg("evidence: acked purge chunk failed")
		return
	}
	if !purged {
		purged, err = s.purgeUnsyncedChunk()
		if err != nil {
			s.log.WarnWith().Err(err).Msg("evidence: unsynced purge chunk failed")
			return
		}
	}
	if !purged {
		s.setPressure(pressureCap)
		return
	}
	s.setPressure("")
	// Bounded checkpoint folds the chunk's WAL so freed pages are reusable
	// and physical usage reflects reality — never VACUUM on the live path.
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	s.maybeCompact()
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

func scanPurgeRows(rs *sql.Rows) (*purgeSet, error) {
	p := &purgeSet{slots: map[string]bool{}, sameDial: true}
	for rs.Next() {
		var uuid, slot string
		var dial float64
		if err := rs.Scan(&uuid, &slot, &dial); err != nil {
			_ = rs.Close()
			return nil, err
		}
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
	return p, rs.Close()
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
func (s *Service) purgeAckedChunk() (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

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
	// The receipt commits WITH the deletions — RT2's whole point.
	if _, err := tx.Exec(
		`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged)
		 VALUES (?, ?, ?, ?, ?, 'cap', 1)`,
		utils.NewUUIDv7At(time.Now()), start, end, len(obs.uuids), len(cov.uuids)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// purgeUnsyncedChunk implements the full-§4.1 ruling: once nothing
// cloud-present remains, the OLDEST unsynced observations drop so current
// capture wins — receipted as a sealed loss interval whose remote_status is
// honest per class: never_offered (offered_at NULL), offered_unacknowledged
// (offered, no ack), or rejected (quarantined — KNOWN remotely absent). One
// class per chunk, decided by the oldest unsynced row, so each receipt's
// status is exact.
func (s *Service) purgeUnsyncedChunk() (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var offered, quarantined bool
	err = tx.QueryRow(
		`SELECT offered_at IS NOT NULL, quarantine_reason IS NOT NULL
		   FROM observations WHERE synced = 0 ORDER BY uuid ASC LIMIT 1`).
		Scan(&offered, &quarantined)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	status, filter := remoteNeverOffered, `offered_at IS NULL AND quarantine_reason IS NULL`
	switch {
	case quarantined:
		status, filter = remoteRejected, `quarantine_reason IS NOT NULL`
	case offered:
		status, filter = remoteOfferedUnacked, `offered_at IS NOT NULL AND quarantine_reason IS NULL`
	}

	rs, err := tx.Query(
		`SELECT uuid, slot_start_utc, dial_mhz FROM observations
		  WHERE synced = 0 AND `+filter+` ORDER BY uuid ASC LIMIT ?`, purgeChunkMaxRows)
	if err != nil {
		return false, err
	}
	set, err := scanPurgeRows(rs)
	if err != nil {
		return false, err
	}
	if len(set.uuids) == 0 {
		return false, nil
	}
	if err := deleteByUUID(tx, "observations", set.uuids); err != nil {
		return false, err
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
func (s *Service) maybeCompact() {
	for _, table := range []string{"loss_intervals", "retention_records"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err == nil && n > compactionTrigger {
			s.compactOnce()
			return
		}
	}
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
func (s *Service) compactOnce() {
	s.compactKind("loss_intervals",
		`SELECT uuid, start_utc, end_utc, slots, observations,
		        reason || '|' || remote_status || '|' || CAST(dial_mhz AS TEXT)
		   FROM loss_intervals
		  WHERE sealed = 1 AND (supersedes IS NULL OR `+cloudPresent+`)
		  ORDER BY start_utc ASC, uuid ASC LIMIT 512`)
	s.compactKind("retention_records",
		`SELECT uuid, start_utc, end_utc, observations, coverage,
		        reason || '|' || CAST(acknowledged AS TEXT)
		   FROM retention_records
		  WHERE supersedes IS NULL OR `+cloudPresent+`
		  ORDER BY start_utc ASC, uuid ASC LIMIT 512`)
}

func (s *Service) compactKind(table, query string) {
	rs, err := s.db.Query(query)
	if err != nil {
		return
	}
	var rows []compactRow
	for rs.Next() {
		var r compactRow
		if err := rs.Scan(&r.uuid, &r.start, &r.end, &r.a, &r.b, &r.key); err != nil {
			_ = rs.Close()
			return
		}
		rows = append(rows, r)
	}
	_ = rs.Close()

	// The first adjacent run of ≥2 agreeing rows, capped at the direct-
	// predecessor bound.
	runStart := -1
	for i := 0; i < len(rows); i++ {
		n := 1
		for i+n < len(rows) && rows[i+n].key == rows[i].key && n < compactionMaxPreds {
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
		return
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
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()
	summary := utils.NewUUIDv7At(time.Now())
	if table == "loss_intervals" {
		parts := splitKey(rows[0].key)
		if _, err := tx.Exec(
			`INSERT INTO loss_intervals (uuid, start_utc, end_utc, slots, observations, reason, remote_status, dial_mhz, sealed, supersedes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			summary, minStart, maxEnd, sumA, sumB, parts[0], parts[1], parts[2], string(supersedes)); err != nil {
			return
		}
	} else {
		parts := splitKey(rows[0].key)
		if _, err := tx.Exec(
			`INSERT INTO retention_records (uuid, start_utc, end_utc, observations, coverage, reason, acknowledged, supersedes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			summary, minStart, maxEnd, sumA, sumB, parts[0], parts[1], string(supersedes)); err != nil {
			return
		}
	}
	if err := deleteByUUID(tx, table, preds); err != nil {
		return
	}
	_ = tx.Commit()
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

// retentionStatusLocked builds Status.Retention. Requires s.mu held.
func (s *Service) retentionStatusLocked() *RetentionStatus {
	if s.db == nil {
		return nil
	}
	rs := &RetentionStatus{Pressure: s.pressure}
	_ = s.db.QueryRow(
		`SELECT COALESCE(SUM(observations), 0), COALESCE(SUM(coverage), 0), COUNT(*) FROM retention_records`).
		Scan(&rs.PurgedObservations, &rs.PurgedCoverage, &rs.Records)
	rs.MetadataBytes = s.metadataBytes()
	return rs
}

var _ = evidencewire.KindRetention // wire kind used by sync.go's table map
