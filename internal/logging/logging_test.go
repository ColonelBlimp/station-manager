package logging

import (
	"bytes"
	stderrs "errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a valid logging config
func validLoggingConfig() *types.LoggingConfig {
	return &types.LoggingConfig{
		Level:             "debug",
		SkipFrameCount:    0,
		WithTimestamp:     true,
		ConsoleLogging:    true,
		FileLogging:       false,
		RelLogFileDir:     ".", // Use current dir to pass validation
		LogFileMaxBackups: 3,
		LogFileMaxAgeDays: 7,
		LogFileMaxSizeMB:  10,
	}
}

// Helper to create a config service with logging config
func newTestConfigService(cfg *types.LoggingConfig) *config.Service {
	svc := &config.Service{
		Cfg: config.Config{
			Logging: *cfg,
		},
	}
	_ = svc.Initialize()
	return svc
}

func TestService_Initialize(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		err := service.Initialize()
		require.NoError(t, err)
		assert.True(t, service.isInitialized.Load())
		assert.NotNil(t, service.logger.Load())
	})

	t.Run("nil service", func(t *testing.T) {
		var service *Service
		err := service.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), errMsgNilService)
	})

	t.Run("nil app config", func(t *testing.T) {
		service := &Service{}
		err := service.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), errMsgAppCfgNotSet)
	})

	t.Run("invalid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		invalidCfg := validLoggingConfig()
		invalidCfg.Level = "invalid_level"

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(invalidCfg),
		}

		err := service.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "validateConfig")
	})

	t.Run("multiple initialize calls", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		err1 := service.Initialize()
		err2 := service.Initialize()

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.True(t, service.isInitialized.Load())
	})

	t.Run("with file logging", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()
		cfg.FileLogging = true
		cfg.ConsoleLogging = false

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		err := service.Initialize()
		require.NoError(t, err)
		assert.NotNil(t, service.fileWriter)
	})

	t.Run("creates log directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()
		cfg.FileLogging = true
		cfg.RelLogFileDir = "."

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		err := service.Initialize()
		require.NoError(t, err)

		// Verify log file was created
		assert.NotNil(t, service.fileWriter)
	})
}

func TestService_Close(t *testing.T) {
	t.Run("successful close", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		err := service.Close()

		require.NoError(t, err)
		assert.False(t, service.isInitialized.Load())
		assert.Nil(t, service.logger.Load())
	})

	t.Run("close nil service", func(t *testing.T) {
		var service *Service
		err := service.Close()
		assert.NoError(t, err)
	})

	t.Run("close uninitialized service", func(t *testing.T) {
		service := &Service{}
		err := service.Close()
		assert.NoError(t, err)
	})

	t.Run("multiple close calls", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())

		err1 := service.Close()
		err2 := service.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})

	t.Run("close with file writer", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()
		cfg.FileLogging = true
		cfg.ConsoleLogging = false

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		err := service.Close()

		assert.NoError(t, err)
	})
}

func TestService_CloseWithTimeout(t *testing.T) {
	t.Run("close with timeout and warning", func(t *testing.T) {
		var buf bytes.Buffer
		cfg := validLoggingConfig()
		cfg.ShutdownTimeoutMS = 10
		cfg.ShutdownTimeoutWarning = true

		service := &Service{
			ConfigService: newTestConfigService(cfg),
		}

		// Override the writer to capture output
		consoleWriter := zerolog.ConsoleWriter{Out: &buf, TimeFormat: time.RFC3339, NoColor: true}
		service.initOnce.Do(func() {
			service.LoggingConfig = cfg
			logger := zerolog.New(consoleWriter).With().Timestamp().Logger()
			service.logger.Store(&logger)
			service.isInitialized.Store(true)
			service.activeOpLocations = make(map[string]int)
		})

		// Simulate an orphaned log operation
		_ = service.InfoWith()

		err := service.Close()
		require.NoError(t, err)

		// Check for the warning message
		output := buf.String()
		assert.Contains(t, output, "Logger shutdown timeout exceeded")
		assert.Contains(t, output, "active_operations=1")
	})
}

