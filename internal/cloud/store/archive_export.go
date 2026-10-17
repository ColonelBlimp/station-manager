package store

import (
	"context"
	"database/sql"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// ArchiveHead is the whole-archive export's head (W-0021 Q7, ADR 0071
// refinement): the archive, its legacy marker, and every logbook with its
// row and tombstone counts, read in the same snapshot as the rows.
type ArchiveHead struct {
	ID           int64
	ArchiveUUID  string
	ArchiveLabel string
	Legacy       bool
	Logbooks     []ArchiveLogbook
}

// ArchiveLogbook is one logbook of ArchiveHead. UUID is nil only for a
// legacy logbook that was never adopted. LegacyName is the stored name the
// name-only wire resolves, exactly as stored (nil when NULL). Count includes
// tombstones.
type ArchiveLogbook struct {
	ID         int64
	UUID       *string
	LegacyName *string
	Label      string
	Callsign   string
	Count      int
	Tombstones int
}

// archiveLogbooksQ is every logbook of one archive with its counts, in id
// order; an empty logbook counts zero.
const archiveLogbooksQ = `
SELECT l.id, l.uuid, l.legacy_name, l.label, l.callsign, count(q.uuid), count(q.deleted_at)
FROM logbooks l
         LEFT JOIN qsos q ON q.tenant_id = l.tenant_id AND q.logbook_id = l.id
WHERE l.tenant_id = $1 AND l.archive_id = $2
GROUP BY l.id
ORDER BY l.id`

func queryArchiveLogbooks(ctx context.Context, op errors.Op, tx *sql.Tx, tenantID, archiveID int64) ([]ArchiveLogbook, error) {
	rows, err := tx.QueryContext(ctx, archiveLogbooksQ, tenantID, archiveID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("logbooks")
	}
	defer func() { _ = rows.Close() }()
	out := []ArchiveLogbook{}
	for rows.Next() {
		var (
			lb         ArchiveLogbook
			uuid, name sql.NullString
		)
		if err := rows.Scan(&lb.ID, &uuid, &name, &lb.Label, &lb.Callsign, &lb.Count, &lb.Tombstones); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan logbook")
		}
		if uuid.Valid {
			lb.UUID = &uuid.String
		}
		if name.Valid {
			lb.LegacyName = &name.String
		}
		out = append(out, lb)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("logbooks")
	}
	return out, nil
}

// readOnlySnapshot opens the one repeatable-read, read-only transaction an
// archive read runs in.
func (s *Store) readOnlySnapshot(ctx context.Context, op errors.Op) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("begin")
	}
	return tx, nil
}

// IdentityArchives lists the tenant's archives that have a UUID (managed
// archives and an adopted legacy archive), in UUID order, each with its
// logbooks and counts, from one snapshot.
func (s *Store) IdentityArchives(ctx context.Context, tenantID int64) (out []ArchiveHead, err error) {
	const op errors.Op = "store.IdentityArchives"
	tx, err := s.readOnlySnapshot(ctx, op)
	if err != nil {
		return nil, err
	}
	defer txutil.Rollback(tx, &err)
	rows, err := tx.QueryContext(ctx, `
SELECT id, archive_uuid, label, legacy FROM archives
WHERE tenant_id = $1 AND archive_uuid IS NOT NULL
ORDER BY archive_uuid`, tenantID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("archives")
	}
	out = []ArchiveHead{}
	for rows.Next() {
		var a ArchiveHead
		if err := rows.Scan(&a.ID, &a.ArchiveUUID, &a.ArchiveLabel, &a.Legacy); err != nil {
			_ = rows.Close()
			return nil, errors.New(op).WithErr(err).WithMsg("scan archive")
		}
		out = append(out, a)
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("archives")
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("archives")
	}
	for i := range out {
		if out[i].Logbooks, err = queryArchiveLogbooks(ctx, op, tx, tenantID, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ArchiveExportSnapshot streams a whole archive reached by UUID from ONE
// repeatable-read, read-only transaction: the archive and its logbooks with
// their counts (onHead, exactly once), then every record of its logbooks,
// tombstones included, in uuid order. The archive row is read first, so the
// snapshot is fixed before the logbooks, counts and rows are. ErrNotFound
// before onHead when the tenant has no archive with that UUID. Callback
// errors return unwrapped.
func (s *Store) ArchiveExportSnapshot(ctx context.Context, tenantID int64, archiveUUID string,
	onHead func(ArchiveHead) error, onRecord func(Record) error) (err error) {
	const op errors.Op = "store.ArchiveExportSnapshot"
	tx, err := s.readOnlySnapshot(ctx, op)
	if err != nil {
		return err
	}
	defer txutil.Rollback(tx, &err)
	head := ArchiveHead{ArchiveUUID: archiveUUID}
	err = tx.QueryRowContext(ctx,
		`SELECT id, label, legacy FROM archives WHERE tenant_id = $1 AND archive_uuid = $2`, tenantID, archiveUUID).
		Scan(&head.ID, &head.ArchiveLabel, &head.Legacy)
	if stderr.Is(err, sql.ErrNoRows) {
		return errors.New(op).WithErr(ErrNotFound).WithMsgf("archive %s", archiveUUID)
	}
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("archive %s", archiveUUID)
	}
	if head.Logbooks, err = queryArchiveLogbooks(ctx, op, tx, tenantID, head.ID); err != nil {
		return err
	}
	if err := onHead(head); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT q.uuid, q.tenant_id, q.logbook_id, q.modified_at, q.revision, q.deleted_at, q.payload
FROM qsos q
         JOIN logbooks l ON l.id = q.logbook_id
WHERE q.tenant_id = $1 AND l.archive_id = $2
ORDER BY q.uuid`, tenantID, head.ID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("archive %s rows", archiveUUID)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			r       Record
			payload []byte
		)
		if err := rows.Scan(&r.UUID, &r.TenantID, &r.LogbookID, &r.ModifiedAt, &r.Revision, &r.DeletedAt, &payload); err != nil {
			return errors.New(op).WithErr(err).WithMsg("scan")
		}
		r.Payload = payload
		if err := onRecord(r); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("rows")
	}
	return nil
}
