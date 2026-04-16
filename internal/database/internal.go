package database

import (
	"context"
	"fmt"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
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
		return emptyString, errors.New(op).Msgf("Unsupported database driver: %s (expected %q or %q)", s.DatabaseConfig.Driver, PostgresDriver, SqliteDriver)
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
		return errors.New(op).Msgf("utils.PathExists: %w", err)
	}
	if exists {
		return nil
	}

	if err = os.MkdirAll(dbDir, 0o700); err != nil {
		return errors.New(op).Msgf("os.MkdirAll: %w", err)
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
		return nil, errors.New(op).Msgf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}

	missing := make([]string, 0, len(required))

	switch s.DatabaseConfig.Driver {
	case PostgresDriver:
		rows, err := s.handle.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()`)
		if err != nil {
			return nil, errors.New(op).Msgf("information_schema.tables query: %w", err)
		}
		defer func() { _ = rows.Close() }()
		existing := map[string]struct{}{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, errors.New(op).Msgf("tables scan: %w", err)
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
			return nil, errors.New(op).Msgf("sqlite_master query: %w", err)
		}
		defer func() { _ = rows.Close() }()
		existing := map[string]struct{}{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, errors.New(op).Msgf("sqlite_master scan: %w", err)
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
