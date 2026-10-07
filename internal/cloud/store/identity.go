package store

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/lib/pq"
)

// IdentityConflictError is an identity-wire refusal the HTTP layer reports as a
// 409 with Code (W-0021 5F.2, ADR 0089). Message names the UUIDs involved —
// client-minted, non-secret — and never a credential. The transaction that
// raised it was rolled back: nothing changed.
type IdentityConflictError struct {
	Code    string
	Message string
}

func (e *IdentityConflictError) Error() string { return e.Code + ": " + e.Message }

func identityConflict(code, format string, args ...any) error {
	return &IdentityConflictError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AdoptRequest is one legacy logbook's adoption: the tenant's legacy archive is
// stamped with ArchiveUUID and the legacy logbook named LegacyName with
// LogbookUUID. Labels and Callsign are display values (S5).
type AdoptRequest struct {
	LegacyName   string
	ArchiveUUID  string
	ArchiveLabel string
	LogbookUUID  string
	LogbookName  string
	Callsign     string
}

// Adopt stamps the tenant's legacy archive and one legacy logbook with the
// client's UUIDs in ONE transaction (ADR 0082 part 7, ADR 0088; rulings Q4,
// S2, S5). It is idempotent: the same mapping again changes nothing,
// metadata included, so a retried adoption never undoes a later rename.
// Display values apply only where this call establishes a mapping. Refusals
// (IdentityConflictError, nothing changed):
//   - legacy_archive_adopted_elsewhere — the legacy archive carries another UUID;
//   - archive_uuid_in_use — the UUID already names a managed archive, which
//     never absorbs the legacy archive;
//   - logbook_mapping_conflict — the legacy logbook carries another UUID, or
//     the UUID is already another logbook's.
//
// UUIDs must arrive in CanonicalUUID form (the HTTP layer normalizes them):
// they are compared as text with what Postgres prints.
//
// The legacy archive row is locked first, so a second adoption waits and then
// sees the first one's stamp; a concurrent first push claiming the same UUID
// loses to whichever commits first, through the unique constraints.
func (s *Store) Adopt(ctx context.Context, tenantID int64, req AdoptRequest) (changed bool, err error) {
	const op errors.Op = "store.Adopt"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer txutil.Rollback(tx, &err)

	archiveID, stamped, err := adoptArchive(ctx, tx, tenantID, req)
	if err != nil {
		return false, wrapIdentity(op, err)
	}
	logbookStamped, err := adoptLogbook(ctx, tx, tenantID, archiveID, req)
	if err != nil {
		return false, wrapIdentity(op, err)
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("commit")
	}
	return stamped || logbookStamped, nil
}

// wrapIdentity keeps an IdentityConflictError reachable by errors.As.
func wrapIdentity(op errors.Op, err error) error {
	return errors.New(op).WithErr(err)
}

func adoptArchive(ctx context.Context, tx *sql.Tx, tenantID int64, req AdoptRequest) (id int64, stamped bool, err error) {
	if id, err = ensureLegacyArchive(ctx, tx, tenantID); err != nil {
		return 0, false, err
	}
	var current sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT archive_uuid::text FROM archives WHERE id = $1 FOR UPDATE`, id).Scan(&current); err != nil {
		return 0, false, err
	}
	if current.Valid {
		if current.String != req.ArchiveUUID {
			return 0, false, identityConflict("legacy_archive_adopted_elsewhere",
				"the legacy archive is already adopted as %s, not %s", current.String, req.ArchiveUUID)
		}
		return id, false, nil
	}
	_, err = tx.ExecContext(ctx, `
UPDATE archives SET archive_uuid = $2, label = CASE WHEN $3 <> '' THEN $3 ELSE label END
WHERE id = $1`, id, req.ArchiveUUID, req.ArchiveLabel)
	if uniqueViolation(err, "archives_tenant_uuid_key") {
		return 0, false, identityConflict("archive_uuid_in_use",
			"archive %s already exists; it cannot absorb the legacy archive", req.ArchiveUUID)
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func adoptLogbook(ctx context.Context, tx *sql.Tx, tenantID, archiveID int64, req AdoptRequest) (stamped bool, err error) {
	// A logbook pushed by name concurrently is created by the same insert; the
	// read after it then finds and locks whichever row exists.
	if _, err := tx.ExecContext(ctx, `
INSERT INTO logbooks (tenant_id, archive_id, legacy_name, label) VALUES ($1, $2, $3, $3)
ON CONFLICT (archive_id, legacy_name) DO NOTHING`, tenantID, archiveID, req.LegacyName); err != nil {
		return false, err
	}
	var (
		id      int64
		current sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `
SELECT id, uuid::text FROM logbooks WHERE archive_id = $1 AND legacy_name = $2 FOR UPDATE`,
		archiveID, req.LegacyName).Scan(&id, &current); err != nil {
		return false, err
	}
	if current.Valid {
		if current.String != req.LogbookUUID {
			return false, identityConflict("logbook_mapping_conflict",
				"legacy logbook %q is already adopted as %s, not %s", req.LegacyName, current.String, req.LogbookUUID)
		}
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `
UPDATE logbooks SET uuid = $2,
    label    = CASE WHEN $3 <> '' THEN $3 ELSE label END,
    callsign = CASE WHEN $4 <> '' THEN $4 ELSE callsign END
WHERE id = $1`, id, req.LogbookUUID, req.LogbookName, req.Callsign)
	if uniqueViolation(err, "logbooks_tenant_uuid_key") {
		return false, identityConflict("logbook_mapping_conflict",
			"logbook %s is already another logbook's", req.LogbookUUID)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CanonicalUUID is a UUID's one textual form, lowercase hex — what Postgres
// prints for uuid::text. A client may send uppercase (UUIDv7 validation accepts
// it); the HTTP layer normalizes every identity UUID to this form before the
// store compares it or the response echoes it, so an identical retry is the
// same UUID (codex P2 on 0bb0754c).
func CanonicalUUID(u string) string { return strings.ToLower(u) }

// uniqueViolation reports a Postgres unique violation on the named constraint.
func uniqueViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	return stderr.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == constraint
}

// IdentityTarget is where an identity push writes: the archive and logbook by
// their UUIDs (CanonicalUUID form), with the display values the push carries.
type IdentityTarget struct {
	ArchiveUUID  string
	ArchiveLabel string
	LogbookUUID  string
	LogbookLabel string
	Callsign     string
}

// UpsertIdentity is the identity wire's write (W-0021 5F.2, ADR 0089 S1, S3,
// S5). In ONE transaction it resolves the archive by UUID — creating a managed
// archive on first use, or reaching the adopted legacy archive — and the
// logbook by UUID within it, applies the non-empty display values, and upserts
// recs into that logbook. A refusal leaves none of it behind:
//   - logbook_in_other_archive — the logbook UUID belongs to another archive;
//   - ArchiveConflictError — a QSO is stored in another archive than the
//     requested one (the upsert's own guard, at every revision);
//   - VersionConflictError — as on the name wire.
//
// Races with an adoption or another first push of the same UUID are settled by
// the unique constraints: an insert-if-absent waits on the other transaction
// and then reads what it committed.
func (s *Store) UpsertIdentity(ctx context.Context, tenantID int64, target IdentityTarget, recs []Record) (applied int, err error) {
	const op errors.Op = "store.UpsertIdentity"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer txutil.Rollback(tx, &err)
	archiveID, err := ensureIdentityArchive(ctx, tx, tenantID, target)
	if err != nil {
		return 0, wrapIdentity(op, err)
	}
	logbookID, err := ensureIdentityLogbook(ctx, tx, tenantID, archiveID, target)
	if err != nil {
		return 0, wrapIdentity(op, err)
	}
	for i := range recs {
		recs[i].LogbookID = logbookID
	}
	if applied, err = upsertTx(ctx, op, tx, recs); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("commit")
	}
	return applied, nil
}

func ensureIdentityArchive(ctx context.Context, tx *sql.Tx, tenantID int64, t IdentityTarget) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO archives (tenant_id, archive_uuid, label) VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, archive_uuid) DO NOTHING`, tenantID, t.ArchiveUUID, t.ArchiveLabel); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM archives WHERE tenant_id = $1 AND archive_uuid = $2`, tenantID, t.ArchiveUUID).Scan(&id); err != nil {
		return 0, err
	}
	// Only a change takes the row lock, so pushes that repeat the label do not
	// queue behind each other.
	if _, err := tx.ExecContext(ctx, `
UPDATE archives SET label = $2 WHERE id = $1 AND $2 <> '' AND label IS DISTINCT FROM $2`, id, t.ArchiveLabel); err != nil {
		return 0, err
	}
	return id, nil
}

func ensureIdentityLogbook(ctx context.Context, tx *sql.Tx, tenantID, archiveID int64, t IdentityTarget) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO logbooks (tenant_id, archive_id, uuid, label, callsign) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, uuid) DO NOTHING`, tenantID, archiveID, t.LogbookUUID, t.LogbookLabel, t.Callsign); err != nil {
		return 0, err
	}
	var id, inArchive int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id, archive_id FROM logbooks WHERE tenant_id = $1 AND uuid = $2`, tenantID, t.LogbookUUID).Scan(&id, &inArchive); err != nil {
		return 0, err
	}
	if inArchive != archiveID {
		return 0, identityConflict("logbook_in_other_archive",
			"logbook %s belongs to another archive than %s", t.LogbookUUID, t.ArchiveUUID)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE logbooks SET
    label    = CASE WHEN $2 <> '' THEN $2 ELSE label END,
    callsign = CASE WHEN $3 <> '' THEN $3 ELSE callsign END
WHERE id = $1 AND (($2 <> '' AND label IS DISTINCT FROM $2) OR ($3 <> '' AND callsign IS DISTINCT FROM $3))`,
		id, t.LogbookLabel, t.Callsign); err != nil {
		return 0, err
	}
	return id, nil
}