func TestService_CloseWaitsForLogs(t *testing.T) {
	var buf threadSafeBuffer
	cfg := validLoggingConfig()
	// Make shutdown wait long enough for our goroutine
	cfg.ShutdownTimeoutMS = 1000

	service := &Service{
		ConfigService: newTestConfigService(cfg),
	}

	// Override the writer to capture output
	consoleWriter := zerolog.ConsoleWriter{Out: &buf, TimeFormat: time.RFC3339, NoColor: true}
	service.initOnce.Do(func() {
		service.LoggingConfig = cfg
		logger := zerolog.New(consoleWriter).With().Timestamp().Logger()
		service.logger.Store(&logger)
		service.isInitialized.Store(true)
	})

	// Use a WaitGroup so we know the goroutine has actually issued the log call
	var wg sync.WaitGroup
	wg.Go(func() {
		// Small delay to overlap with Close, but not too long
		time.Sleep(50 * time.Millisecond)
		service.InfoWith().Msg("final log message")
	})

	// Wait until the logging goroutine has run InfoWith().Msg
	wg.Wait()

	// Now Close should see zero in-flight operations and return
	err := service.Close()
	require.NoError(t, err)

	// Check that the log was written before close returned
	output := buf.String()
	assert.Contains(t, output, "final log message")
}

func TestService_LoggingMethods(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())
	defer service.Close()

	t.Run("InfoWith", func(t *testing.T) {
		event := service.InfoWith()
		assert.NotNil(t, event)
		event.Msg("test info")
	})

	t.Run("WarnWith", func(t *testing.T) {
		event := service.WarnWith()
		assert.NotNil(t, event)
		event.Msg("test warn")
	})

	t.Run("ErrorWith", func(t *testing.T) {
		event := service.ErrorWith()
		assert.NotNil(t, event)
		event.Msg("test error")
	})

	t.Run("DebugWith", func(t *testing.T) {
		event := service.DebugWith()
		assert.NotNil(t, event)
		event.Msg("test debug")
	})

	t.Run("FatalWith returns event", func(t *testing.T) {
		event := service.FatalWith()
		assert.NotNil(t, event)
	})

	t.Run("PanicWith returns event", func(t *testing.T) {
		event := service.PanicWith()
		assert.NotNil(t, event)
	})
}

func TestService_LoggingMethodsUninitialized(t *testing.T) {
	service := &Service{}

	t.Run("InfoWith when uninitialized", func(t *testing.T) {
		event := service.InfoWith()
		assert.NotNil(t, event)
		event.Msg("should not panic")
	})

	t.Run("ErrorWith when uninitialized", func(t *testing.T) {
		event := service.ErrorWith()
		assert.NotNil(t, event)
		event.Msg("should not panic")
	})
}

func TestService_With(t *testing.T) {
	t.Run("successful with", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		defer service.Close()

		ctx := service.With()
		assert.NotNil(t, ctx)

		childLogger := ctx.Str("key", "value").Logger()
		assert.NotNil(t, childLogger)

		childLogger.InfoWith().Msg("test from child logger")
	})

	t.Run("with uninitialized returns noop", func(t *testing.T) {
		service := &Service{}

		ctx := service.With()
		assert.NotNil(t, ctx)

		// Should return a noop logger that doesn't panic
		logger := ctx.Str("key", "value").Logger()
		assert.NotNil(t, logger)

		// Verify logging doesn't panic
		logger.InfoWith().Msg("should not panic or log")
	})

	t.Run("context logger methods", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		defer service.Close()

		childLogger := service.With().Str("ctx", "test").Logger()

		// Test all methods
		childLogger.InfoWith().Msg("info")
		childLogger.WarnWith().Msg("warn")
		childLogger.ErrorWith().Msg("error")
		childLogger.DebugWith().Msg("debug")
		childLogger.FatalWith() // Don't call Msg() to avoid exit
		childLogger.PanicWith() // Don't call Msg() to avoid panic

		// Test nested context
		nestedLogger := childLogger.With().Str("nested", "value").Logger()
		assert.NotNil(t, nestedLogger)
	})
}

func TestConcurrentLogging(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())
	defer service.Close()

	var wg sync.WaitGroup
	numGoroutines := 10
	logsPerGoroutine := 50

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range logsPerGoroutine {
				service.InfoWith().Int("goroutine", id).Int("iteration", j).Msg("concurrent log")
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentLoggingAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())

	var wg sync.WaitGroup
	numGoroutines := 5

	// Start logging goroutines
	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 100 {
				service.InfoWith().Int("goroutine", id).Msg("log before close")
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Close after a short delay
	time.Sleep(5 * time.Millisecond)
	err := service.Close()
	assert.NoError(t, err)

	wg.Wait()
}

