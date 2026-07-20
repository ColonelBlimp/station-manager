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

// ErrEmptyPayload is returned by Upsert when a Record carries no JSON payload.
// The payload column is NOT NULL, so catching it here yields a UUID-tagged
// error rather than a bare Postgres constraint violation. errors.Is-matchable.
var ErrEmptyPayload = stderr.New("smcloud: record has empty payload")

// canonicalPrecision is the timestamp resolution the reconcile protocol pins
// modified_at / deleted_at to. Postgres TIMESTAMPTZ stores microseconds while Go
// time.Time (and the local SQLite side) carry nanoseconds; reconcile diffs a
// hash-of-sorted-(uuid, modified_at) (ADR 0040), so a nanosecond local value
// that never equals its microsecond-truncated stored form would re-flag every
// QSO every cycle and re-push the whole logbook — exactly wrong over the
// flaky-link constraint the project weights up. Truncating on write pins the
// stored value to what Postgres would keep anyway; the local peer applying the
// same truncation before it hashes is what makes the two sides compare equal.
const canonicalPrecision = time.Microsecond

// canonicalTime truncates t to canonicalPrecision (a no-op on an already-µs
// value). Keeps modified_at / deleted_at at the one resolution both reconcile
// ends agree on.
func canonicalTime(t time.Time) time.Time { return t.Truncate(canonicalPrecision) }

// Store is the smcloud Postgres persistence layer (see package doc). Driver-
// agnostic: the caller owns the *sql.DB (and its Close).
type Store struct {
	db *sql.DB
}

// New wraps an open *sql.DB.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Record is one stored QSO: the identity + reconcile columns lifted out for
// indexing, plus the full types.Qso as JSON. DeletedAt non-nil marks a tombstone.
// Revision is the row's monotonic edit counter (ADR 0050) — the primary
// ordering the upsert guard applies; 0 = pre-revision legacy row.
type Record struct {
	UUID       string
	TenantID   int64
	LogbookID  int64
	ModifiedAt time.Time
	Revision   int64
	DeletedAt  *time.Time
	Payload    json.RawMessage
}

// ManifestEntry is one row of a logbook's reconcile manifest — the minimal
// (uuid, modified_at, revision, deleted) tuple a peer diffs against its local
// copy. Revision 0 = pre-revision row (ADR 0050).
type ManifestEntry struct {
	UUID       string    `json:"uuid"`
	ModifiedAt time.Time `json:"modified_at"`
	Revision   int64     `json:"revision"`
	Deleted    bool      `json:"deleted"`
}

// EnsureTenant returns the id of the tenant for callsign, creating it on first
// use. Idempotent (callsign is unique), so it is safe on every startup. A
// re-ensure with an EMPTY name keeps the stored name — "ensure" must not wipe a
// previously stored value; a non-empty name updates it (the caller is
// authoritative). The DO UPDATE (rather than DO NOTHING) is also what lets
// RETURNING id yield the existing row's id on conflict.
//
// The unique constraint is case-SENSITIVE, and boot only started normalising
// callsigns (trim + uppercase) after early deployments existed — so a database
// provisioned by an older build may hold a case-variant row ("7q5mlv").
// Upserting the canonical spelling against that would create a NEW empty
// tenant beside it and silently orphan the existing backup from the token's
// view. So: a single case-insensitive match is first RENAMED to the canonical
// spelling and reused; multiple rows differing only in case (corrupt
// provisioning) fail loudly rather than guess which one owns the data.
func (s *Store) EnsureTenant(ctx context.Context, callsign, name string) (int64, error) {
	const op errors.Op = "store.EnsureTenant"
	variants, err := s.tenantCaseVariants(ctx, callsign)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("case-insensitive lookup %q", callsign)
	}
	switch {
	case len(variants) > 1:
		return 0, errors.New(op).WithMsgf(
			"%d tenants differ only in case from %q — merge them manually before starting",
			len(variants), callsign)
	case len(variants) == 1 && variants[0].callsign != callsign:
		if _, err := s.db.ExecContext(ctx,
			`UPDATE tenants SET callsign = $1 WHERE id = $2`, callsign, variants[0].id); err != nil {
			return 0, errors.New(op).WithErr(err).
				WithMsgf("canonicalise tenant %q → %q", variants[0].callsign, callsign)
		}
	}
	const q = `
INSERT INTO tenants (callsign, name) VALUES ($1, $2)
ON CONFLICT (callsign) DO UPDATE SET name = COALESCE(NULLIF(EXCLUDED.name, ''), tenants.name)
RETURNING id`
	var id int64
	if err := s.db.QueryRowContext(ctx, q, callsign, name).Scan(&id); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("callsign %q", callsign)
	}
	return id, nil
}

