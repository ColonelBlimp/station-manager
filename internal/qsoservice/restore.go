package qsoservice

import (
	"context"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// RestoreStatus classifies one Restore call's outcome.
type RestoreStatus string

const (
	// RestoreStored — the QSO was inserted (metadata preserved).
	RestoreStored RestoreStatus = "stored"
	// RestoreSkippedExisting — a row with this UUID already exists locally
	// (tombstones included); restore never overwrites — re-running is
	// idempotent, and repairing a diverged existing row is reconcile's job
	// (push direction), not restore's.
	RestoreSkippedExisting RestoreStatus = "skipped_existing"
)

// Restore inserts one QSO from an SM Cloud export, preserving its identity
// and storage metadata (ADR 0040 S5): UUID, the full additional_data-carried
// field set (the caller unmarshalled the verbatim payload), modified_at, and
// the tombstone. THE DELIBERATE NON-FEATURES: no ADIF round-trip (lossy), no
// validation gauntlet (the data was valid when logged; a since-tightened
// validator must not lose backup rows), no upload-queue rows (the cloud
// already holds these — re-pushing the restore would be circular), no
// enrichment (the stored fields ARE the enrichment).
//
// qso must carry a valid UUIDv7 and a non-zero ModifiedAt (the export wire
// guarantees both); logbookID is the LOCAL target logbook — cloud logbook ids
// mean nothing here.
func (s *Service) Restore(ctx context.Context, logbookID int64, qso types.Qso) (RestoreStatus, error) {
	const op errors.Op = "qsoservice.Restore"

	// Canonicalise BEFORE validating and keep the trimmed form: cloud backups
	// written before the server's raw-UUID gate (review 2026-07-20 #1) can
	// carry a padded UUID in the payload while the cloud's key column holds
	// the trimmed form. Trimming here makes those rows restorable — the raw
	// value would pass a trimmed validation only to die on the qso table's
	// 36-char CHECK — and keeps the stored UUID matching the cloud's key.
	qso.UUID = strings.TrimSpace(qso.UUID)
	if !utils.IsValidUUIDv7(qso.UUID) {
		return "", errors.New(op).WithMsgf("restored qso has invalid uuid %q", qso.UUID)
	}
	if qso.ModifiedAt.IsZero() {
		return "", errors.New(op).WithMsgf("restored qso %s has no modified_at", qso.UUID)
	}
	if logbookID < 1 {
		return "", errors.New(op).WithMsg("target logbook id is required")
	}

	// Idempotence: an existing row (live OR tombstone) wins — restore fills
	// gaps, never overwrites.
	if _, err := s.DB.FetchQsoByUUIDIncludingDeletedWithContext(ctx, qso.UUID); err == nil {
		s.logRestore(qso.UUID, logbookID, RestoreSkippedExisting)
		return RestoreSkippedExisting, nil
	}

	qso.ID = 0 // the cloud payload may carry the OLD local id; never reuse it
	qso.LogbookID = logbookID
	// Mirror the submit path's one schema-level defaulting (the qso table's
	// CHECK requires a well-formed time_off): an absent TIME_OFF means the
	// contact's end wasn't separately recorded — TIME_ON stands in, exactly
	// as prepareQso does.
	if strings.TrimSpace(qso.TimeOff) == "" {
		qso.TimeOff = qso.TimeOn
	}
	// Old payloads may pre-date the dedupe column riding the wire; recompute
	// with the same inputs the submit path uses so the unique index holds.
	if qso.DedupeKey == "" {
		freqKHz, err := utils.ParseFreqMHz(qso.Freq)
		if err != nil {
			return "", errors.New(op).WithErr(err).WithMsgf("qso %s: parse freq", qso.UUID)
		}
		qso.DedupeKey = ComputeDedupeKey(
			strings.ToUpper(strings.TrimSpace(qso.Call)), qso.Band, qso.Mode,
			strconv.FormatInt(freqKHz, 10), qso.QsoDate, utils.TimeToHHMM(qso.TimeOn))
	}

	if _, err := s.DB.InsertRestoredQsoWithContext(ctx, qso); err != nil {
		return "", errors.New(op).WithErr(err).WithMsgf("insert restored qso %s", qso.UUID)
	}
	s.logRestore(qso.UUID, logbookID, RestoreStored)
	return RestoreStored, nil
}

// logRestore writes the per-call restore record (logging-gaps Q1): stored and
// skipped_existing used to be identical silence, and telling a real recovery
// (every row stored) from an idempotent re-run (every row skipped) is the
// entire question during a recovery — the one time the log is least able to
// rely on anything else having survived. Debug, because a restore is a bulk
// loop; the per-run stored/skipped/failed summary is the caller's Info line
// (cmd/smd restore).
func (s *Service) logRestore(uuid string, logbookID int64, outcome RestoreStatus) {
	s.Logger.DebugWith().
		Str("uuid", uuid).
		Int64("logbook_id", logbookID).
		Str("outcome", string(outcome)).
		Msg("restore: qso")
}
