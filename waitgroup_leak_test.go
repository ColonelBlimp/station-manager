package logging

import (
	"testing"
	"time"

	"github.com/Station-Manager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWaitGroupLeakWithNilEvent tests that WaitGroup is properly cleaned up
// even when zerolog returns a nil event (which should never happen in practice,
// but we need to handle it defensively)
func TestWaitGroupLeakWithNilEvent(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "debug",
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      1000,
		ShutdownTimeoutWarning: false,
		ConsoleNoColor:         true,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Simulate the bug: call newTrackedLogEvent with nil event
	// This should not leak the WaitGroup counter

	// First, manually increment the counter like logEventBuilder does
	service.activeOps.Add(1)
	service.wg.Add(1)

	// Now call newTrackedLogEvent with nil event
	// The fix should decrement the counter
	event := newTrackedLogEvent(nil, service, "test")
	require.NotNil(t, event)

	// The event should be a no-op, but more importantly,
	// the WaitGroup counter should have been decremented

	// Try to close with a short timeout
	// If the WaitGroup was leaked, this would timeout
	done := make(chan error, 1)
	go func() {
		done <- service.Close()
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close() timed out - WaitGroup was leaked!")
	}
}

// TestWaitGroupBalanceWithMultipleOperations ensures that the WaitGroup
// counter stays balanced through various logging operations
func TestWaitGroupBalanceWithMultipleOperations(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "debug",
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      1000,
		ShutdownTimeoutWarning: false,
		ConsoleNoColor:         true,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Perform various logging operations
	service.InfoWith().Msg("test 1")
	service.ErrorWith().Msg("test 2")
	service.WarnWith().Msg("test 3")
	service.DebugWith().Msg("test 4")

	// Check that activeOps is 0 after all operations complete
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(0), service.activeOps.Load(), "activeOps should be 0 after all operations complete")

	// Close should succeed immediately
	done := make(chan error, 1)
	go func() {
		done <- service.Close()
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close() took too long - WaitGroup may be unbalanced")
	}
}

// TestWaitGroupWithContextLoggerNilEvent tests that context loggers
// also properly handle nil events
func TestWaitGroupWithContextLoggerNilEvent(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "debug",
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      1000,
		ShutdownTimeoutWarning: false,
		ConsoleNoColor:         true,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Create a context logger
	childLogger := service.With().Str("test", "context").Logger()

	// Use the context logger
	childLogger.InfoWith().Msg("test from context logger")

	// Check that activeOps is 0
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(0), service.activeOps.Load(), "activeOps should be 0 after context logger operation")

	// Close should succeed
	err := service.Close()
	assert.NoError(t, err)
}

// TestWaitGroupNoLeakOnQuickShutdown simulates the exact scenario from the bug report:
// server starts, migrations run with logging, then immediate shutdown
func TestWaitGroupNoLeakOnQuickShutdown(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "info",
		SkipFrameCount:         3,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      5,
		LogFileMaxAgeDays:      30,
		LogFileMaxSizeMB:       100,
		ShutdownTimeoutMS:      10000,
		ShutdownTimeoutWarning: true, // Enable warning to detect leaks
		ConsoleNoColor:         false,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Simulate migrations logging (like in database/migrations.go)
	service.InfoWith().Str("driver", "postgres").Msg("starting migrations")
	service.InfoWith().Msg("m.Up completed or no change")
	service.InfoWith().Msg("schema verified")

	// Simulate a brief pause (server starting)
	time.Sleep(5 * time.Millisecond)

	// Now immediately shutdown (like Ctrl+C right after startup)
	// This should NOT produce a timeout warning or leak
	err := service.Close()
	assert.NoError(t, err)

	// Verify no operations leaked
	assert.Equal(t, int32(0), service.activeOps.Load(), "No operations should be leaked after close")
}

// TestWaitGroupWithConcurrentLoggingAndShutdown tests the race condition
// where logging happens concurrently with shutdown
func TestWaitGroupWithConcurrentLoggingAndShutdown(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "debug",
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      2000,
		ShutdownTimeoutWarning: true,
		ConsoleNoColor:         true,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Start goroutines that log continuously
	stopLogging := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func(id int) {
			for {
				select {
				case <-stopLogging:
					return
				default:
					service.InfoWith().Int("goroutine", id).Msg("concurrent log")
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Let them log for a bit
	time.Sleep(50 * time.Millisecond)

	// Signal logging to stop
	close(stopLogging)

	// Wait a moment for goroutines to stop
	time.Sleep(10 * time.Millisecond)

	// Now close - should succeed without timeout
	done := make(chan error, 1)
	go func() {
		done <- service.Close()
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
		// Verify no leaks
		assert.Equal(t, int32(0), service.activeOps.Load(), "No operations should be leaked")
	case <-time.After(3 * time.Second):
		t.Fatal("Close() timed out with concurrent logging - possible WaitGroup leak")
	}
}

// TestNewTrackedLogEventWithNilService verifies defensive handling
func TestNewTrackedLogEventWithNilService(t *testing.T) {
	// This should not panic and should return a no-op event
	event := newTrackedLogEvent(nil, nil, "test")
	require.NotNil(t, event)

	// Calling Msg should be safe (no-op)
	event.Msg("test")
}

// TestNewTrackedLogEventWithNilEventOnly tests nil event but valid service
func TestNewTrackedLogEventWithNilEventOnly(t *testing.T) {
	workingDir := t.TempDir()

	cfg := types.LoggingConfig{
		Level:                  "debug",
		SkipFrameCount:         0,
		WithTimestamp:          false,
		ConsoleLogging:         false,
		FileLogging:            true,
		RelLogFileDir:          "logs",
		LogFileMaxBackups:      1,
		LogFileMaxAgeDays:      1,
		LogFileMaxSizeMB:       1,
		ShutdownTimeoutMS:      1000,
		ShutdownTimeoutWarning: false,
		ConsoleNoColor:         true,
		ConsoleTimeFormat:      "",
		LogFileCompress:        false,
	}

	service := &Service{
		WorkingDir:    workingDir,
		ConfigService: newTestConfigService(&cfg),
	}

	require.NoError(t, service.Initialize())

	// Manually increment counter (simulating what logEventBuilder does)
	service.activeOps.Add(1)
	service.wg.Add(1)

	initialOps := service.activeOps.Load()
	assert.Equal(t, int32(1), initialOps)

	// Call newTrackedLogEvent with nil event
	event := newTrackedLogEvent(nil, service, "test")
	require.NotNil(t, event)

	// The counter should have been decremented back to 0
	time.Sleep(1 * time.Millisecond) // Brief pause for consistency
	finalOps := service.activeOps.Load()
	assert.Equal(t, int32(0), finalOps, "Counter should be decremented when nil event is passed")

	// Cleanup
	err := service.Close()
	assert.NoError(t, err)
}
