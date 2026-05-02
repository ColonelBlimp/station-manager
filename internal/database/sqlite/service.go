package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	_ "modernc.org/sqlite"
)

const ServiceName = types.SqliteServiceName

type Service struct {
	ConfigService  *config.Service  `di.inject:"configservice"`
	LoggerService  *logging.Service `di.inject:"loggingservice"`
	DatabaseConfig *types.DatastoreConfig

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
			initErr = errors.New(op).WithMsg("logger service has not been set/injected")
			return
		}

		if s.ConfigService == nil {
			initErr = errors.New(op).WithMsg("application config has not been set/injected")
			return
		}

		dbCfg, err := s.ConfigService.DatastoreConfig()
		if err != nil {
			initErr = errors.New(op).WithErr(err)
			return
		}

		if err = validateConfig(&dbCfg); err != nil {
			initErr = errors.New(op).WithErr(err).WithMsg("Invalid database config")
			return
		}
		s.DatabaseConfig = &dbCfg

		if s.DatabaseConfig.Driver == SqliteDriver {
			// Ensure the database directory exists
			if err = s.checkDatabaseDir(s.DatabaseConfig.Path); err != nil {
				initErr = errors.New(op).WithErr(err)
				return
			}
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
		return errors.New(op).WithMsg(errMsgNotInitialized)
	}

	// Quick pre-check to see if the database is already open.
	if s.isOpen.Load() {
		return errors.New(op).WithMsg(errMsgAlreadyOpen)
	}

	// Outside the mutex as its config is read-only
	dsn, err := s.getDsn()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg(errMsgDsnBuildError)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under lock to avoid TOCTOU
	if s.isOpen.Load() {
		return errors.New(op).WithMsg(errMsgAlreadyOpen)
	}

	db, err := sql.Open(s.DatabaseConfig.Driver, dsn)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg(errMsgConnFailed)
	}

	db.SetMaxOpenConns(s.DatabaseConfig.MaxOpenConns)
	db.SetMaxIdleConns(s.DatabaseConfig.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(s.DatabaseConfig.ConnMaxLifetime) * time.Minute)
	db.SetConnMaxIdleTime(time.Duration(s.DatabaseConfig.ConnMaxIdleTime) * time.Minute)

	ctx, cancel := s.withDefaultTimeout(context.Background())
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg(errMsgPingFailed)
	}

	// Ensure SQLite enforces foreign keys on this connection. Some drivers may ignore DSN params,
	// so execute the PRAGMA explicitly per-connection. If this fails, close the DB and return error.
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("failed to enable sqlite foreign_keys PRAGMA")
	}
	// Reinforce busy timeout and WAL journal mode explicitly (DSN may not always apply reliably across drivers)
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("failed to set sqlite busy_timeout PRAGMA")
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return errors.New(op).WithErr(err).WithMsg("failed to set sqlite journal_mode WAL")
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
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under lock - TOCTOU
	if !s.isOpen.Load() {
		return nil // Idempotent
	}

	if err := s.handle.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg(errMsgFailedClose)
	}

	s.handle = nil
	s.isOpen.Store(false)

	// Reset Initialize's guard so a subsequent Initialize()->Open() cycle
	// actually re-runs. Without this the next Initialize is a no-op and
	// any config change between cycles is silently ignored — a trap for
	// a future config-reload path (SIGHUP etc.). Safe against races
	// because Close holds s.mu.Lock() and every non-get path takes the
	// same mutex; if that lock discipline ever changes this reassignment
	// needs revisiting.
	s.initOnce = sync.Once{}
	s.isInitialized.Store(false)

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
	for range 2 {
		ctx, cancel := s.withDefaultTimeout(context.Background())
		err := h.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientPingError(err) {
			return errors.New(op).WithErr(err).WithMsg(errMsgPingFailed)
		}
		// Small backoff before retrying transient failure
		time.Sleep(pingRetryBackoff)
	}

	return errors.New(op).WithErr(lastErr).WithMsg(errMsgPingFailed)
}

// Migrate runs the database migrations.
func (s *Service) Migrate() error {
	const op errors.Op = "sqlite.Service.Migrate"

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.handle == nil || !s.isOpen.Load() {
		return errors.New(op).WithMsg(errMsgNotOpen)
	}

	err := s.doMigrations()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg(errMsgMigrateFailed)
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
			return nil, nil, errors.New(op).WithErr(err).WithMsg("Transaction context timed out.")
		}
		return nil, nil, errors.New(op).WithErr(err).WithMsg("creating new transaction")
	}

	return tx, cancel, nil
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
