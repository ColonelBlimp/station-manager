package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	moderncsqlite "modernc.org/sqlite"
	moderncsqlitelib "modernc.org/sqlite/lib"
)

func (s *Service) getOpenHandle(op errors.Op) (*sql.DB, error) {
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return nil, errors.New(op).WithMsg(errMsgNotOpen)
	}
	return h, nil
}

// getDsn returns the DSN for the SQLite database identified in the config.
func (s *Service) getDsn() (string, error) {
	const op errors.Op = "sqlite.Service.getDsn"

	if s.DatabaseConfig.Driver != SqliteDriver {
		return emptyString, errors.New(op).WithMsgf("Unsupported database driver: %s (expected %q)", s.DatabaseConfig.Driver, SqliteDriver)
	}

	path := s.DatabaseConfig.Path
	if path == emptyString {
		return emptyString, errors.New(op).WithMsg(errMsgEmptyPath)
	}

	s.LoggerService.InfoWith().Str("path", path).Msg("Using sqlite database file")

	// Merge operator-supplied options (raw query-param key/values) on
	// top of the driver-default base. Operator overrides win.
	opts := map[string]string{}
	maps.Copy(opts, s.DatabaseConfig.Options)

	// Driver-default PRAGMAs, applied per-connection via modernc's
	// `_pragma=name(value)` query-param convention. Each `_pragma`
	// runs on every new connection from the pool — load-bearing
	// because PRAGMAs like busy_timeout are connection-scoped, so a
	// pool > 1 needs them re-applied on every conn or only the
	// connection that ran the runtime ExecContext PRAGMA gets them.
	//
	// Earlier draft used mattn-style flat option names
	// (`_busy_timeout`, `_journal_mode`); modernc.org/sqlite ignores
	// those silently, so under pool > 1 the un-PRAGMA'd connections
	// returned SQLITE_BUSY immediately on write contention rather
	// than waiting up to busy_timeout. Caught during a stress run
	// 2026-05-09 once max_open_conns was bumped from 1 to 16.
	pragmas := []string{
		fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS),
		"journal_mode(WAL)",
		"foreign_keys(on)",
		// synchronous=NORMAL is safe under WAL (the WAL itself is
		// sync'd on commit; the main DB only at checkpoint) and is
		// the SQLite-recommended default for WAL mode. Trades a
		// small risk of losing the LAST committed transaction on a
		// power loss against a substantial throughput improvement —
		// acceptable for a personal logging app where the operator
		// can re-log a single QSO if power dies mid-commit.
		"synchronous(normal)",
	}

	// Shared-cache mode is essential for in-memory databases: without
	// it each pool connection opens a private :memory: DB, so schema
	// created on one connection is invisible to the next. Without
	// cache=shared, CI flaked under -race timing when the single
	// pooled connection was transiently dropped and replaced.
	if path == ":memory:" {
		if _, ok := opts["cache"]; !ok {
			opts["cache"] = "shared"
		}
	}

	// Stabilize key order for determinism, then assemble the DSN with
	// repeated `_pragma=` parameters (one per pragma) plus the merged
	// scalar options. url.Values would collapse repeated `_pragma`
	// keys into one because Set overwrites; build the query string
	// manually to preserve the multi-value semantic.
	q := url.Values{}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q.Set(k, opts[k])
	}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	return fmt.Sprintf("file:%s?%s", path, q.Encode()), nil
}

// ensureCtxTimeout ensures that the context has an associated timeout.
// If the context is nil, a new context with a default timeout is created.
// If the context already has a deadline, it is returned as-is.
// If the context is derived from another context, the original cancel function is called when the derived context is done.
func (s *Service) ensureCtxTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return s.withDefaultTimeout(ctx)
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return s.withDefaultTimeout(ctx)
}

