package database

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adapters"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging" // added for structured logging
	"github.com/ColonelBlimp/station-manager/internal/types"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Service struct {
	ConfigService  *config.Service  `di.inject:"configservice"`
	Logger         *logging.Service `di.inject:"loggingservice"`
	DatabaseConfig *types.DatastoreConfig
	handle         *sql.DB

	mu            sync.RWMutex
	isInitialized atomic.Bool
	isOpen        atomic.Bool

	adapterToModel   *adapters.Adapter
	adapterFromModel *adapters.Adapter
}

// Initialize initializes the database service. No constructor is provided as this service is to be
// initialized within an IOC/DI container.
func (s *Service) Initialize() error {
	const op errors.Op = "database.Service.Initialize"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if s.isInitialized.Load() {
		return nil // Exit gracefully
	}

	if s.ConfigService == nil {
		return errors.New(op).Msg(errMsgAppConfigNil)
	}

	// Logger must be injected
	if s.Logger == nil {
		return errors.New(op).Msg(errMsgLoggerNil)
	}

	dbCfg, err := s.ConfigService.DatastoreConfig()
	if err != nil {
		return errors.New(op).Err(err)
	}

	if err = validateConfig(&dbCfg); err != nil {
		return errors.New(op).Err(err).Msg("Invalid database config")
	}
	s.DatabaseConfig = &dbCfg

	if err = s.checkDatabaseDir(s.DatabaseConfig.Path); err != nil {
		return errors.New(op).Err(err)
	}

	s.isInitialized.Store(true)

	return nil
}

// Open opens the database connection.
func (s *Service) Open() error {
	const op errors.Op = "database.Service.Open"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	// Has the service been initialized?
	if !s.isInitialized.Load() {
		return errors.New(op).Msg(errMsgNotInitialized)
	}

	// Quick pre-check to see if the database is already open.
	if s.isOpen.Load() {
		return errors.New(op).Msg(errMsgAlreadyOpen)
	}

	// Outside the mutex as its config is read-only
	dsn, err := s.getDsn()
	if err != nil {
		return errors.New(op).Err(err).Msg(errMsgDsnBuildError)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under lock to avoid TOCTOU
	if s.isOpen.Load() {
		return errors.New(op).Msg(errMsgAlreadyOpen)
	}

	db, err := sql.Open(s.DatabaseConfig.Driver, dsn)
	if err != nil {
		return errors.New(op).Err(err).Msg(errMsgConnFailed)
	}

	db.SetMaxOpenConns(s.DatabaseConfig.MaxOpenConns)
	db.SetMaxIdleConns(s.DatabaseConfig.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(s.DatabaseConfig.ConnMaxLifetime) * time.Minute)
	db.SetConnMaxIdleTime(time.Duration(s.DatabaseConfig.ConnMaxIdleTime) * time.Minute)

	ctx, cancel := s.withDefaultTimeout(context.Background())
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return errors.New(op).Err(err).Msg(errMsgPingFailed)
	}

	// Ensure SQLite enforces foreign keys on this connection. Some drivers may ignore DSN params,
	// so execute the PRAGMA explicitly per-connection. If this fails, close the DB and return error.
	if s.DatabaseConfig.Driver == SqliteDriver {
		if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
			_ = db.Close()
			return errors.New(op).Err(err).Msg("failed to enable sqlite foreign_keys PRAGMA")
		}
		// Reinforce busy timeout and WAL journal mode explicitly (DSN may not always apply reliably across drivers)
		if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
			_ = db.Close()
			return errors.New(op).Err(err).Msg("failed to set sqlite busy_timeout PRAGMA")
		}
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return errors.New(op).Err(err).Msg("failed to set sqlite journal_mode WAL")
		}
	}

	s.handle = db
	s.isOpen.Store(true)

	return nil
}

