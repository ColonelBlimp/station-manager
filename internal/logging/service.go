package logging

import (
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	"github.com/rs/zerolog"
	"go.uber.org/atomic"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Service is the primary logging facility for the application.
//
// It wraps rs/zerolog with:
//   - one-time initialization and graceful shutdown
//   - concurrency-safe event builders
//   - file rotation via lumberjack
//   - optional console writer with configurable color/time format
//   - error history enrichment for Err/AnErr calls
//
// A Service must be initialized via Initialize() before use and closed with Close().
// It is safe for concurrent use by multiple goroutines.
type Service struct {
	WorkingDir        string          `di.inject:"workingdir"`
	ConfigService     *config.Service `di.inject:"configservice"`
	LoggingConfig     *types.LoggingConfig
	fileWriter        *lumberjack.Logger
	logger            atomic.Pointer[zerolog.Logger]
	isInitialized     atomic.Bool
	initOnce          sync.Once
	initErr           error
	mu                sync.RWMutex
	activeOps         atomic.Int32 // Track active logging operations
	wg                sync.WaitGroup
	activeOpLocations map[string]int // Debug: Track where active operations were created
}

// Initialize prepares the Service for use: it validates configuration, ensures
// the log directory exists, sets up file/console writers, sets the log level,
// and builds the zerolog logger with any requested timestamp or caller info.
// Initialize is safe to call multiple times; subsequent calls are no-ops.
func (s *Service) Initialize() error {
	const op errors.Op = "logging.Service.Initialize"
	if s == nil {
		return errors.New(op).Msg(errMsgNilService)
	}

	if s.ConfigService == nil {
		return errors.New(op).Msg(errMsgAppCfgNotSet)
	}

	s.initOnce.Do(func() {
		loggingCfg, cfgErr := s.ConfigService.LoggingConfig()
		if cfgErr != nil {
			s.initErr = errors.New(op).Errorf("s.AppConfig.LoggingConfig: %w", cfgErr)
			return
		}

		if cfgErr = validateConfig(&loggingCfg); cfgErr != nil {
			s.initErr = errors.New(op).Errorf("validateConfig: %w", cfgErr)
			return
		}
		s.LoggingConfig = &loggingCfg

		if s.WorkingDir == emptyString {
			exeDir, pathErr := utils.AbsDirPathForExecutable()
			if pathErr != nil {
				s.initErr = errors.New(op).Errorf("utils.AbsDirPathForExecutable: %w", pathErr)
				return
			}
			s.WorkingDir = exeDir
		}

		loggingDir := filepath.Join(s.WorkingDir, s.LoggingConfig.RelLogFileDir)
		exists, existsErr := utils.PathExists(loggingDir)
		if existsErr != nil {
			s.initErr = errors.New(op).Errorf("utils.PathExists: %w", existsErr)
			return
		}

		if !exists {
			if mdErr := os.MkdirAll(loggingDir, 0750); mdErr != nil {
				s.initErr = errors.New(op).Errorf("os.MkdirAll: %w", mdErr)
				return
			}
		}

		exeName, exeErr := utils.ExecName(true)
		if exeErr != nil {
			s.initErr = errors.New(op).Errorf("utils.ExecName: %w", exeErr)
			return
		}

		mw := io.MultiWriter(s.initializeWriters(exeName)...)
		logger := zerolog.New(mw).With().Logger()

		level, levelErr := parseLevel(s.LoggingConfig.Level)
		if levelErr != nil {
			s.initErr = errors.New(op).Errorf("parseLevel: %w", levelErr)
			return
		}
		logger = logger.Level(level)

		if s.LoggingConfig.WithTimestamp {
			logger = logger.With().Timestamp().Logger()
		}

		if s.LoggingConfig.SkipFrameCount > 0 {
			logger = logger.With().CallerWithSkipFrameCount(s.LoggingConfig.SkipFrameCount).Logger()
		}

		// Store logger atomically
		s.logger.Store(&logger)

		s.isInitialized.Store(true)
	})

	return s.initErr
}

// Close stops accepting new log operations, waits for in-flight logging to
// finish up to a configured timeout, optionally warns on timeout, and closes
// any open file writer. It is safe to call multiple times.
func (s *Service) Close() error {
	const op errors.Op = "logging.Service.Close"
	if s == nil {
		return nil
	}
	if !s.isInitialized.Load() {
		return nil
	}

	// Lock to prevent concurrent logging operations during close
	s.mu.Lock()

	// Double-check after acquiring lock
	if !s.isInitialized.Load() {
		s.mu.Unlock()
		return nil
	}

	// Capture logger for potential warning before marking uninitialized
	logger := s.logger.Load()

	// Mark as uninitialized first to prevent new operations
	s.isInitialized.Store(false)
	s.logger.Store(nil)
	s.mu.Unlock()

	// Determine timeout (default 100ms if not configured)
	timeoutMS := 100
	warnOnTimeout := false
	if s.LoggingConfig != nil {
		if s.LoggingConfig.ShutdownTimeoutMS > 0 {
			timeoutMS = s.LoggingConfig.ShutdownTimeoutMS
		}
		warnOnTimeout = s.LoggingConfig.ShutdownTimeoutWarning
	}

	// Wait for active logging operations to complete using WaitGroup with timeout
	if waitTimeout(&s.wg, time.Duration(timeoutMS)*time.Millisecond) {
		// Timed out
		if warnOnTimeout && logger != nil {
			activeOps := s.activeOps.Load()

			// Capture location info for debugging
			s.mu.Lock()
			locations := make(map[string]int)
			for k, v := range s.activeOpLocations {
				locations[k] = v
			}
			s.mu.Unlock()

			event := logger.Warn().
				Int32("active_operations", activeOps).
				Int("timeout_ms", timeoutMS)

			// Add location info if available
			if len(locations) > 0 {
				event = event.Interface("operation_locations", locations)
			}

			event.Msg("Logger shutdown timeout exceeded, forcing close with active operations")

			// Force-drain the WaitGroup to prevent indefinite blocking
			// This handles orphaned log operations that never called Msg()/Send()
			for i := int32(0); i < activeOps; i++ {
				s.activeOps.Add(-1)
				s.wg.Done()
			}
		}
	}

	// Close the file writer if it exists
	// fileWriter is only accessed here and during initialization (protected by sync.Once)
	// The activeOps counter ensures no writes are in progress
	s.mu.Lock()
	fileWriter := s.fileWriter
	s.fileWriter = nil
	s.mu.Unlock()

	if fileWriter != nil {
		if err := fileWriter.Close(); err != nil {
			return errors.New(op).Errorf("fileWriter.Close: %w", err)
		}
	}

	return nil
}

// waitTimeout waits for the waitgroup for the specified duration.
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

// Wait blocks until all active logging operations have completed.
// This is useful for ensuring all logs are written before program exit.
func (s *Service) Wait() {
	if s == nil || !s.isInitialized.Load() {
		return
	}
	s.wg.Wait()
}

// ActiveOperations returns the current number of active logging operations.
// This is primarily used by shutdown logic to wait for in-flight operations to complete.
func (s *Service) ActiveOperations() int32 {
	if s == nil {
		return 0
	}
	return s.activeOps.Load()
}

// TraceWith returns a LogEvent for structured Trace-level logging.
// Trace is the most verbose logging level, typically used for very detailed debugging.
func (s *Service) TraceWith() LogEvent {
	return logEventBuilder(s, zerolog.TraceLevel)
}

// DebugWith returns a LogEvent for structured Debug-level logging.
func (s *Service) DebugWith() LogEvent {
	return logEventBuilder(s, zerolog.DebugLevel)
}

// InfoWith returns a LogEvent for structured Info-level logging.
// Use this for queryable, structured logs instead of Info/Infof.
// Example: logger.InfoWith().Str("user_id", id).Int("count", 5).Msg("User processed")
func (s *Service) InfoWith() LogEvent {
	return logEventBuilder(s, zerolog.InfoLevel)
}

// WarnWith returns a LogEvent for structured Warn-level logging.
func (s *Service) WarnWith() LogEvent {
	return logEventBuilder(s, zerolog.WarnLevel)
}

// ErrorWith returns a LogEvent for structured Error-level logging.
// Example: logger.ErrorWith().Err(err).Str("operation", "database").Msg("Query failed")
func (s *Service) ErrorWith() LogEvent {
	return logEventBuilder(s, zerolog.ErrorLevel)
}

// FatalWith returns a LogEvent for structured Fatal-level logging.
// The program will exit after the log is written.
func (s *Service) FatalWith() LogEvent {
	return logEventBuilder(s, zerolog.FatalLevel)
}

// PanicWith returns a LogEvent for structured Panic-level logging.
// The program will panic after the log is written.
func (s *Service) PanicWith() LogEvent {
	return logEventBuilder(s, zerolog.PanicLevel)
}

// With returns a LogContext for creating a child logger with pre-populated fields.
// Example: reqLogger := logger.With().Str("request_id", id).Logger()
// Returns a no-op context if the service is not initialized.
func (s *Service) With() LogContext {
	if s == nil || !s.isInitialized.Load() {
		return &noopLogContext{}
	}

	// Acquire read lock to prevent Close() from running
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Double-check after acquiring lock
	if !s.isInitialized.Load() {
		return &noopLogContext{}
	}

	logger := s.logger.Load()
	if logger == nil {
		return &noopLogContext{}
	}
	return &logContext{
		context: logger.With(),
		service: s,
	}
}
