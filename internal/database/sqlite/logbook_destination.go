package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// DestinationSeed is one legacy config entry as the one-time seed sees it
// (ADR 0082 part 4): the destination type, the entry's durable name, its
// enabled state and its LOGBOOK-scoped credential fields only.
type DestinationSeed struct {
	Destination string
	LegacyName  string
	Enabled     bool
	Credentials json.RawMessage
}

// SeedLogbookDestinationsResult reports what one seed call did. Seeded is
// false when the marker was already set — the call decided nothing.
type SeedLogbookDestinationsResult struct {
	Seeded   bool
	Inserted int
	Renamed  int64
}

// ListLogbookDestinationsWithContext returns every binding of a LIVE logbook
// (a soft-deleted logbook's bindings are inert, never a worker), ordered by
// logbook then insertion — the seed inserts in config order, so the adopted
// archive's fan-out keeps the order config.json had (the 5A pin). Credentials
// come back verbatim: callers that put them on a wire mask them.
func (s *Service) ListLogbookDestinationsWithContext(ctx context.Context) ([]types.LogbookDestination, error) {
	const op errors.Op = "sqlite.Service.ListLogbookDestinationsWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	rows, err := h.QueryContext(ctx, `
		SELECT d.id, d.logbook_id, d.destination, d.forwarder_name, d.enabled, d.credentials,
		       d.remote_adopted_at, d.adoption_reserved_at, d.remote_adopted_account, d.legacy_name, d.created_at, d.modified_at
		FROM logbook_destination d
		         JOIN logbook l ON l.id = d.logbook_id
		WHERE l.deleted_at IS NULL
		ORDER BY d.logbook_id, d.id`)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("list logbook destinations")
	}
	defer func() { _ = rows.Close() }()
	var out []types.LogbookDestination
	for rows.Next() {
		var (
			d        types.LogbookDestination
			enabled  int64
			creds    sql.NullString
			adopted  sql.NullTime
			reserved sql.NullTime
			account  sql.NullString
			legacy   sql.NullString
			modified sql.NullTime
		)
		if err := rows.Scan(&d.ID, &d.LogbookID, &d.Destination, &d.ForwarderName, &enabled, &creds, &adopted, &reserved, &account, &legacy, &d.CreatedAt, &modified); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan logbook destination")
		}
		d.Enabled = enabled == 1
		if creds.Valid && creds.String != "" {
			d.Credentials = json.RawMessage(creds.String)
		}
		d.RemoteAdoptedAt, d.AdoptionReservedAt, d.RemoteAdoptedAccount = timePtr(adopted), timePtr(reserved), account.String
		if legacy.Valid {
			d.LegacyName = legacy.String
		}
		if modified.Valid {
			t := modified.Time
			d.ModifiedAt = &t
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("iterate logbook destinations")
	}
	return out, nil
}

// DestinationBindingsSeededAtWithContext reads the one-time adoption marker:
// nil when the file's seed has not been decided yet, errors.ErrNotFound when
// the file has no identity row at all.
func (s *Service) DestinationBindingsSeededAtWithContext(ctx context.Context) (*time.Time, error) {
	const op errors.Op = "sqlite.Service.DestinationBindingsSeededAtWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	var at sql.NullTime
	err = h.QueryRowContext(ctx, `SELECT destination_bindings_seeded_at FROM archive_metadata WHERE singleton = 1`).Scan(&at)
	if stderr.Is(err, sql.ErrNoRows) {
		return nil, errors.ErrNotFound
	}
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("read seed marker")
	}
	if !at.Valid {
		return nil, nil
	}
	t := at.Time
	return &t, nil
}

// SeedLogbookDestinationsWithContext decides the file's bindings ONCE (ADR
// 0082 part 4), in one transaction: while the marker is NULL it inserts, for
// every live logbook and every seed, the binding that is missing — the file's
// default logbook (else the lowest id) keeps the seed's legacy name, every
// other logbook is named `<destination>.<logbook uuid>` and its existing
// queue rows under the legacy name are renamed to match — a binding that
// already exists keeps its name and the rows follow THAT name — every created
// row records the legacy name it derives from, then the marker is set. A nil seed list records the decision with no rows (a managed or
// external archive). Once the marker is set the call changes nothing, so a
// retry never overwrites a durable row and a later logbook stays unbound.
// errors.ErrNotFound when the file has no identity row; any failure rolls the
// whole seed back, marker included.
func (s *Service) SeedLogbookDestinationsWithContext(ctx context.Context, seeds []DestinationSeed) (SeedLogbookDestinationsResult, error) {
	const op errors.Op = "sqlite.Service.SeedLogbookDestinationsWithContext"
	var res SeedLogbookDestinationsResult
	if err := checkService(op, s); err != nil {
		return res, err
	}
	tx, cancel, err := s.BeginTxContext(ctx)
	if err != nil {
		return res, errors.New(op).WithErr(err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()

	var (
		marker sql.NullTime
		def    sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `SELECT destination_bindings_seeded_at, default_logbook_id FROM archive_metadata WHERE singleton = 1`).Scan(&marker, &def)
	if stderr.Is(err, sql.ErrNoRows) {
		return res, errors.ErrNotFound
	}
	if err != nil {
		return res, errors.New(op).WithErr(err).WithMsg("read seed marker")
	}
	if marker.Valid {
		return res, nil
	}

	type lb struct {
		id   int64
		uuid sql.NullString
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, uuid FROM logbook WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return res, errors.New(op).WithErr(err).WithMsg("list logbooks")
	}
	var logbooks []lb
	for rows.Next() {
		var l lb
		if err := rows.Scan(&l.id, &l.uuid); err != nil {
			_ = rows.Close()
			return res, errors.New(op).WithErr(err).WithMsg("scan logbook")
		}
		logbooks = append(logbooks, l)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return res, errors.New(op).WithErr(err).WithMsg("iterate logbooks")
	}

	// The keeper of each legacy name: the file's default logbook when it names
	// a live row, otherwise the lowest id — so the ordinary one-logbook Home
	// never sees a queue name or API path change.
	keeper := int64(0)
	for _, l := range logbooks {
		if def.Valid && l.id == def.Int64 {
			keeper = l.id
		}
	}
	if keeper == 0 && len(logbooks) > 0 {
		keeper = logbooks[0].id
	}

	for _, l := range logbooks {
		for _, sd := range seeds {
			name := sd.LegacyName
			if l.id != keeper {
				if !l.uuid.Valid || l.uuid.String == "" {
					return res, errors.New(op).WithMsgf("logbook %d has no uuid; cannot name its %s binding", l.id, sd.Destination)
				}
				name = sd.Destination + "." + l.uuid.String
			}
			enabled := 0
			if sd.Enabled {
				enabled = 1
			}
			var creds any
			if len(sd.Credentials) > 0 {
				creds = string(sd.Credentials)
			}
			r, err := tx.ExecContext(ctx, `
				INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, legacy_name)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (logbook_id, destination) DO NOTHING`, l.id, sd.Destination, name, enabled, creds, sd.LegacyName)
			if err != nil {
				return res, errors.New(op).WithErr(err).WithMsgf("seed %s binding for logbook %d", sd.Destination, l.id)
			}
			if n, _ := r.RowsAffected(); n == 1 {
				res.Inserted++
			} else {
				// A durable row already binds this (logbook, destination): its
				// name is the one the queue must follow, never a freshly computed
				// one (a rename to a name no binding carries would strand rows).
				if err := tx.QueryRowContext(ctx, `SELECT forwarder_name FROM logbook_destination WHERE logbook_id = ? AND destination = ?`,
					l.id, sd.Destination).Scan(&name); err != nil {
					return res, errors.New(op).WithErr(err).WithMsgf("read the existing %s binding of logbook %d", sd.Destination, l.id)
				}
			}
			if name != sd.LegacyName {
				r, err := tx.ExecContext(ctx, `
					UPDATE qso_upload SET forwarder_name = ?
					WHERE forwarder_name = ? AND qso_id IN (SELECT id FROM qso WHERE logbook_id = ?)`, name, sd.LegacyName, l.id)
				if err != nil {
					return res, errors.New(op).WithErr(err).WithMsgf("rename queue rows of logbook %d to %s", l.id, name)
				}
				n, _ := r.RowsAffected()
				res.Renamed += n
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE archive_metadata SET destination_bindings_seeded_at = datetime('now') WHERE singleton = 1`); err != nil {
		return res, errors.New(op).WithErr(err).WithMsg("set seed marker")
	}
	if err := tx.Commit(); err != nil {
		return res, errors.New(op).WithErr(err).WithMsg("commit seed")
	}
	res.Seeded = true
	return res, nil
}