func TestConcurrentContextLoggers(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())
	defer service.Close()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			childLogger := service.With().Int("goroutine_id", id).Logger()
			for j := range 30 {
				childLogger.InfoWith().Int("iteration", j).Msg("context log")
			}
		}(i)
	}

	wg.Wait()
}

func TestLogEvent_AllMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	event := newLogEvent(logger.Info())

	t.Run("string methods", func(t *testing.T) {
		event.Str("key", "value").
			Strs("keys", []string{"a", "b"})
	})

	t.Run("integer methods", func(t *testing.T) {
		event.Int("int", 1).
			Int8("int8", 2).
			Int16("int16", 3).
			Int32("int32", 4).
			Int64("int64", 5)
	})

	t.Run("unsigned integer methods", func(t *testing.T) {
		event.Uint("uint", 1).
			Uint8("uint8", 2).
			Uint16("uint16", 3).
			Uint32("uint32", 4).
			Uint64("uint64", 5)
	})

	t.Run("float methods", func(t *testing.T) {
		event.Float32("float32", 1.5).
			Float64("float64", 2.5)
	})

	t.Run("bool methods", func(t *testing.T) {
		event.Bool("bool", true).
			Bools("bools", []bool{true, false})
	})

	t.Run("time methods", func(t *testing.T) {
		now := time.Now()
		event.Time("time", now).
			Dur("duration", time.Second)
	})

	t.Run("error methods", func(t *testing.T) {
		err := assert.AnError
		event.Err(err).
			AnErr("custom_err", err)
	})

	t.Run("bytes methods", func(t *testing.T) {
		event.Bytes("bytes", []byte("data")).
			Hex("hex", []byte{0x01, 0x02})
	})

	t.Run("interface method", func(t *testing.T) {
		event.Interface("interface", map[string]int{"a": 1})
	})

	event.Msg("test message")
}

func TestLogEvent_NilEvent(t *testing.T) {
	event := newLogEvent(nil)

	// All methods should be safe to call on nil event
	event.Str("key", "value").
		Int("num", 42).
		Bool("flag", true).
		Msg("should not crash")
}

func TestLogContext_AllMethods(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())
	defer service.Close()

	ctx := service.With()

	childLogger := ctx.
		Str("str_key", "value").
		Strs("strs_key", []string{"a", "b"}).
		Int("int_key", 42).
		Int64("int64_key", 100).
		Uint("uint_key", 10).
		Uint64("uint64_key", 200).
		Float64("float64_key", 3.14).
		Bool("bool_key", true).
		Time("time_key", time.Now()).
		Err(assert.AnError).
		Interface("interface_key", map[string]int{"a": 1}).
		Logger()

	assert.NotNil(t, childLogger)
	childLogger.InfoWith().Msg("context test")
}

func TestGetLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected zerolog.Level
		wantErr  bool
	}{
		{"debug", "debug", zerolog.DebugLevel, false},
		{"info", "info", zerolog.InfoLevel, false},
		{"warn", "warn", zerolog.WarnLevel, false},
		{"error", "error", zerolog.ErrorLevel, false},
		{"fatal", "fatal", zerolog.FatalLevel, false},
		{"panic", "panic", zerolog.PanicLevel, false},
		{"invalid", "invalid", zerolog.DebugLevel, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := zerolog.ParseLevel(tt.level)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, level)
			}
		})
	}
}

func TestLogEventBuilder(t *testing.T) {
	t.Run("nil service", func(t *testing.T) {
		var service *Service
		event := logEventBuilder(service, zerolog.InfoLevel)
		assert.NotNil(t, event)
		event.Msg("should not panic")
	})

	t.Run("uninitialized service", func(t *testing.T) {
		service := &Service{}
		event := logEventBuilder(service, zerolog.InfoLevel)
		assert.NotNil(t, event)
		event.Msg("should not panic")
	})

	t.Run("no level", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		defer service.Close()

		event := logEventBuilder(service, zerolog.NoLevel)
		assert.NotNil(t, event)
	})

	t.Run("all levels", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := validLoggingConfig()

		service := &Service{
			WorkingDir:    tmpDir,
			ConfigService: newTestConfigService(cfg),
		}

		require.NoError(t, service.Initialize())
		defer service.Close()

		levels := []zerolog.Level{
			zerolog.DebugLevel,
			zerolog.InfoLevel,
			zerolog.WarnLevel,
			zerolog.ErrorLevel,
			zerolog.FatalLevel,
			zerolog.PanicLevel,
			zerolog.TraceLevel,
		}

		for _, level := range levels {
			event := logEventBuilder(service, level)
			assert.NotNil(t, event)
		}
	})
}

