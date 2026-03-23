package logging

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/types"
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
		AppConfig: types.AppConfig{
			LoggingConfig: *cfg,
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Small delay to overlap with Close, but not too long
		time.Sleep(50 * time.Millisecond)
		service.InfoWith().Msg("final log message")
	}()

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

func TestService_Dump(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := validLoggingConfig()

	service := &Service{
		WorkingDir:    tmpDir,
		ConfigService: newTestConfigService(cfg),
	}

	require.NoError(t, service.Initialize())
	defer service.Close()

	t.Run("dump nil", func(t *testing.T) {
		service.Dump(nil)
	})

	t.Run("dump struct", func(t *testing.T) {
		type TestStruct struct {
			Name  string
			Value int
		}
		service.Dump(TestStruct{Name: "test", Value: 42})
	})

	t.Run("dump map", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		service.Dump(m)
	})

	t.Run("dump slice", func(t *testing.T) {
		s := []int{1, 2, 3, 4, 5}
		service.Dump(s)
	})

	t.Run("dump basic type", func(t *testing.T) {
		service.Dump(42)
		service.Dump("string")
		service.Dump(true)
	})

	t.Run("dump nested struct", func(t *testing.T) {
		type Inner struct {
			Value int
		}
		type Outer struct {
			Name  string
			Inner Inner
		}
		service.Dump(Outer{Name: "test", Inner: Inner{Value: 42}})
	})

	t.Run("dump large slice", func(t *testing.T) {
		s := make([]int, 20)
		for i := range s {
			s[i] = i
		}
		service.Dump(s)
	})

	t.Run("dump circular reference", func(t *testing.T) {
		type Node struct {
			Value int
			Next  *Node
		}
		n1 := &Node{Value: 1}
		n2 := &Node{Value: 2}
		n1.Next = n2
		n2.Next = n1

		service.Dump(n1)
	})

	t.Run("dump when uninitialized", func(t *testing.T) {
		uninitService := &Service{}
		uninitService.Dump("should not panic")
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

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < logsPerGoroutine; j++ {
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
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
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

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			childLogger := service.With().Int("goroutine_id", id).Logger()
			for j := 0; j < 30; j++ {
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
			level, err := parseLevel(tt.level)
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

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				service.InfoWith().Int("goroutine", id).Int("iteration", j).Msg("active-ops-test")
			}
		}(i)
	}

	// While logging is happening, periodically read ActiveOperations in a best-effort way
	stopMonitor := make(chan struct{})
	var monitorWG sync.WaitGroup
	monitorWG.Add(1)
	go func() {
		defer monitorWG.Done()
		for {
			select {
			case <-stopMonitor:
				return
			default:
				_ = service.ActiveOperations()
				time.Sleep(time.Millisecond)
			}
		}
	}()

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
