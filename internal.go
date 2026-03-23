package database

import (
	"context"
	"fmt"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/utils"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// getDsn returns the DSN for the database identified in the config.
func (s *Service) getDsn() (string, error) {
	const op errors.Op = "database.Service.getDsn"

	switch s.DatabaseConfig.Driver {
	case PostgresDriver:
		userInfo := url.UserPassword(s.DatabaseConfig.User, s.DatabaseConfig.Password)
		q := url.Values{}
		if s.DatabaseConfig.SSLMode != emptyString {
			q.Set("sslmode", s.DatabaseConfig.SSLMode)
		}
		hostPort := net.JoinHostPort(s.DatabaseConfig.Host, fmt.Sprintf("%d", s.DatabaseConfig.Port))
		u := &url.URL{
			Scheme:   "postgres",
			User:     userInfo,
			Host:     hostPort,
			Path:     "/" + s.DatabaseConfig.Database,
			RawQuery: q.Encode(),
		}
		return u.String(), nil

	case SqliteDriver:
		path := s.DatabaseConfig.Path
		if path == emptyString {
			return emptyString, errors.New(op).Msg(errMsgEmptyPath)
		}

		// Merge defaults if not provided
		opts := map[string]string{}
		for k, v := range s.DatabaseConfig.Options {
			opts[k] = v
		}
		// Set safe defaults only if not present
		if _, ok := opts["_busy_timeout"]; !ok {
			opts["_busy_timeout"] = "5000"
		}
		if _, ok := opts["_journal_mode"]; !ok {
			opts["_journal_mode"] = "WAL"
		}
		if _, ok := opts["_foreign_keys"]; !ok {
			opts["_foreign_keys"] = "on"
		}

		if len(opts) == 0 {
			return fmt.Sprintf("file:%s", path), nil
		}
		// Stabilize key order for determinism
		keys := make([]string, 0, len(opts))
		for k := range opts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		q := url.Values{}
		for _, k := range keys {
			q.Set(k, opts[k])
		}
		return fmt.Sprintf("file:%s?%s", path, q.Encode()), nil

	default:
		return emptyString, errors.New(op).Errorf("Unsupported database driver: %s (expected %q or %q)", s.DatabaseConfig.Driver, PostgresDriver, SqliteDriver)
	}
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
	const op errors.Op = "database.Service.checkDatabaseDir"

	if len(dbFilePath) == 0 {
		return errors.New(op).Msg(errMsgEmptyPath)
	}

	dbDir := filepath.Dir(dbFilePath)

	exists, err := utils.PathExists(dbDir)
	if err != nil {
		return errors.New(op).Errorf("utils.PathExists: %w", err)
	}
	if exists {
		return nil
	}

	if err = os.MkdirAll(dbDir, 0o700); err != nil {
		return errors.New(op).Errorf("os.MkdirAll: %w", err)
	}

	return nil
}

// missingCoreTables checks the existence of required core tables and returns any missing names.
func (s *Service) missingCoreTables() ([]string, error) {
	const op errors.Op = "database.Service.missingCoreTables"
	if s == nil {
		return nil, errors.New(op).Msg(errMsgNilService)
	}
	if s.handle == nil {
		return nil, errors.New(op).Msg(errMsgNotOpen)
	}

	var required []string
	switch s.DatabaseConfig.Driver {
	case PostgresDriver:
		required = []string{"logbook", "api_keys", "qso"}
	case SqliteDriver:
		// Client-side schema: no api_keys table (full key stored on logbook)
		required = []string{"logbook", "qso"}
	default:
		return nil, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}

	missing := make([]string, 0, len(required))

	switch s.DatabaseConfig.Driver {
	case PostgresDriver:
		rows, err := s.handle.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()`)
		if err != nil {
			return nil, errors.New(op).Errorf("information_schema.tables query: %w", err)
		}
		defer func() { _ = rows.Close() }()
		existing := map[string]struct{}{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, errors.New(op).Errorf("tables scan: %w", err)
			}
			existing[name] = struct{}{}
		}
		for _, r := range required {
			if _, ok := existing[r]; !ok {
				missing = append(missing, r)
			}
		}
	case SqliteDriver:
		rows, err := s.handle.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
		if err != nil {
			return nil, errors.New(op).Errorf("sqlite_master query: %w", err)
		}
		defer func() { _ = rows.Close() }()
		existing := map[string]struct{}{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, errors.New(op).Errorf("sqlite_master scan: %w", err)
			}
			existing[name] = struct{}{}
		}
		for _, r := range required {
			if _, ok := existing[r]; !ok {
				missing = append(missing, r)
			}
		}
	}
	return missing, nil
}

// logPostgresActivity logs a short snapshot of active Postgres queries and waits.
// It uses a short background timeout, so it's safe to call from a deadline-failed path.
func (s *Service) logPostgresActivity() {
	if s == nil || s.DatabaseConfig == nil || s.DatabaseConfig.Driver != PostgresDriver || s.handle == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	//noinspection SqlNoDataSourceInspection,SqlResolve
	rows, err := s.handle.QueryContext(ctx, `SELECT pid, usename, state, wait_event_type, wait_event, CURRENT_TIMESTAMP - query_start AS duration, query FROM pg_stat_activity WHERE state <> 'idle' ORDER BY duration DESC LIMIT 10`)
	if err != nil {
		// Internal diagnostic only (error not returned to caller)
		s.Logger.DebugWith().Str("component", "db").Str("sub", "activity").Err(err).Msg("pg_stat_activity query error")
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var pid int
		var usename, state, waitType, waitEvent, duration, query string
		if err := rows.Scan(&pid, &usename, &state, &waitType, &waitEvent, &duration, &query); err != nil {
			s.Logger.DebugWith().Str("component", "db").Str("sub", "activity").Err(err).Msg("pg_stat_activity row scan error")
			continue
		}
		s.Logger.DebugWith().Str("component", "db").Str("sub", "activity").Int("pid", pid).Str("user", usename).Str("state", state).Str("wait_type", waitType).Str("wait_event", waitEvent).Str("duration", duration).Str("query", query).Msg("active query")
	}
}

// isTransientPingError returns true if the error message indicates a transient condition worth a short retry.
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