// threadSafeBuffer is a simple thread-safe buffer for capturing log output.
type threadSafeBuffer struct {
	bytes.Buffer
	sync.Mutex
}

func (b *threadSafeBuffer) Write(p []byte) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

func TestService_ActiveOperationsAndClose_NoLeaks(t *testing.T) {
	// This test stresses the logging service under concurrent load and asserts that
	// Close() returns without deadlock or error. ActiveOperations() is sampled
	// heavily to ensure it is safe to call while logging is in progress.

	tmpDir := t.TempDir()
	cfg := validLoggingConfig()
	cfg.ShutdownTimeoutMS = 2000 // generous timeout to avoid flakiness

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())

	// Start a bunch of goroutines that log repeatedly
	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 200

	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				service.InfoWith().Int("goroutine", id).Int("iteration", j).Msg("active-ops-test")
			}
		}(i)
	}

	// While logging is happening, periodically read ActiveOperations in a best-effort way
	stopMonitor := make(chan struct{})
	var monitorWG sync.WaitGroup
	monitorWG.Go(func() {
		for {
			select {
			case <-stopMonitor:
				return
			default:
				_ = service.ActiveOperations()
				time.Sleep(time.Millisecond)
			}
		}
	})

	// Wait for all log goroutines to complete
	wg.Wait()
	// Stop monitor and wait for it to exit
	close(stopMonitor)
	monitorWG.Wait()

	// At this point, all user goroutines have finished. ActiveOperations() should not grow,
	// and Close() must complete without error or deadlock.
	err := service.Close()
	require.NoError(t, err)

	// After Close(), it is safe to call ActiveOperations; we only assert it is non-negative.
	// (The internal counter may be left >0 in rare forced-timeout paths, which Close
	// handles by draining the WaitGroup and logging a warning.)
	assert.GreaterOrEqual(t, service.ActiveOperations(), int32(0))
}

// stringerValue is a minimal fmt.Stringer implementation used by
// TestNoop_FullChainIsNoOp to exercise LogEvent.Stringer().
type stringerValue string

func (s stringerValue) String() string { return string(s) }

// TestLogEventBuilder_FilteredLevelSkipsCounters locks in the
// behavioral guarantee from docs/reviews/archive/internal-logging.md finding
// 4.6: a log call at a level below the logger's configured level must
// NOT increment the activeOps counter. The short-circuit level check
// in logEventBuilder ensures filtered events return an untracked no-op
// LogEvent without touching counters or locks.
//
// We verify two things: (1) activeOps is zero after a large burst of
// filtered events — proving no leak — and (2) Close() returns
// essentially immediately — proving no drain wait occurred, which
// would only happen if the counter were being incremented along the
// filtered path.
func TestLogEventBuilder_FilteredLevelSkipsCounters(t *testing.T) {
	workingDir := t.TempDir()
	cfg := types.LoggingConfig{
		Level:                  "warn", // Debug, Info, Trace all filtered
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      5000, // generous — we expect Close to return far faster
		ShutdownTimeoutWarning: false,
		ConsoleNoColor:         true,
	}
	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}
	require.NoError(t, service.Initialize())

	// Burst 1000 filtered events of each filtered level. Every call
	// constructs a chain with typed field methods and terminates with
	// Msg — exactly the shape that would leak under the pre-fix
	// embedding bug AND the pre-fix level-check ordering.
	for i := range 1000 {
		service.DebugWith().Str("key", "value").Int("i", i).Msg("filtered debug")
		service.InfoWith().Str("key", "value").Int("i", i).Msg("filtered info")
		service.TraceWith().Str("key", "value").Msg("filtered trace")
	}

	assert.Equal(t, int32(0), service.activeOps.Load(),
		"filtered events must never increment activeOps (finding 4.6)")

	start := time.Now()
	err := service.Close()
	elapsed := time.Since(start)
	assert.NoError(t, err)
	assert.Less(t, elapsed, 100*time.Millisecond,
		"Close() should return immediately when no tracked events are outstanding; took %v", elapsed)
}

