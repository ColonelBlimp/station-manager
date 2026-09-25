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
// logbook then destination. Credentials come back verbatim: callers that put
// them on a wire mask them.
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
		       d.remote_adopted_at, d.created_at, d.modified_at
		FROM logbook_destination d
		         JOIN logbook l ON l.id = d.logbook_id
		WHERE l.deleted_at IS NULL
		ORDER BY d.logbook_id, d.destination`)
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
			modified sql.NullTime
		)
		if err := rows.Scan(&d.ID, &d.LogbookID, &d.Destination, &d.ForwarderName, &enabled, &creds, &adopted, &d.CreatedAt, &modified); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("scan logbook destination")
		}
		d.Enabled = enabled == 1
		if creds.Valid && creds.String != "" {
			d.Credentials = json.RawMessage(creds.String)
		}
		if adopted.Valid {
			t := adopted.Time
			d.RemoteAdoptedAt = &t
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
// already exists keeps its name and the rows follow THAT name — then sets the
// marker. A nil seed list records the decision with no rows (a managed or
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
				INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (logbook_id, destination) DO NOTHING`, l.id, sd.Destination, name, enabled, creds)
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
