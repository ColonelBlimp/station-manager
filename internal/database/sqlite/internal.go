package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/utils"
)

func (s *Service) getOpenHandle(op errors.Op) (*sql.DB, error) {
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()
	if h == nil || !isOpen {
		return nil, errors.New(op).Msg(errMsgNotOpen)
	}
	return h, nil
}

// getDsn returns the DSN for the SQLite database identified in the config.
func (s *Service) getDsn() (string, error) {
	const op errors.Op = "sqlite.Service.getDsn"

	if s.DatabaseConfig.Driver != SqliteDriver {
		return emptyString, errors.New(op).Errorf("Unsupported database driver: %s (expected %q)", s.DatabaseConfig.Driver, SqliteDriver)
	}

	path := s.DatabaseConfig.Path
	if path == emptyString {
		return emptyString, errors.New(op).Msg(errMsgEmptyPath)
	}

	s.LoggerService.InfoWith().Str("path", path).Msg("Using sqlite database file")

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
	const op errors.Op = "sqlite.Service.missingCoreTables"
	if s == nil {
		return nil, errors.New(op).Msg(errMsgNilService)
	}
	if s.handle == nil {
		return nil, errors.New(op).Msg(errMsgNotOpen)
	}

	if s.DatabaseConfig.Driver != SqliteDriver {
		return nil, errors.New(op).Errorf("Unsupported database driver: %s", s.DatabaseConfig.Driver)
	}

	// Client-side schema: no api_keys table (full key stored on logbook)
	required := []string{"logbook", "qso"}

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

	missing := make([]string, 0, len(required))
	for _, r := range required {
		if _, ok := existing[r]; !ok {
			missing = append(missing, r)
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