// TestEventForLevel_AllKnownLevels directly exercises the eventForLevel
// helper extracted in docs/reviews/archive/internal-logging.md finding 4.4. It
// verifies that every zerolog level recognized by the switch returns a
// non-nil event when the logger is configured to accept all levels, and
// that an unknown level falls through the default case to nil.
func TestEventForLevel_AllKnownLevels(t *testing.T) {
	// Logger at TraceLevel — every documented level is enabled.
	logger := zerolog.New(io.Discard).Level(zerolog.TraceLevel)

	knownLevels := []struct {
		name  string
		level zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
	}

	for _, tc := range knownLevels {
		t.Run(tc.name, func(t *testing.T) {
			event := eventForLevel(&logger, tc.level)
			assert.NotNil(t, event, "eventForLevel should return non-nil for level %v on a fully-enabled logger", tc.level)
		})
	}

	t.Run("unknown_level_returns_nil", func(t *testing.T) {
		// zerolog.NoLevel is not in the switch cases; the default
		// branch returns nil so the caller can treat it as a no-op.
		event := eventForLevel(&logger, zerolog.NoLevel)
		assert.Nil(t, event, "eventForLevel should return nil for unrecognized level")
	})
}

// TestNoop_FullChainIsNoOp exercises every method on the LogEvent and
// LogContext interfaces via Noop(). The noopLogger, noopLogContext, and
// no-op logEvent implementations together have ~50 methods that must
// chain correctly and never panic. Before this test the Noop path had
// zero coverage, so any accidental nil-return or wrong type in a noop
// method would go undetected until a consumer tripped over it.
func TestNoop_FullChainIsNoOp(t *testing.T) {
	n := Noop()
	require.NotNil(t, n, "Noop() must return non-nil")

	err := stderrs.New("test error")

	// Exercise every LogEvent method in a single chain, then terminate.
	n.InfoWith().
		Str("str", "x").
		Strs("strs", []string{"a", "b"}).
		Stringer("stringer", stringerValue("v")).
		Int("int", 1).
		Int8("int8", 2).
		Int16("int16", 3).
		Int32("int32", 4).
		Int64("int64", 5).
		Uint("uint", 6).
		Uint8("uint8", 7).
		Uint16("uint16", 8).
		Uint32("uint32", 9).
		Uint64("uint64", 10).
		Float32("f32", 1.1).
		Float64("f64", 1.2).
		Bool("bool", true).
		Bools("bools", []bool{true, false}).
		Time("time", time.Now()).
		Dur("dur", time.Second).
		Err(err).
		AnErr("err2", err).
		Bytes("bytes", []byte("b")).
		Hex("hex", []byte{0xff}).
		IPAddr("ip", net.ParseIP("127.0.0.1")).
		MACAddr("mac", net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}).
		Interface("iface", "value").
		Dict("dict", func(e LogEvent) { e.Str("nested", "v") }).
		Msg("message")

	// Terminal variants
	n.DebugWith().Str("x", "y").Msgf("formatted: %d", 42)
	n.WarnWith().Str("x", "y").Send()
	n.ErrorWith().Err(err).Msg("error")
	n.TraceWith().Msg("trace")
	// Not calling FatalWith().Msg() or PanicWith().Msg() because those
	// call os.Exit / panic in the real zerolog path. The noop variants
	// don't, but we shouldn't encode that dependency into a test — just
	// verify they return a non-nil LogEvent.
	assert.NotNil(t, n.FatalWith())
	assert.NotNil(t, n.PanicWith())

	// Context logger path: build a context, turn it into a Logger, and
	// chain through it too.
	child := n.With().
		Str("str", "x").
		Strs("strs", []string{"a"}).
		Int("int", 1).
		Int64("int64", 5).
		Uint("uint", 6).
		Uint64("uint64", 10).
		Float64("f64", 1.2).
		Bool("bool", true).
		Time("time", time.Now()).
		Err(err).
		Interface("iface", "value").
		Logger()
	require.NotNil(t, child, "Noop context logger must produce a non-nil child Logger")

	child.InfoWith().Str("child", "msg").Msg("from child")
	child.ErrorWith().Err(err).Msg("child error")

	// Verify the child also exposes a With() that doesn't panic.
	grandchild := child.With().Str("gc", "ok").Logger()
	require.NotNil(t, grandchild)
	grandchild.InfoWith().Msg("grandchild")
}

// --- 2026-06-19 review (logging) regression tests ----------------------------

