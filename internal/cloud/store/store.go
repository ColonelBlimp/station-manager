package store

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// ErrNotFound is returned by Get when no QSO row matches the UUID. It is
// errors.Is-matchable through the operation-tagged wrapper.
var ErrNotFound = stderr.New("smcloud: qso not found")

// Store is the smcloud Postgres persistence layer (see package doc). Driver-
// agnostic: the caller owns the *sql.DB (and its Close).
type Store struct {
	db *sql.DB
}

// New wraps an open *sql.DB.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Record is one stored QSO: the identity + reconcile columns lifted out for
// indexing, plus the full types.Qso as JSON. DeletedAt non-nil marks a tombstone.
type Record struct {
	UUID       string
	TenantID   int64
	LogbookID  int64
	ModifiedAt time.Time
	DeletedAt  *time.Time
	Payload    json.RawMessage
}

// ManifestEntry is one row of a logbook's reconcile manifest — the minimal
// (uuid, modified_at, deleted) tuple a peer diffs against its local copy.
type ManifestEntry struct {
	UUID       string    `json:"uuid"`
	ModifiedAt time.Time `json:"modified_at"`
	Deleted    bool      `json:"deleted"`
}

// EnsureTenant returns the id of the tenant for callsign, creating it on first
// use. Idempotent (callsign is unique), so it is safe on every startup.
func (s *Store) EnsureTenant(ctx context.Context, callsign, name string) (int64, error) {
	const op errors.Op = "store.EnsureTenant"
	const q = `
INSERT INTO tenants (callsign, name) VALUES ($1, $2)
ON CONFLICT (callsign) DO UPDATE SET name = EXCLUDED.name
RETURNING id`
	var id int64
	if err := s.db.QueryRowContext(ctx, q, callsign, name).Scan(&id); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("callsign %q", callsign)
	}
	return id, nil
}

// EnsureLogbook returns the id of the (tenant, name) logbook, creating it on first
// use. Idempotent (unique per tenant).
func (s *Store) EnsureLogbook(ctx context.Context, tenantID int64, name string) (int64, error) {
	const op errors.Op = "store.EnsureLogbook"
	const q = `
INSERT INTO logbooks (tenant_id, name) VALUES ($1, $2)
ON CONFLICT (tenant_id, name) DO UPDATE SET name = EXCLUDED.name
RETURNING id`
	var id int64
	if err := s.db.QueryRowContext(ctx, q, tenantID, name).Scan(&id); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("tenant %d name %q", tenantID, name)
	}
	return id, nil
}

// Upsert writes a batch of QSO records by UUID in a single transaction — inserts
// new rows, updates existing ones; a Record with DeletedAt set is a tombstone.
// The update is guarded on modified_at (ON CONFLICT ... WHERE): an incoming record
// overwrites a stored one only when it is at least as new, so a stale or reordered
// push can't clobber a newer row (reconcile soundness, ADR 0040).
func (s *Store) Upsert(ctx context.Context, recs []Record) error {
	const op errors.Op = "store.Upsert"
	if len(recs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	const q = `
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, deleted_at, payload)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (uuid) DO UPDATE SET
    tenant_id   = EXCLUDED.tenant_id,
    logbook_id  = EXCLUDED.logbook_id,
    modified_at = EXCLUDED.modified_at,
    deleted_at  = EXCLUDED.deleted_at,
    payload     = EXCLUDED.payload
WHERE EXCLUDED.modified_at >= qsos.modified_at`
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("prepare")
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range recs {
		if _, err := stmt.ExecContext(ctx, r.UUID, r.TenantID, r.LogbookID,
			r.ModifiedAt, r.DeletedAt, []byte(r.Payload)); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("uuid %s", r.UUID)
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit")
	}
	return nil
}

// Manifest returns the (uuid, modified_at, deleted) list for a logbook — the diff
// basis a peer reconciles against. Tombstones are included (the cloud is
// retentive). Ordered by uuid for stable diffing.
func (s *Store) Manifest(ctx context.Context, logbookID int64) ([]ManifestEntry, error) {
	const op errors.Op = "store.Manifest"
	const q = `
SELECT uuid, modified_at, deleted_at IS NOT NULL
FROM qsos WHERE logbook_id = $1 ORDER BY uuid`
	rows, err := s.db.QueryContext(ctx, q, logbookID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("logbook %d", logbookID)
	}
	defer func() { _ = rows.Close() }()

	var out []ManifestEntry
	for rows.Next() {
		var e ManifestEntry
		if err := rows.Scan(&e.UUID, &e.ModifiedAt, &e.Deleted); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan")
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("rows")
	}
	return out, nil
}

// Get returns the full record for a UUID, tombstones included (restore needs the
// deleted marker). Returns ErrNotFound (errors.Is-matchable) when no row matches.
func (s *Store) Get(ctx context.Context, uuid string) (Record, error) {
	const op errors.Op = "store.Get"
	const q = `
SELECT uuid, tenant_id, logbook_id, modified_at, deleted_at, payload
FROM qsos WHERE uuid = $1`
	var (
		r       Record
		payload []byte
	)
	err := s.db.QueryRowContext(ctx, q, uuid).Scan(
		&r.UUID, &r.TenantID, &r.LogbookID, &r.ModifiedAt, &r.DeletedAt, &payload)
	if stderr.Is(err, sql.ErrNoRows) {
		return Record{}, errors.New(op).WithErr(ErrNotFound).WithMsgf("uuid %s", uuid)
	}
	if err != nil {
		return Record{}, errors.New(op).WithErr(err).WithMsgf("uuid %s", uuid)
	}
	r.Payload = payload
	return r, nil
}