// DestinationUpsert is one binding row to write: an existing (logbook,
// destination) row is updated in place (enabled, credentials, modified_at);
// a missing one is inserted under ForwarderName with no legacy name.
type DestinationUpsert struct {
	LogbookID     int64
	Destination   string
	ForwarderName string
	Enabled       bool
	Credentials   json.RawMessage
}

// UpsertLogbookDestinationsWithContext writes every row in ONE transaction:
// all or none (ADR 0082 part 9 — the PUT validates the complete candidate
// first and commits atomically). forwarder_name is never changed on an
// existing row.
func (s *Service) UpsertLogbookDestinationsWithContext(ctx context.Context, rows []DestinationUpsert) error {
	const op errors.Op = "sqlite.Service.UpsertLogbookDestinationsWithContext"
	if err := checkService(op, s); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	tx, cancel, err := s.BeginTxContext(ctx)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	defer cancel()
	defer func() { _ = tx.Rollback() }()
	for _, r := range rows {
		enabled := 0
		if r.Enabled {
			enabled = 1
		}
		var creds any
		if len(r.Credentials) > 0 && string(r.Credentials) != "null" && string(r.Credentials) != "{}" {
			creds = string(r.Credentials)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (logbook_id, destination) DO UPDATE SET
				enabled = excluded.enabled,
				credentials = excluded.credentials,
				modified_at = datetime('now')`,
			r.LogbookID, r.Destination, r.ForwarderName, enabled, creds); err != nil {
			return errors.New(op).WithErr(err).WithMsgf("write %s binding for logbook %d", r.Destination, r.LogbookID)
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("commit bindings")
	}
	return nil
}

// RecordLogbookDestinationAdoptedWithContext records an adoption's
// confirmation on the binding an attempt reserved and read (ADR 0091):
// remote_adopted_at and account, the fingerprint of the station account it was
// confirmed under, in one write. It writes only while the binding is reserved,
// enabled, on logbookID (a live logbook), holds exactly the credentials passed
// (nil = none), and is not already confirmed under account; a binding
// confirmed under another account, or under none, is re-confirmed. It reports
// whether it wrote; false with a nil error means nothing matched. The write is
// durable before it returns (durableUpdate): the confirmation moves the
// binding onto the identity wire at the next start.
func (s *Service) RecordLogbookDestinationAdoptedWithContext(ctx context.Context, forwarderName string, logbookID int64, credentials json.RawMessage, account string) (bool, error) {
	const op errors.Op = "sqlite.Service.RecordLogbookDestinationAdoptedWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	if account == "" {
		return false, errors.New(op).WithMsg("a confirmation needs the account it was made under")
	}
	return s.durableUpdate(ctx, op, "record adoption", `
		UPDATE logbook_destination SET remote_adopted_at = datetime('now'), remote_adopted_account = ?
		WHERE forwarder_name = ? AND logbook_id = ? AND enabled = 1 AND credentials IS ?
		  AND adoption_reserved_at IS NOT NULL
		  AND (remote_adopted_at IS NULL OR remote_adopted_account IS NOT ?)
		  AND logbook_id IN (SELECT id FROM logbook WHERE deleted_at IS NULL)`,
		account, forwarderName, logbookID, credentialsArg(credentials), account)
}

// ReserveLogbookDestinationAdoptionWithContext records adoption_reserved_at on
// the binding an adoption attempt read (ADR 0091): the binding named
// forwarderName on logbookID, enabled, on a live logbook, not already
// confirmed under account (the attempt's station account), and with exactly
// the stored credentials passed (nil = none). A binding already reserved keeps
// its first reservation and reports true. false with a nil error means nothing
// matched. A true result is durable before it is returned (durableUpdate),
// because the adoption request follows it.
func (s *Service) ReserveLogbookDestinationAdoptionWithContext(ctx context.Context, forwarderName string, logbookID int64, credentials json.RawMessage, account string) (bool, error) {
	const op errors.Op = "sqlite.Service.ReserveLogbookDestinationAdoptionWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	return s.durableUpdate(ctx, op, "reserve adoption", `
		UPDATE logbook_destination SET adoption_reserved_at = COALESCE(adoption_reserved_at, datetime('now'))
		WHERE forwarder_name = ? AND logbook_id = ? AND enabled = 1 AND credentials IS ?
		  AND (remote_adopted_at IS NULL OR remote_adopted_account IS NOT ?)
		  AND logbook_id IN (SELECT id FROM logbook WHERE deleted_at IS NULL)`,
		forwarderName, logbookID, credentialsArg(credentials), account)
}

func credentialsArg(credentials json.RawMessage) any {
	if len(credentials) == 0 {
		return nil
	}
	return string(credentials)
}

// durableUpdate runs one UPDATE that must survive a power loss once it is
// reported (ADR 0091: an adoption's reservation and its confirmation), and
// reports whether it changed exactly one row. The pool runs WAL with
// synchronous=NORMAL, under which a committed transaction may roll back after a
// power failure or an OS crash (sqlite.org, PRAGMA synchronous). So the write
// runs on one pinned connection set to FULL before its transaction begins, the
// setting is read back inside the transaction, and the connection is set back
// to NORMAL afterwards. Any failure before the commit writes nothing.
func (s *Service) durableUpdate(ctx context.Context, op errors.Op, what, query string, args ...any) (bool, error) {
	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	conn, err := h.Conn(ctx)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("pin a connection")
	}
	defer func() { _ = conn.Close() }()
	if err := setSynchronous(ctx, conn, "FULL"); err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("set synchronous=FULL")
	}
	// Restored whatever happens, so the pooled connection goes back at the
	// pool's setting. A failed restore leaves it at FULL, which is only slower.
	defer func() { _ = setSynchronous(context.WithoutCancel(ctx), conn, "NORMAL") }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }()
	var level int
	if err := tx.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&level); err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("read synchronous")
	}
	if level != synchronousFull {
		return false, errors.New(op).WithMsgf("the connection runs synchronous=%d, not FULL; nothing was written (%s)", level, what)
	}
	r, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg(what)
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, errors.New(op).WithErr(err).WithMsg(what)
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New(op).WithErr(err).WithMsg("commit: " + what)
	}
	return n == 1, nil
}

// synchronousFull is PRAGMA synchronous's value for FULL.
const synchronousFull = 2

// setSynchronous sets PRAGMA synchronous on one connection. It is a variable
// so tests can observe the pinned connection and inject failures.
var setSynchronous = func(ctx context.Context, conn *sql.Conn, level string) error {
	_, err := conn.ExecContext(ctx, "PRAGMA synchronous = "+level)
	return err
}

// ListAdoptionClaimsWithContext returns every binding that is reserved for, or
// has recorded, an adoption (ADR 0091), whether enabled or not and whether its
// logbook is live or deleted: neither disabling nor deleting releases the name
// it protects. Only the fields the protection needs are filled.
func (s *Service) ListAdoptionClaimsWithContext(ctx context.Context) ([]types.LogbookDestination, error) {
	const op errors.Op = "sqlite.Service.ListAdoptionClaimsWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	rows, err := h.QueryContext(ctx, `
		SELECT id, logbook_id, destination, forwarder_name, enabled, credentials, adoption_reserved_at, remote_adopted_at, remote_adopted_account
		FROM logbook_destination
		WHERE adoption_reserved_at IS NOT NULL OR remote_adopted_at IS NOT NULL
		ORDER BY id`)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("list adoption claims")
	}
	defer func() { _ = rows.Close() }()
	var out []types.LogbookDestination
	for rows.Next() {
		var (
			d                 types.LogbookDestination
			enabled           int64
			creds             sql.NullString
			reserved, adopted sql.NullTime
			account           sql.NullString
		)
		if err := rows.Scan(&d.ID, &d.LogbookID, &d.Destination, &d.ForwarderName, &enabled, &creds, &reserved, &adopted, &account); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan adoption claim")
		}
		d.Enabled = enabled == 1
		if creds.Valid && creds.String != "" {
			d.Credentials = json.RawMessage(creds.String)
		}
		d.AdoptionReservedAt, d.RemoteAdoptedAt, d.RemoteAdoptedAccount = timePtr(reserved), timePtr(adopted), account.String
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("iterate adoption claims")
	}
	return out, nil
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