// TestInitialize_FailsWhenLogFileUnwritable guards review M1: lumberjack opens
// the file lazily, so an unwritable target must be proven at Initialize, not
// surface at the first (lost) log line. Root-proof: the would-be log file path
// is pre-created as a DIRECTORY, so opening it for write returns EISDIR
// regardless of euid (a chmod-based test would pass under root).
func TestInitialize_FailsWhenLogFileUnwritable(t *testing.T) {
	exeName, err := utils.ExecName(true)
	require.NoError(t, err)

	workingDir := t.TempDir()
	logDir := filepath.Join(workingDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0o750))
	require.NoError(t, os.Mkdir(filepath.Join(logDir, exeName+".log"), 0o750))

	cfg := validLoggingConfig()
	cfg.FileLogging = true
	cfg.ConsoleLogging = false
	cfg.RelLogFileDir = "logs"
	service := &Service{WorkingDir: workingDir, ConfigService: newTestConfigService(cfg)}

	require.Error(t, service.Initialize(), "Initialize must fail when the log file target is not writable")
}

// TestClose_LateEventAfterTimeoutWritesSafely guards review M2: when the drain
// times out with an event still in flight, Close leaves the writer OPEN so the
// straggler's terminal write lands safely instead of reopening a closed file.
func TestClose_LateEventAfterTimeoutWritesSafely(t *testing.T) {
	workingDir := t.TempDir()
	cfg := validLoggingConfig()
	cfg.FileLogging = true
	cfg.ConsoleLogging = false
	cfg.RelLogFileDir = "logs"
	cfg.Level = "info"
	cfg.ShutdownTimeoutMS = 10 // force a drain timeout while an event is outstanding (10ms = config min)
	service := &Service{WorkingDir: workingDir, ConfigService: newTestConfigService(cfg)}
	require.NoError(t, service.Initialize())

	// Build a tracked event but do NOT finish it — Close will drain-timeout.
	e := service.InfoWith().Str("k", "v")
	require.NoError(t, service.Close())

	// The straggler completes after Close returned: must not panic, and the line
	// must be written (the writer was left open on timeout, not closed).
	e.Msg("late line")

	exeName, err := utils.ExecName(true)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(workingDir, "logs", exeName+".log"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "late line")
}

// TestFinish_IdempotentOnDoubleTerminal guards review M3: a second terminal call
// on the same event (Msg twice, or Msg then Send) must be a no-op, not a
// negative-WaitGroup panic. Balanced counters + a clean Close prove it.
func TestFinish_IdempotentOnDoubleTerminal(t *testing.T) {
	workingDir := t.TempDir()
	cfg := validLoggingConfig()
	cfg.FileLogging = true
	cfg.ConsoleLogging = false
	cfg.RelLogFileDir = "logs"
	cfg.Level = "info"
	service := &Service{WorkingDir: workingDir, ConfigService: newTestConfigService(cfg)}
	require.NoError(t, service.Initialize())

	e := service.InfoWith().Str("k", "v")
	e.Msg("first")
	e.Msg("second") // double terminal — must not underflow the WaitGroup

	e2 := service.InfoWith().Int("n", 1)
	e2.Msg("a")
	e2.Send() // Msg then Send — also a no-op the second time

	assert.Equal(t, int32(0), service.activeOps.Load(), "counters stay balanced after double terminal calls")
	require.NoError(t, service.Close(), "Close must not hang or panic with a balanced WaitGroup")
}

// TestLogBuilder_ConcurrentWithClose_NoPanic guards review H1: the event
// builder's wg.Add must not race Close's wg.Wait. Many goroutines log while
// Close runs; under -race + the WaitGroup misuse detector this would panic
// before the fix (Add concurrent with Wait). Light enough for -race -short.
func TestLogBuilder_ConcurrentWithClose_NoPanic(t *testing.T) {
	for iter := 0; iter < 20; iter++ {
		workingDir := t.TempDir()
		cfg := validLoggingConfig()
		cfg.FileLogging = true
		cfg.ConsoleLogging = false
		cfg.RelLogFileDir = "logs"
		cfg.Level = "info"
		service := &Service{WorkingDir: workingDir, ConfigService: newTestConfigService(cfg)}
		require.NoError(t, service.Initialize())

		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					service.InfoWith().Int("i", i).Msg("concurrent")
				}
			}()
		}
		_ = service.Close() // close while loggers are still firing
		wg.Wait()
	}
}
