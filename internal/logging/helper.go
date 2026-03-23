package logging

import (
	stderrs "errors"
	"fmt"
	"runtime"
	"strings"

	smerrors "github.com/Station-Manager/errors"
	"github.com/rs/zerolog"
)

// parseLevel parses a string log level into a zerolog.Level.
// Returns zerolog.NoLevel and an error if parsing fails.
func parseLevel(level string) (zerolog.Level, error) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.NoLevel, err
	}
	return l, nil
}

// buildErrorChain walks an error's cause chain and returns:
//   - chain: outermost -> innermost error messages
//   - ops: operation identifiers for DetailedError links ("" if not available)
//   - root: the innermost error message
//   - rootOp: the innermost operation identifier if available
//
// The traversal prefers Station-Manager DetailedError.Cause() and then
// falls back to stdlib errors.Unwrap. It guards against excessive depth
// and repeated messages to avoid cycles.
func buildErrorChain(err error) (chain []string, ops []string, root string, rootOp string) {
	const maxDepth = 50
	visited := 0
	seen := map[string]bool{}

	for err != nil && visited < maxDepth {
		visited++

		if dErr, ok := smerrors.AsDetailedError(err); ok && dErr != nil {
			msg := dErr.Error()
			chain = append(chain, msg)
			op := string(dErr.Op())
			ops = append(ops, op)
			// prefer unwrapping via our error type first
			err = dErr.Cause()
			continue
		}

		// Fallback: generic error
		msg := err.Error()
		// avoid infinite loops if messages repeat due to unusual cycles
		if seen[msg] {
			break
		}
		seen[msg] = true
		chain = append(chain, msg)
		ops = append(ops, "")
		// unwrap via stdlib
		err = stderrs.Unwrap(err)
	}

	if len(chain) > 0 {
		root = chain[len(chain)-1]
	}
	if len(ops) > 0 {
		rootOp = ops[len(ops)-1]
	}
	return
}

// joinChain returns a single string for the error chain separated by " -> ".
func joinChain(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	return strings.Join(chain, " -> ")
}

// logEventBuilder creates a log event for the given level.
// It uses reference counting to ensure the logger remains valid for the duration
// of the logging operation, preventing race conditions with Close().
// If the level is disabled on the logger, it returns a no-op LogEvent.
func logEventBuilder(s *Service, level zerolog.Level) LogEvent {
	if s == nil || !s.isInitialized.Load() {
		return newLogEvent(nil)
	}
	if level == zerolog.NoLevel {
		return newLogEvent(nil)
	}

	// Check if initialized BEFORE incrementing counters to avoid leaks during shutdown
	if !s.isInitialized.Load() {
		return newLogEvent(nil)
	}

	// Increment active operations counter before acquiring lock
	s.activeOps.Add(1)
	s.wg.Add(1)

	// Debug: Track where this operation was created
	var location string
	if s.LoggingConfig.ShutdownTimeoutWarning {
		_, file, line, ok := runtime.Caller(2)
		if ok {
			location = fmt.Sprintf("%s:%d", file, line)
			s.mu.Lock()
			if s.activeOpLocations == nil {
				s.activeOpLocations = make(map[string]int)
			}
			s.activeOpLocations[location]++
			s.mu.Unlock()
		}
	}

	// Acquire read lock to prevent Close() from running during log creation
	s.mu.RLock()

	// Double-check after acquiring lock (TOCTOU protection)
	if !s.isInitialized.Load() {
		s.mu.RUnlock()
		s.activeOps.Add(-1)
		s.wg.Done()
		// Also decrement location counter if tracking is enabled
		if location != "" {
			s.mu.Lock()
			if s.activeOpLocations != nil {
				s.activeOpLocations[location]--
				if s.activeOpLocations[location] <= 0 {
					delete(s.activeOpLocations, location)
				}
			}
			s.mu.Unlock()
		}
		return newLogEvent(nil)
	}

	logger := s.logger.Load()
	if logger == nil {
		s.mu.RUnlock()
		s.activeOps.Add(-1)
		s.wg.Done()
		// Also decrement location counter if tracking is enabled
		if location != "" {
			s.mu.Lock()
			if s.activeOpLocations != nil {
				s.activeOpLocations[location]--
				if s.activeOpLocations[location] <= 0 {
					delete(s.activeOpLocations, location)
				}
			}
			s.mu.Unlock()
		}
		return newLogEvent(nil)
	}

	if logger.GetLevel() > level {
		s.mu.RUnlock()
		s.activeOps.Add(-1)
		s.wg.Done()
		// Also decrement location counter if tracking is enabled
		if location != "" {
			s.mu.Lock()
			if s.activeOpLocations != nil {
				s.activeOpLocations[location]--
				if s.activeOpLocations[location] <= 0 {
					delete(s.activeOpLocations, location)
				}
			}
			s.mu.Unlock()
		}
		return newLogEvent(nil) // Return early if level is not enabled
	}

	var event *zerolog.Event
	switch level {
	case zerolog.DebugLevel:
		event = logger.Debug()
	case zerolog.InfoLevel:
		event = logger.Info()
	case zerolog.WarnLevel:
		event = logger.Warn()
	case zerolog.ErrorLevel:
		event = logger.Error()
	case zerolog.FatalLevel:
		event = logger.Fatal()
	case zerolog.PanicLevel:
		event = logger.Panic()
	case zerolog.TraceLevel:
		event = logger.Trace()
	default:
		s.mu.RUnlock()
		s.activeOps.Add(-1)
		s.wg.Done()
		return newLogEvent(nil)
	}

	s.mu.RUnlock()

	// Wrap the event to decrement counter when done
	return newTrackedLogEvent(event, s, location)
}