// Close closes the database connection.
func (s *Service) Close() error {
	const op errors.Op = "database.Service.Close"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	// Quick pre-check
	if !s.isOpen.Load() {
		return nil // Idempotent
		// return errors.New(op).Msg(errMsgNotOpen)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under lock - TOCTOU
	if !s.isOpen.Load() {
		return nil // Idempotent
		// return errors.New(op).Msg(errMsgNotOpen)
	}

	if err := s.handle.Close(); err != nil {
		return errors.New(op).Err(err).Msg(errMsgFailedClose)
	}

	s.handle = nil
	s.isOpen.Store(false)

	return nil
}

// Ping pings the database connection.
func (s *Service) Ping() error {
	const op errors.Op = "database.Service.Ping"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	// Snapshot state under read lock to minimize lock hold time during network call.
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()

	if h == nil || !isOpen {
		return errors.New(op).Msg(errMsgNotOpen)
	}

	var lastErr error
	// Up to 2 attempts for transient failures (e.g., brief network hiccup, SQLITE_BUSY)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := s.withDefaultTimeout(context.Background())
		err := h.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientPingError(err) {
			return errors.New(op).Err(err).Msg(errMsgPingFailed)
		}
		// Small backoff before retrying transient failure
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New(op).Err(lastErr).Msg(errMsgPingFailed)
}

// Migrate runs the database migrations.
func (s *Service) Migrate() error {
	const op errors.Op = "database.Service.Migrate"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.handle == nil || !s.isOpen.Load() {
		return errors.New(op).Msg(errMsgNotOpen)
	}

	err := s.doMigrations()
	if err != nil {
		return errors.New(op).Err(err).Msg(errMsgMigrateFailed)
	}

	return nil
}

// BeginTxContext starts a new transaction.
func (s *Service) BeginTxContext(ctx context.Context) (*sql.Tx, context.CancelFunc, error) {
	const op errors.Op = "database.Service.BeginTxContext"
	if s == nil {
		return nil, nil, errors.New(op).Msg(errMsgNilService)
	}

	// Snapshot handle & open state under read lock (mirrors ExecContext/QueryContext pattern)
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()

	if h == nil || !isOpen {
		return nil, nil, errors.New(op).Msg(errMsgNotOpen)
	}

	_, hasDeadline := ctx.Deadline()
	var txCtx context.Context
	var cancel context.CancelFunc
	if !hasDeadline {
		txCtx, cancel = context.WithTimeout(ctx, time.Duration(s.DatabaseConfig.TransactionContextTimeout)*time.Second)
	} else {
		txCtx = ctx
		cancel = func() {} // No-op cancel when caller supplied deadline
	}

	tx, err := h.BeginTx(txCtx, nil)
	if err != nil {
		cancel()
		if stderr.Is(err, context.DeadlineExceeded) {
			return nil, nil, errors.New(op).Err(err).Msg("Transaction context timed out.")
		}
		return nil, nil, errors.New(op).Errorf("creating new transaction: %w", err)
	}

	// If using Postgres, set a local statement_timeout so long-running queries inside the
	// transaction are bounded by the transaction timeout. This ensures the DB will cancel
	// server-side statements (when supported) rather than leaving the client waiting.
	if s.DatabaseConfig != nil && s.DatabaseConfig.Driver == PostgresDriver {
		// Prefer the actual remaining deadline on txCtx if present so server-side timeout
		// matches the client-side remaining time. Fallback to the configured value.
		var ms int
		if dl, ok := txCtx.Deadline(); ok {
			remain := time.Until(dl)
			if remain <= 0 {
				ms = 1
			} else {
				ms = int(remain.Milliseconds())
			}
		} else if to := s.DatabaseConfig.TransactionContextTimeout; to > 0 {
			ms = int(to) * 1000
		} else {
			ms = 0
		}
		if ms > 0 {
			if _, err := tx.ExecContext(txCtx, fmt.Sprintf("SET LOCAL statement_timeout = %d", ms)); err != nil {
				_ = tx.Rollback()
				cancel()
				if stderr.Is(err, context.DeadlineExceeded) {
					return nil, nil, errors.New(op).Err(err).Msg("Transaction context timed out while setting statement_timeout.")
				}
				return nil, nil, errors.New(op).Errorf("setting local statement_timeout: %w", err)
			}
			// Also set a lock_timeout to a fraction of the statement timeout so waits on locks fail sooner.
			lockMs := ms / 2
			if lockMs < 100 {
				lockMs = 100
			}
			if _, err := tx.ExecContext(txCtx, fmt.Sprintf("SET LOCAL lock_timeout = %d", lockMs)); err != nil {
				_ = tx.Rollback()
				cancel()
				return nil, nil, errors.New(op).Errorf("setting local lock_timeout: %w", err)
			}
		}
	}

	return tx, cancel, nil
}

func (s *Service) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	const op errors.Op = "database.Service.ExecContext"
	if s == nil {
		return nil, errors.New(op).Msg(errMsgNilService)
	}

	// Holding s.mu.RLock() while performing s.handle.ExecContext(...) means the read lock is held for the duration
	// of the exec. This can block Close()/Migrate(), which need the write lock. So, we copy the *sql.DB handle under
	// the lock, release the lock, then call ExecContext so long-running ops don’t hold the lock.
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()

	if h == nil || !isOpen {
		return nil, errors.New(op).Msg(errMsgNotOpen)
	}

	var cancel context.CancelFunc
	_, ok := ctx.Deadline()
	if !ok {
		ctx, cancel = s.withDefaultTimeout(ctx)
	} else {
		cancel = func() {} // No-op
	}
	defer cancel()

	res, err := h.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, errors.New(op).Errorf("s.handle.ExecContext: %w", err)
	}

	return res, nil
}

func (s *Service) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	const op errors.Op = "database.Service.QueryContext"
	if s == nil {
		return nil, errors.New(op).Msg(errMsgNilService)
	}

	// Holding s.mu.RLock() while performing s.handle.ExecContext(...) means the read lock is held for the duration
	// of the exec. This can block Close()/Migrate(), which need the write lock. So, we copy the *sql.DB handle under
	// the lock, release the lock, then call ExecContext so long-running ops don’t hold the lock.
	s.mu.RLock()
	h := s.handle
	isOpen := s.isOpen.Load()
	s.mu.RUnlock()

	if h == nil || !isOpen {
		return nil, errors.New(op).Msg(errMsgNotOpen)
	}

	// Note: We do NOT defer cancel() here because the returned *sql.Rows needs
	// the context to remain valid while the caller iterates over rows.
	// The caller must ensure rows.Close() is called to release resources.
	// The timeout context (if created) will be automatically cleaned up after the timeout expires.
	_, ok := ctx.Deadline()
	if !ok {
		ctx, _ = s.withDefaultTimeout(ctx)
	}

	res, err := h.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.New(op).Errorf("s.handle.QueryContext: %w", err)
	}

	return res, nil
}

func (s *Service) LogStats(prefix string) {
	// Snapshot handle under read lock to avoid races with Close()/Open()
	if s == nil {
		return
	}
	s.mu.RLock()
	h := s.handle
	s.mu.RUnlock()
	if h == nil {
		return
	}
	st := h.Stats()
	// Structured, non-error diagnostic (not returned to caller)
	s.Logger.DebugWith().Str("component", "db").Str("metric", "pool").Str("phase", prefix).Int("open", st.OpenConnections).Int("in_use", st.InUse).Int("idle", st.Idle).Int64("wait_count", st.WaitCount).Dur("wait_duration", st.WaitDuration).Int("max_open", st.MaxOpenConnections).Msg("db pool stats")
}
