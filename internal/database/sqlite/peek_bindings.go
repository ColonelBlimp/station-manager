package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// PeekedBindings is what an offline command reads from a CLOSED archive file
// about its destination bindings (W-0021 5C): `smd restore` and `smd
// config-downgrade` run with the daemon stopped and must not open, migrate or
// write the file to learn this.
type PeekedBindings struct {
	// Identity is the file's identity row; HasIdentity is false for a file no
	// identity-aware daemon has started.
	Identity    ArchiveIdentity
	HasIdentity bool
	// HasBindingsTable is false below log schema 14: the file holds no binding
	// at all, and a stripped config cannot be rebuilt from it.
	HasBindingsTable bool
	// Seeded reports the one-time seed marker as set.
	Seeded bool
	// Logbooks are the file's LIVE logbooks, by id.
	Logbooks []types.Logbook
	// Bindings are every binding of a live logbook, as ListLogbookDestinations
	// orders them (logbook, then insertion).
	Bindings []types.LogbookDestination
	// CollapseTargets is, per destination, the ONE name migration 0015's down
	// step renames that destination's queue rows to — computed by the same
	// SELECT over ALL bindings, a soft-deleted logbook's included, so a v5
	// entry rebuilt under it drains exactly those rows (codex P1 on 52077508).
	CollapseTargets map[string]string
	// IdentityStamped counts every binding marked born on SM Cloud identity
	// (ADR 0092), enabled or not and a deleted logbook's included; 0 on a file
	// below log schema 18, which has no such column.
	IdentityStamped int
}

// PeekDestinationBindings reads an archive file's identity, seed marker, live
// logbooks and destination bindings through a separate READ-ONLY connection.
// A missing file is an error: the caller asked about a file it named.
func PeekDestinationBindings(path string) (PeekedBindings, error) {
	const op errors.Op = "sqlite.PeekDestinationBindings"
	if path == "" || path == ":memory:" {
		return PeekedBindings{}, errors.New(op).WithMsg("no archive file to read")
	}
	if _, err := os.Stat(path); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err).WithMsg("stat archive file")
	}
	db, err := sql.Open(SqliteDriver, "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)&_time_format=sqlite")
	if err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err).WithMsg("open archive read-only")
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	var out PeekedBindings

	if has, err := hasTable(db, "archive_metadata"); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err).WithMsg("inspect archive file")
	} else if has {
		out.Identity, err = readArchiveIdentity(ctx, op, db)
		switch {
		case err == nil:
			out.HasIdentity = true
		case !stderr.Is(err, errors.ErrNotFound):
			return PeekedBindings{}, err
		}
	}
	// The logbooks are read before the binding table is looked for: schemas 12
	// and 13 carry logbook UUIDs, which an identity restore resolves by, and
	// no binding table.
	if out.Logbooks, err = peekLiveLogbooks(ctx, db); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err)
	}
	if out.HasBindingsTable, err = hasTable(db, "logbook_destination"); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err).WithMsg("inspect archive file")
	}
	if !out.HasBindingsTable {
		return out, nil
	}
	if out.HasIdentity {
		var at sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT destination_bindings_seeded_at FROM archive_metadata WHERE singleton = 1`).Scan(&at); err != nil {
			return PeekedBindings{}, errors.New(op).WithErr(err).WithMsg("read the seed marker")
		}
		out.Seeded = at.Valid
	}
	if out.Bindings, err = peekBindings(ctx, db); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err)
	}
	if out.CollapseTargets, err = peekCollapseTargets(ctx, db); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err)
	}
	if out.IdentityStamped, err = peekIdentityStamped(ctx, db); err != nil {
		return PeekedBindings{}, errors.New(op).WithErr(err)
	}
	return out, nil
}

// peekIdentityStamped counts the stamped bindings, all of them; a file below
// schema 18 has no identity_since column and none.
func peekIdentityStamped(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'identity_since'`).Scan(&n); err != nil || n == 0 {
		return 0, err
	}
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM logbook_destination WHERE identity_since IS NOT NULL`).Scan(&n)
	return n, err
}

// peekCollapseTargets runs migration 0015 down's target SELECT read-only (keep
// the two in step). At schema 14 there is no legacy_name, and the target is
// the default logbook's binding, else the first name.
func peekCollapseTargets(ctx context.Context, db *sql.DB) (map[string]string, error) {
	legacy := "NULL"
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = 'legacy_name'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 1 {
		legacy = `(SELECT d0.legacy_name
		           FROM logbook_destination d0
		           WHERE d0.destination = d.destination AND d0.legacy_name IS NOT NULL
		           ORDER BY d0.id
		           LIMIT 1)`
	}
	rows, err := db.QueryContext(ctx, `
		SELECT d.destination,
		       COALESCE(`+legacy+`,
		                (SELECT d2.forwarder_name
		                 FROM logbook_destination d2
		                          JOIN archive_metadata am ON am.singleton = 1 AND d2.logbook_id = am.default_logbook_id
		                 WHERE d2.destination = d.destination),
		                MIN(d.forwarder_name))
		FROM logbook_destination d
		GROUP BY d.destination`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var dest, target string
		if err := rows.Scan(&dest, &target); err != nil {
			return nil, err
		}
		out[dest] = target
	}
	return out, rows.Err()
}

// peekLiveLogbooks reads the live logbooks; below log schema 12 there is no
// uuid column, and every logbook reads with none.
func peekLiveLogbooks(ctx context.Context, db *sql.DB) ([]types.Logbook, error) {
	uuid := "''"
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('logbook') WHERE name = 'uuid'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 1 {
		uuid = "COALESCE(uuid, '')"
	}
	rows, err := db.QueryContext(ctx, `SELECT id, `+uuid+`, name, callsign FROM logbook WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []types.Logbook
	for rows.Next() {
		var lb types.Logbook
		if err := rows.Scan(&lb.ID, &lb.UUID, &lb.Name, &lb.Callsign); err != nil {
			return nil, err
		}
		out = append(out, lb)
	}
	return out, rows.Err()
}

// peekBindings reads the live logbooks' bindings. legacy_name exists from log
// schema 15 and identity_since from 18; below those they read as empty.
func peekBindings(ctx context.Context, db *sql.DB) ([]types.LogbookDestination, error) {
	column := func(name string) (string, error) {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('logbook_destination') WHERE name = ?`, name).Scan(&n); err != nil || n == 0 {
			return "NULL", err
		}
		return "d." + name, nil
	}
	legacy, err := column("legacy_name")
	if err != nil {
		return nil, err
	}
	identity, err := column("identity_since")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT d.id, d.logbook_id, d.destination, d.forwarder_name, d.enabled, d.credentials, `+legacy+`, `+identity+`
		FROM logbook_destination d
		         JOIN logbook l ON l.id = d.logbook_id
		WHERE l.deleted_at IS NULL
		ORDER BY d.logbook_id, d.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []types.LogbookDestination
	for rows.Next() {
		var (
			d       types.LogbookDestination
			enabled int64
			creds   sql.NullString
			name    sql.NullString
			since   sql.NullTime
		)
		if err := rows.Scan(&d.ID, &d.LogbookID, &d.Destination, &d.ForwarderName, &enabled, &creds, &name, &since); err != nil {
			return nil, err
		}
		d.IdentitySince = timePtr(since)
		d.Enabled = enabled == 1
		if creds.Valid && creds.String != "" {
			d.Credentials = json.RawMessage(creds.String)
		}
		d.LegacyName = name.String
		out = append(out, d)
	}
	return out, rows.Err()
}
