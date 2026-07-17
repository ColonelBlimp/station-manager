package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/aarondl/null/v8"
	"github.com/aarondl/sqlboiler/v4/boil"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// FetchQsoManifestWithContext returns the (uuid, modified_at, deleted) tuple
// for EVERY row of a logbook — tombstones included — ordered by uuid: the
// local half of the SM Cloud reconcile diff (ADR 0040), mirroring the cloud
// store's Manifest read. Rows with a NULL/blank uuid (none should exist —
// UUID backfill ran at migration 0003) are skipped rather than emitted as
// empty-keyed entries a diff would trip over.
func (s *Service) FetchQsoManifestWithContext(ctx context.Context, logbookID int64) ([]types.QsoManifestEntry, error) {
	const op errors.Op = "sqlite.Service.FetchQsoManifestWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if logbookID < 1 {
		return nil, errors.New(op).WithMsg(errMsgInvalidId)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	// Both timestamp columns come back typed (a SQL COALESCE would lose the
	// column's DATETIME affinity and scan as a raw string); the fallback
	// happens in Go below.
	rows, err := h.QueryContext(ctx,
		`SELECT uuid, modified_at, created_at, deleted_at IS NOT NULL
		 FROM qso WHERE logbook_id = ? AND uuid IS NOT NULL AND uuid != ''
		 ORDER BY uuid`, logbookID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("logbook %d", logbookID)
	}
	defer func() { _ = rows.Close() }()

	var out []types.QsoManifestEntry
	for rows.Next() {
		var (
			e  types.QsoManifestEntry
			mt sql.NullTime
			ct time.Time
		)
		if err := rows.Scan(&e.UUID, &mt, &ct, &e.Deleted); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan")
		}
		// modified_at is NULL until the update trigger first fires (no INSERT
		// default): a never-edited row's modified time IS its creation time.
		// MUST match adapters.QsoModelToType's fallback — the forwarder pushes
		// qso.ModifiedAt while this feeds the local reconcile hash;
		// disagreement = phantom drift on every fresh row. Values are UTC by
		// construction (migration 0004). Truncated to SECONDS like the
		// adapter: the trigger's datetime('now') defines the local precision,
		// and created_at's sub-second digits would otherwise make a
		// same-second follow-up push read as stale (see QsoModelToType).
		if mt.Valid {
			e.ModifiedAt = mt.Time.UTC().Truncate(time.Second)
		} else {
			e.ModifiedAt = ct.UTC().Truncate(time.Second)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("rows")
	}
	return out, nil
}

// InsertRestoredQsoWithContext inserts a QSO PRESERVING its storage metadata —
// the SM Cloud restore writer (ADR 0040 S5). Unlike the normal insert path
// (whose adapter deliberately leaves modified_at/deleted_at unmapped so the
// bump trigger owns them), this sets both columns explicitly from
// qso.ModifiedAt / qso.DeletedAt: a restored row must carry its ORIGINAL
// recency (or reconcile would flag every restored row as drifted) and its
// tombstone state. INSERT does not fire the update trigger, so the values
// land verbatim. Requires uuid + non-zero ModifiedAt (caller-validated
// invariants of the export wire).
func (s *Service) InsertRestoredQsoWithContext(ctx context.Context, qso types.Qso) (int64, error) {
	const op errors.Op = "sqlite.Service.InsertRestoredQsoWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	if qso.UUID == "" {
		return 0, errors.New(op).WithMsg("restored qso has no uuid")
	}
	if qso.ModifiedAt.IsZero() {
		return 0, errors.New(op).WithMsgf("restored qso %s has no modified_at", qso.UUID)
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return 0, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()

	model, err := adapters.QsoTypeToModel(qso)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	model.ModifiedAt = null.TimeFrom(qso.ModifiedAt.UTC())
	if !qso.DeletedAt.IsZero() {
		model.DeletedAt = null.TimeFrom(qso.DeletedAt.UTC())
	}
	if err = model.Insert(ctx, h, boil.Infer()); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("uuid %s", qso.UUID)
	}
	return model.ID, nil
}
