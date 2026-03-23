package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	_ "modernc.org/sqlite"
)

const ServiceName = types.SqliteServiceName

type Service struct {
	ConfigService  *config.Service  `di.inject:"configservice"`
	LoggerService  *logging.Service `di.inject:"loggingservice"`
	DatabaseConfig *types.DatastoreConfig

	requiredCfgs types.RequiredConfigs

	handle *sql.DB

	mu            sync.RWMutex
	isInitialized atomic.Bool
	isOpen        atomic.Bool
	initOnce      sync.Once
}

// Initialize initializes the database service. No constructor is provided as this service is to be
// initialized within an IOC/DI container.
func (s *Service) Initialize() error {
	const op errors.Op = "sqlite.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}

	var initErr error
	s.initOnce.Do(func() {
		if s.LoggerService == nil {
			initErr = errors.New(op).Msg("logger service has not been set/injected")
			return
		}

		if s.ConfigService == nil {
			initErr = errors.New(op).Msg("application config has not been set/injected")
			return
		}

		dbCfg, err := s.ConfigService.DatastoreConfig()
		if err != nil {
			initErr = errors.New(op).Err(err)
			return
		}

		if err = validateConfig(&dbCfg); err != nil {
			initErr = errors.New(op).Err(err).Msg("Invalid database config")
			return
		}
		s.DatabaseConfig = &dbCfg

		if s.DatabaseConfig.Driver == SqliteDriver {
			// Ensure the database directory exists
			if err = s.checkDatabaseDir(s.DatabaseConfig.Path); err != nil {
				initErr = errors.New(op).Err(err)
				return
			}
		}

		if s.requiredCfgs, err = s.ConfigService.RequiredConfigs(); err != nil {
			initErr = errors.New(op).Err(err)
			return
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Open opens the database connection.
func (s *Service) Open() error {
	const op errors.Op = "sqlite.Service.Open"

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
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return errors.New(op).Err(err).Msg("failed to enable sqlite foreign_keys PRAGMA")
	}
	// Reinforce busy timeout and WAL journal mode explicitly (DSN may not always apply reliably across drivers)
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return errors.New(op).Err(err).Msg("failed to set sqlite busy_timeout PRAGMA")
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return errors.New(op).Err(err).Msg("failed to set sqlite journal_mode WAL")
	}

	s.handle = db

	s.isOpen.Store(true)

	return nil
}

// Close closes the database connection.
func (s *Service) Close() error {
	const op errors.Op = "sqlite.Service.Close"

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
	const op errors.Op = "sqlite.Service.Ping"

	// Snapshot state under read lock to minimize lock hold time during network call.
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
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
		time.Sleep(pingRetryBackoff)
	}

	return errors.New(op).Err(lastErr).Msg(errMsgPingFailed)
}

// Migrate runs the database migrations.
func (s *Service) Migrate() error {
	const op errors.Op = "sqlite.Service.Migrate"

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
	const op errors.Op = "sqlite.Service.BeginTxContext"

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, nil, err
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

	return tx, cancel, nil
}

func (s *Service) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	const op errors.Op = "sqlite.Service.ExecContext"

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
	const op errors.Op = "sqlite.Service.QueryContext"

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
	s.mu.RLock()
	h := s.handle
	s.mu.RUnlock()
	if h == nil {
		return
	}
	st := h.Stats()
	// Structured, non-error diagnostic (not returned to caller)
	s.LoggerService.DebugWith().Str("component", "db").Str("metric", "pool").Str("phase", prefix).Int("open", st.OpenConnections).Int("in_use", st.InUse).Int("idle", st.Idle).Int64("wait_count", st.WaitCount).Dur("wait_duration", st.WaitDuration).Int("max_open", st.MaxOpenConnections).Msg("db pool stats")
}