// IdentityLogbookInfo is a logbook reached by UUID with its archive's identity
// and the display values the scoped reads report.
type IdentityLogbookInfo struct {
	ID           int64
	ArchiveUUID  string
	ArchiveLabel string
	LogbookUUID  string
	LogbookLabel string
	Callsign     string
}

const identityLogbookQ = `
SELECT l.id, a.archive_uuid::text, a.label, l.uuid::text, l.label, l.callsign
FROM logbooks l JOIN archives a ON a.id = l.archive_id
WHERE a.tenant_id = $1 AND a.archive_uuid = $2 AND l.uuid = $3`

// IdentityLogbook returns the logbook logbookUUID of the tenant's archive
// archiveUUID (both CanonicalUUID form), or ErrNotFound — also when the
// logbook belongs to another archive or tenant.
func (s *Store) IdentityLogbook(ctx context.Context, tenantID int64, archiveUUID, logbookUUID string) (IdentityLogbookInfo, error) {
	const op errors.Op = "store.IdentityLogbook"
	return queryIdentityLogbook(ctx, op, s.db, tenantID, archiveUUID, logbookUUID)
}

func queryIdentityLogbook(ctx context.Context, op errors.Op, q execQuerier, tenantID int64, archiveUUID, logbookUUID string) (IdentityLogbookInfo, error) {
	var l IdentityLogbookInfo
	err := q.QueryRowContext(ctx, identityLogbookQ, tenantID, archiveUUID, logbookUUID).
		Scan(&l.ID, &l.ArchiveUUID, &l.ArchiveLabel, &l.LogbookUUID, &l.LogbookLabel, &l.Callsign)
	if stderr.Is(err, sql.ErrNoRows) {
		return l, errors.New(op).WithErr(ErrNotFound).WithMsgf("archive %s logbook %s", archiveUUID, logbookUUID)
	}
	if err != nil {
		return l, errors.New(op).WithErr(err).WithMsgf("archive %s logbook %s", archiveUUID, logbookUUID)
	}
	return l, nil
}

// IdentityExportSnapshot streams one logbook reached by UUID — its identity
// first (onHead, exactly once), then every record, tombstones included, in
// uuid order — from ONE repeatable-read, read-only transaction, as
// ExportSnapshot does for the legacy archive. ErrNotFound before onHead when
// the logbook is not in that archive. Callback errors return unwrapped.
func (s *Store) IdentityExportSnapshot(ctx context.Context, tenantID int64, archiveUUID, logbookUUID string,
	onHead func(IdentityLogbookInfo) error, onRecord func(Record) error) (err error) {
	const op errors.Op = "store.IdentityExportSnapshot"
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer txutil.Rollback(tx, &err)
	head, err := queryIdentityLogbook(ctx, op, tx, tenantID, archiveUUID, logbookUUID)
	if err != nil {
		return err
	}
	if err := onHead(head); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT uuid, tenant_id, logbook_id, modified_at, revision, deleted_at, payload
FROM qsos WHERE tenant_id = $1 AND logbook_id = $2 ORDER BY uuid`, tenantID, head.ID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("logbook %s", logbookUUID)
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