// tenantVariant is one row matching a callsign case-insensitively.
type tenantVariant struct {
	id       int64
	callsign string
}

func (s *Store) tenantCaseVariants(ctx context.Context, callsign string) ([]tenantVariant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, callsign FROM tenants WHERE UPPER(callsign) = UPPER($1)`, callsign)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []tenantVariant
	for rows.Next() {
		var v tenantVariant
		if err := rows.Scan(&v.id, &v.callsign); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
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
// It returns the number of rows actually applied: an insert or update counts,
// while a row rejected by the stale-push guard does not, so a caller can log
// "pushed N, applied M" and see how many were stale (sync telemetry).
//
// Guards on the ON CONFLICT WHERE:
//   - revision, then modified_at (ADR 0050): an incoming record overwrites a
//     stored one when its revision is strictly higher; on a revision TIE the
//     modified_at comparison decides (>=, so an identical re-push is
//     idempotent). Revision is the primary order because local modified_at is
//     second-precision wall clock — it cannot order two edits within one
//     second, and a reconcile-goroutine push racing the serial worker could
//     regress the payload invisibly on such a tie. All-legacy rows (both
//     revisions 0) get exactly the pre-0050 semantics via the fallback.
//     A NEWER non-tombstone over a tombstone resurrects the row by design —
//     edit-after-delete wins by recency (local is authoritative); a STALE
//     missed-delete push is rejected by this same guard, so the tombstone holds.
//   - Tenant isolation is structural: uuid uniqueness is scoped to
//     (tenant_id, uuid) (migration 0004), so a conflict can only ever be an
//     intra-tenant re-push. QSO UUIDs are client-generated and appear in every
//     exported logbook (not secret) — under the pre-0004 global-unique uuid, a
//     cross-tenant collision hit the old WHERE tenant guard and became a
//     zero-row "success" (applied 0), making that UUID permanently unbackable
//     for the later tenant (review 2026-07-20 #2). With the composite key the
//     same UUID simply coexists per tenant; no guard clause is needed.
//
// modified_at / deleted_at are truncated to canonicalPrecision so the stored
// value matches what the reconcile hash expects (see canonicalPrecision).
func (s *Store) Upsert(ctx context.Context, recs []Record) (int, error) {
	const op errors.Op = "store.Upsert"
	if len(recs) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	const q = `
INSERT INTO qsos (uuid, tenant_id, logbook_id, modified_at, revision, deleted_at, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, uuid) DO UPDATE SET
    logbook_id  = EXCLUDED.logbook_id,
    modified_at = EXCLUDED.modified_at,
    revision    = EXCLUDED.revision,
    deleted_at  = EXCLUDED.deleted_at,
    payload     = EXCLUDED.payload
WHERE EXCLUDED.revision > qsos.revision
   OR (EXCLUDED.revision = qsos.revision AND EXCLUDED.modified_at >= qsos.modified_at)`
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("prepare")
	}
	defer func() { _ = stmt.Close() }()

	applied := 0
	for _, r := range recs {
		// The payload column is NOT NULL; fail here with the UUID rather than let
		// a nil payload surface as a context-free Postgres constraint violation.
		if len(r.Payload) == 0 {
			return 0, errors.New(op).WithErr(ErrEmptyPayload).WithMsgf("uuid %s", r.UUID)
		}
		var deletedAt *time.Time
		if r.DeletedAt != nil {
			d := canonicalTime(*r.DeletedAt)
			deletedAt = &d
		}
		res, err := stmt.ExecContext(ctx, r.UUID, r.TenantID, r.LogbookID,
			canonicalTime(r.ModifiedAt), r.Revision, deletedAt, []byte(r.Payload))
		if err != nil {
			return 0, errors.New(op).WithErr(err).WithMsgf("uuid %s", r.UUID)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, errors.New(op).WithErr(err).WithMsgf("uuid %s: rows affected", r.UUID)
		}
		applied += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("commit")
	}
	return applied, nil
}

// Manifest returns the (uuid, modified_at, deleted) list for a logbook — the diff
// basis a peer reconciles against. Tombstones are included (the cloud is
// retentive). Ordered by uuid for stable diffing.
func (s *Store) Manifest(ctx context.Context, logbookID int64) ([]ManifestEntry, error) {
	const op errors.Op = "store.Manifest"
	const q = `
SELECT uuid, modified_at, revision, deleted_at IS NOT NULL
FROM qsos WHERE logbook_id = $1 ORDER BY uuid`
	rows, err := s.db.QueryContext(ctx, q, logbookID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("logbook %d", logbookID)
	}
	defer func() { _ = rows.Close() }()

	var out []ManifestEntry
	for rows.Next() {
		var e ManifestEntry
		if err := rows.Scan(&e.UUID, &e.ModifiedAt, &e.Revision, &e.Deleted); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan")
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("rows")
	}
	return out, nil
}

// LogbookInfo is a logbook's identity row — what the HTTP layer lists and
// checks ownership against (TenantID never goes on the wire).
type LogbookInfo struct {
	ID       int64  `json:"id"`
	TenantID int64  `json:"-"`
	Name     string `json:"name"`
}

// querier is the read surface shared by *sql.DB and *sql.Tx, so the row-scan
// helpers serve both the autocommit reads and ExportSnapshot's transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Logbooks lists a tenant's logbooks, ordered by id (creation order).
func (s *Store) Logbooks(ctx context.Context, tenantID int64) ([]LogbookInfo, error) {
	return queryLogbooks(ctx, "store.Logbooks", s.db, tenantID)
}

func queryLogbooks(ctx context.Context, op errors.Op, q querier, tenantID int64) ([]LogbookInfo, error) {
	const query = `SELECT id, tenant_id, name FROM logbooks WHERE tenant_id = $1 ORDER BY id`
	rows, err := q.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("tenant %d", tenantID)
	}
	defer func() { _ = rows.Close() }()

	var out []LogbookInfo
	for rows.Next() {
		var l LogbookInfo
		if err := rows.Scan(&l.ID, &l.TenantID, &l.Name); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan")
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("rows")
	}
	return out, nil
}

// Logbook returns one logbook's identity row — the ownership check the HTTP
// layer runs before serving a per-logbook read. Returns ErrNotFound
// (errors.Is-matchable) when the id doesn't exist.
func (s *Store) Logbook(ctx context.Context, id int64) (LogbookInfo, error) {
	const op errors.Op = "store.Logbook"
	const q = `SELECT id, tenant_id, name FROM logbooks WHERE id = $1`
	var l LogbookInfo
	err := s.db.QueryRowContext(ctx, q, id).Scan(&l.ID, &l.TenantID, &l.Name)
	if stderr.Is(err, sql.ErrNoRows) {
		return LogbookInfo{}, errors.New(op).WithErr(ErrNotFound).WithMsgf("logbook %d", id)
	}
	if err != nil {
		return LogbookInfo{}, errors.New(op).WithErr(err).WithMsgf("logbook %d", id)
	}
	return l, nil
}

// Export returns EVERY record a tenant owns, tombstones included (restore needs
// the deleted markers), ordered by (logbook_id, uuid) for a stable dump.
func (s *Store) Export(ctx context.Context, tenantID int64) ([]Record, error) {
	return queryExport(ctx, "store.Export", s.db, tenantID)
}

// ExportSnapshot streams a tenant's logbooks AND every record it owns from
// ONE repeatable-read, read-only transaction — the consistent dump behind
// GET /v1/export (the full-fidelity restore source). Two separate autocommit
// reads can interleave with a concurrent push: a new logbook and its first
// batch committing between them yields QSOs whose logbook_id is absent from
// the same dump's logbook list, which restore has no way to map.
//
// onBooks is called exactly once, before any onRecord call, with the (small,
// bounded) logbook list; onRecord is then called once per QSO row. Records are
// NOT accumulated — the export set is unbounded (live rows + tombstones, per
// tenant, growing forever), so materialising it made every export an
// O(logbook) heap allocation and a service-level OOM lever (review 2026-07-20
// #3). A non-nil error from either callback aborts the scan and is returned
// verbatim (no wrapping — the caller distinguishes its own write errors from
// store read errors by identity). The transaction never escapes this method.
func (s *Store) ExportSnapshot(ctx context.Context, tenantID int64,
	onBooks func([]LogbookInfo) error, onRecord func(Record) error) error {
	const op errors.Op = "store.ExportSnapshot"
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }() // read-only; rollback is the cheap close
	books, err := queryLogbooks(ctx, op, tx, tenantID)
	if err != nil {
		return err
	}
	if err := onBooks(books); err != nil {
		return err
	}
	return streamExport(ctx, op, tx, tenantID, onRecord)
}

func queryExport(ctx context.Context, op errors.Op, q querier, tenantID int64) ([]Record, error) {
	var out []Record
	err := streamExport(ctx, op, q, tenantID, func(r Record) error {
		out = append(out, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// streamExport row-streams a tenant's full record set (tombstones included)
// to onRecord in (logbook_id, uuid) order. Callback errors abort the scan and
// return unwrapped (see ExportSnapshot).
func streamExport(ctx context.Context, op errors.Op, q querier, tenantID int64,
	onRecord func(Record) error) error {
	const query = `
SELECT uuid, tenant_id, logbook_id, modified_at, revision, deleted_at, payload
FROM qsos WHERE tenant_id = $1 ORDER BY logbook_id, uuid`
	rows, err := q.QueryContext(ctx, query, tenantID)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("tenant %d", tenantID)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			r       Record
			payload []byte
		)
		if err := rows.Scan(&r.UUID, &r.TenantID, &r.LogbookID,
			&r.ModifiedAt, &r.Revision, &r.DeletedAt, &payload); err != nil {
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

// Get returns the full record for a tenant's UUID, tombstones included (restore
// needs the deleted marker). Tenant-scoped because uuid alone stopped being a
// key at migration 0004 — the same UUID may exist under multiple tenants.
// Returns ErrNotFound (errors.Is-matchable) when no row matches.
func (s *Store) Get(ctx context.Context, tenantID int64, uuid string) (Record, error) {
	const op errors.Op = "store.Get"
	const q = `
SELECT uuid, tenant_id, logbook_id, modified_at, revision, deleted_at, payload
FROM qsos WHERE tenant_id = $1 AND uuid = $2`
	var (
		r       Record
		payload []byte
	)
	err := s.db.QueryRowContext(ctx, q, tenantID, uuid).Scan(
		&r.UUID, &r.TenantID, &r.LogbookID, &r.ModifiedAt, &r.Revision, &r.DeletedAt, &payload)
	if stderr.Is(err, sql.ErrNoRows) {
		return Record{}, errors.New(op).WithErr(ErrNotFound).WithMsgf("uuid %s", uuid)
	}
	if err != nil {
		return Record{}, errors.New(op).WithErr(err).WithMsgf("uuid %s", uuid)
	}
	r.Payload = payload
	return r, nil
}