// withDefaultTimeout returns a context with a default timeout if none is provided.
// If the context is nil, a new context with a default timeout is returned.
// If the context is already cancelled or has a deadline, it is returned as-is.
func (s *Service) withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := time.Duration(s.DatabaseConfig.ContextTimeout) * time.Second

	if ctx == nil {
		if timeout > 0 {
			return context.WithTimeout(context.Background(), timeout)
		}
		return context.WithCancel(context.Background())
	}
	if ctx.Err() != nil {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// checkDatabaseDir checks if the database directory exists, and if not, creates it.
func (s *Service) checkDatabaseDir(dbFilePath string) error {
	const op errors.Op = "sqlite.Service.checkDatabaseDir"

	if len(dbFilePath) == 0 {
		return errors.New(op).WithMsg(errMsgEmptyPath)
	}

	dbDir := filepath.Dir(dbFilePath)

	exists, err := utils.PathExists(dbDir)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("utils.PathExists")
	}
	if exists {
		return nil
	}

	if err = os.MkdirAll(dbDir, 0o700); err != nil {
		return errors.New(op).WithErr(err).WithMsg("os.MkdirAll")
	}

	return nil
}

// missingCoreTables checks the existence of required core tables and returns any missing names.
func (s *Service) missingCoreTables() ([]string, error) {
	const op errors.Op = "sqlite.Service.missingCoreTables"
	if s == nil {
		return nil, errors.New(op).WithMsg(errMsgNilService)
	}
	if s.handle == nil {
		return nil, errors.New(op).WithMsg(errMsgNotOpen)
	}

	if s.DatabaseConfig.Driver != SqliteDriver {
		return nil, errors.New(op).WithMsgf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}

	// Every table the running daemon touches must exist after migration —
	// verifying only logbook+qso let a partially-damaged "version current"
	// schema (missing qso_upload / qso_history / enrichment-cache tables) pass
	// startup and fail later in forwarding, session upload-status reads, audit
	// history, or enrichment refreshes (review 2026-06-19 M2). Client-side
	// schema: no api_keys table (full key stored on logbook).
	required := []string{
		"logbook",
		"qso",
		"contacted_station",
		"country",
		"qso_upload",
		"qso_history",
	}

	rows, err := s.handle.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("sqlite_master query")
	}
	defer func() { _ = rows.Close() }()

	existing := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, errors.New(op).WithErr(err).WithMsg("sqlite_master scan")
		}
		existing[name] = struct{}{}
	}
	// Standard Go SQL idiom: check iteration error after the loop.
	// rows.Next() returning false could mean "no more rows" OR "the
	// driver hit an error"; rows.Err() disambiguates.
	if err := rows.Err(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("sqlite_master iteration")
	}

	missing := make([]string, 0, len(required))
	for _, r := range required {
		if _, ok := existing[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing, nil
}

// IsUniqueConstraintError reports whether err is a sqlite UNIQUE-index
// violation, including through wrapping. Used by Insert/Update paths
// that need to translate constraint failures into typed sentinels
// (e.g. errors.ErrDuplicateName) so handlers can map them to 409
// without string-matching the driver's message.
//
// Belt and braces: try the modernc-typed *sqlite.Error first (the
// correct detection — the daemon's registered driver is
// modernc.org/sqlite, so this is the type sqlboiler / database/sql
// errors actually unwrap to), then fall back to matching the driver's
// stable "UNIQUE constraint failed" message. The fallback exists
// because sqlboiler wraps errors with `friendsofgo/errors.Wrap` and a
// future version that drops Unwrap interop would silently stop
// matching the typed branch.
//
// Exported so internal/qsoservice can call it without duplicating the
// detection logic — the second-caller threshold the comment in the
// previous local copy reserved for promotion has now been crossed
// (qsoservice.Submit uses it for race-resolution; sqlite api_context
// uses it for InsertLogbook / UpdateLogbook duplicate-name handling).
//
// SQLITE_CONSTRAINT (19) covers all constraint violations; the
// extended code SQLITE_CONSTRAINT_UNIQUE (2067) is what UNIQUE-index
// failures specifically produce. We accept either — modernc returns
// the extended code, but the primary code path is the documented
// fallback for drivers that don't expose extended codes.
func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr *moderncsqlite.Error
	if stderr.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == moderncsqlitelib.SQLITE_CONSTRAINT_UNIQUE ||
			code == moderncsqlitelib.SQLITE_CONSTRAINT
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isUniqueConstraintError is the unexported alias kept so call sites
// inside this package read naturally. New external callers should use
// the exported IsUniqueConstraintError.
func isUniqueConstraintError(err error) bool { return IsUniqueConstraintError(err) }

func isTransientPingError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Common transient indicators across drivers
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "locked") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") {
		return true
	}
	return false
}
