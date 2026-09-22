package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ArchiveIdentity is the QSO file's own identity (ADR 0071, migration 0012):
// the singleton archive_metadata row. The file is the authority — the
// catalogue in config.json carries the same UUID and must agree.
type ArchiveIdentity struct {
	ArchiveUUID      string
	CreatedAt        time.Time
	DefaultLogbookID int64 // 0 when the file has no default logbook (NULL)
}

// ArchiveIdentityWithContext reads the identity row; errors.ErrNotFound when
// the file has never been started by an identity-aware daemon.
func (s *Service) ArchiveIdentityWithContext(ctx context.Context) (ArchiveIdentity, error) {
	const op errors.Op = "sqlite.Service.ArchiveIdentityWithContext"
	if err := checkService(op, s); err != nil {
		return ArchiveIdentity{}, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return ArchiveIdentity{}, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	return readArchiveIdentity(ctx, op, h)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readArchiveIdentity(ctx context.Context, op errors.Op, q rowQuerier) (ArchiveIdentity, error) {
	var (
		id  ArchiveIdentity
		def sql.NullInt64
	)
	// The driver hands a DATETIME column back as time.Time (UTC, second
	// precision from datetime('now')); two reads of the same row compare equal.
	err := q.QueryRowContext(ctx, `SELECT archive_uuid, created_at, default_logbook_id FROM archive_metadata WHERE singleton = 1`).
		Scan(&id.ArchiveUUID, &id.CreatedAt, &def)
	if stderr.Is(err, sql.ErrNoRows) {
		return ArchiveIdentity{}, errors.ErrNotFound
	}
	if err != nil {
		return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("read archive identity")
	}
	if def.Valid {
		id.DefaultLogbookID = def.Int64
	}
	return id, nil
}

// EnsureArchiveIdentityWithContext fills the file's identity ONCE, in one
// transaction (ADR 0071's database-first rule, ruling (b) 2026-09-22): the
// archive UUID is minted only when the singleton row is absent, the default
// logbook pointer is taken from defaultLogbookID when that row exists (NULL
// otherwise — the daemon's default-logbook self-heal owns that repair), and
// every logbook row without a uuid receives one. A second call changes nothing
// and returns the same identity, which is what makes a crash between "identity
// written" and "catalogue written" safe: the next start reads the UUID back
// instead of minting another. Returns the identity as READ BACK from the file.
func (s *Service) EnsureArchiveIdentityWithContext(ctx context.Context, defaultLogbookID int64) (ArchiveIdentity, error) {
	const op errors.Op = "sqlite.Service.EnsureArchiveIdentityWithContext"
	if err := checkService(op, s); err != nil {
		return ArchiveIdentity{}, err
	}
	tx, cancel, err := s.BeginTxContext(ctx)
	if err != nil {
		return ArchiveIdentity{}, errors.New(op).WithErr(err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	var have int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM archive_metadata WHERE singleton = 1`).Scan(&have); err != nil {
		return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("count archive identity")
	}
	if have == 0 {
		var def sql.NullInt64
		if defaultLogbookID > 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook WHERE id = ?`, defaultLogbookID).Scan(&exists); err != nil {
				return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("check default logbook")
			}
			if exists == 1 {
				def = sql.NullInt64{Int64: defaultLogbookID, Valid: true}
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, ?, ?)`,
			utils.NewUUIDv7(), def); err != nil {
			return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("write archive identity")
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT id FROM logbook WHERE uuid IS NULL`)
	if err != nil {
		return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("find logbooks without uuid")
	}
	var missing []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return ArchiveIdentity{}, errors.New(op).WithErr(err)
		}
		missing = append(missing, id)
	}
	_ = rows.Close()
	for _, id := range missing {
		if _, err := tx.ExecContext(ctx, `UPDATE logbook SET uuid = ? WHERE id = ? AND uuid IS NULL`, utils.NewUUIDv7(), id); err != nil {
			return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsgf("backfill logbook %d uuid", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return ArchiveIdentity{}, errors.New(op).WithErr(err).WithMsg("commit archive identity")
	}
	return s.ArchiveIdentityWithContext(ctx)
}

// SetArchiveDefaultLogbookWithContext points the file's default logbook at an
// existing row. It is the file-side half of every default_logbook_id write
// (the config field is a projection of this value).
func (s *Service) SetArchiveDefaultLogbookWithContext(ctx context.Context, logbookID int64) error {
	const op errors.Op = "sqlite.Service.SetArchiveDefaultLogbookWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if logbookID < 1 {
		return errors.New(op).WithMsg(errMsgInvalidId)
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	res, err := h.ExecContext(ctx, `UPDATE archive_metadata SET default_logbook_id = ?
		WHERE singleton = 1 AND EXISTS (SELECT 1 FROM logbook WHERE id = ?)`, logbookID, logbookID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("set archive default logbook")
	}
	n, err := checkedRowsAffected(op, res, "set archive default logbook")
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New(op).WithMsgf("no archive identity row, or logbook %d does not exist", logbookID)
	}
	return nil
}
