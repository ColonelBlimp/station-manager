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
