package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
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

	// migrationSets restricts which schema domains this connection migrates
	// and verifies. Empty means "all domains in one connection" (the default
	// single-DB shape, used by every test); the file-split wiring sets a
	// single domain per connection via SetMigrationSets.
	migrationSets []string

	handle *sql.DB

	mu            sync.RWMutex
	isInitialized atomic.Bool
	isOpen        atomic.Bool
}

// SetMigrationSets restricts this Service to the named migration sets
// (MigrationSetLog / MigrationSetReference), affecting both which migrations
// run and which core tables are verified. Call before Migrate(). With no sets
// configured the Service runs every domain in one connection (the default).
func (s *Service) SetMigrationSets(sets ...string) { s.migrationSets = sets }

// SetDatabasePath overrides the on-disk path resolved from config — used to
// point a second connection at reference.db, sharing the log DB's directory.
// Call after Initialize and before Open; a no-op once the connection is open.
func (s *Service) SetDatabasePath(p string) {
	if s.isOpen.Load() || s.DatabaseConfig == nil {
		return
	}
	s.DatabaseConfig.Path = p
}

// Initialize initializes the database service. No constructor is provided as this service is to be
// initialized within an IOC/DI container.
// Initialize validates dependencies + config and prepares the data directory.
// Idempotent and RETRYABLE: a failure (missing logger/config, invalid config,
// directory error) leaves the service uninitialized so a later call — after the
// dependency/config problem is fixed — runs the body again and can succeed.
// (The previous sync.Once consumed the guard on the first failure, so a retry
// silently returned nil while the service stayed uninitialized and Open kept
// failing.) Guarded by s.mu so concurrent calls serialise; isInitialized is set
// ONLY after every step succeeds.
func (s *Service) Initialize() error {
	const op errors.Op = "sqlite.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-check under the lock: another goroutine may have initialised between
	// the lock-free fast path above and acquiring the lock.
	if s.isInitialized.Load() {
		return nil
	}

	if s.LoggerService == nil {
		return errors.New(op).WithMsg("logger service has not been set/injected")
	}
	if s.ConfigService == nil {
		return errors.New(op).WithMsg("application config has not been set/injected")
	}

	dbCfg, err := s.ConfigService.DatastoreConfig()
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	if err = validateConfig(&dbCfg); err != nil {
		return errors.New(op).WithErr(err).WithMsg("Invalid database config")
	}

	if dbCfg.Driver == SqliteDriver {
		// Ensure the database directory exists.
		if err = s.checkDatabaseDir(dbCfg.Path); err != nil {
			return errors.New(op).WithErr(err)
		}
	}

	// All steps succeeded — commit the validated config and latch initialised.
	s.DatabaseConfig = &dbCfg
	s.isInitialized.Store(true)
	return nil
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
	if _, err = db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeoutMS)); err != nil {
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

	// `PRAGMA optimize` is the SQLite-recommended way to keep planner
	// statistics fresh: it cheaply re-analyses tables that have changed
	// significantly since the last ANALYZE, skipping ones that haven't.
	// Run on shutdown so each daemon lifecycle leaves the DB primed for
	// the next startup. Failure is non-fatal — we're on the way out
	// anyway, and the next Migrate's ANALYZE is the backstop.
	ctx, cancel := s.withDefaultTimeout(context.Background())
	if _, err := s.handle.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		s.LoggerService.WarnWith().
			Err(err).
			Msg("PRAGMA optimize failed on close; planner stats may be slightly stale next startup")
	}
	cancel()

	if err := s.handle.Close(); err != nil {
		return errors.New(op).WithErr(err).WithMsg(errMsgFailedClose)
	}

	s.handle = nil
	s.isOpen.Store(false)

	// Clear the initialised flag so a subsequent Initialize()->Open() cycle
	// actually re-runs (Initialize is mutex-guarded + re-checkable, so simply
	// dropping the flag is enough — there is no longer a sync.Once to reset).
	// Without this the next Initialize is a no-op and any config change between
	// cycles is silently ignored — a trap for a future config-reload path
	// (SIGHUP etc.). Safe against races: Close holds s.mu.Lock(), the same lock
	// Initialize takes.
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
	// Up to pingMaxAttempts for transient failures (e.g., brief network hiccup, SQLITE_BUSY)
	for range pingMaxAttempts {
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

	// ANALYZE populates sqlite_stat1 so the query planner picks indexes
	// based on actual selectivity rather than default heuristics. Without
	// it the planner falls back to picking by index column count, which
	// for a single-logbook install meant `idx_qso_logbook_id` (which
	// matches 100% of rows and is therefore useless) was preferred over
	// `idx_qso_active_call` (one-row selectivity). Stress run 2026-05-09
	// confirmed: 100ms+ contact-history latency from a 125k-row scan
	// dropped to <1ms once stats were populated. Stats persist in the
	// database file, so this only runs at meaningful cost the first
	// time on a freshly populated DB; subsequent restarts are
	// near-instant.
	//
	// Failure is non-fatal — a daemon that refused to start because
	// ANALYZE failed would be worse than one with stale stats. Log a
	// warning and continue.
	ctx, cancel := s.withDefaultTimeout(context.Background())
	defer cancel()
	if _, err := s.handle.ExecContext(ctx, "ANALYZE"); err != nil {
		s.LoggerService.WarnWith().
			Err(err).
			Msg("ANALYZE failed after migrate; planner statistics may be stale (queries will work, just slower)")
	}

	return nil
}

// BeginTxContext starts a new transaction.
func (s *Service) BeginTxContext(ctx context.Context) (*sql.Tx, context.CancelFunc, error) {
	const op errors.Op = "sqlite.Service.BeginTxContext"

	// Tolerate a nil ctx like the package's ensureCtxTimeout-based methods do —
	// ctx.Deadline() below would otherwise panic on nil.
	if ctx == nil {
		ctx = context.Background()
	}

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
