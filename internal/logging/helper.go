package logging

import (
	stderrs "errors"
	"strings"

	smerrors "github.com/ColonelBlimp/station-manager/internal/errors"
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
			err = stderrs.Unwrap(err)
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

	// Increment active operations counter before acquiring lock. Every
	// early-return path below must decrement these (via releaseCounters)
	// to keep the WaitGroup balanced.
	s.activeOps.Add(1)
	s.wg.Add(1)

	// Debug: capture file:line of the caller, but only when compiled
	// with the `logging_debug` build tag. In release builds trackLocation
	// is a no-op that returns "", so there is no runtime.Caller cost.
	location := trackLocation(s, 2)

	// Acquire read lock to prevent Close() from running during log creation
	s.mu.RLock()

	// Double-check after acquiring lock (TOCTOU protection)
	if !s.isInitialized.Load() {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil)
	}

	logger := s.logger.Load()
	if logger == nil {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil)
	}

	if logger.GetLevel() > level {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil) // Return early if level is not enabled
	}

	event := eventForLevel(logger, level)
	if event == nil {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil)
	}

	s.mu.RUnlock()

	// Wrap the event so its terminal Msg/Msgf/Send call decrements the
	// counters this function incremented above.
	return newTrackedLogEvent(event, s, location)
}

// eventForLevel returns the zerolog.Event for the given level from the
// given logger. Returns nil for zerolog.NoLevel or any unrecognized
// level (which the caller should treat as an untracked no-op).
//
// Extracted so the 7-case level dispatch isn't duplicated between
// logEventBuilder and newTrackedContextLogEvent — see
// docs/reviews/internal-logging.md finding 4.4.
func eventForLevel(l *zerolog.Logger, level zerolog.Level) *zerolog.Event {
	switch level {
	case zerolog.DebugLevel:
		return l.Debug()
	case zerolog.InfoLevel:
		return l.Info()
	case zerolog.WarnLevel:
		return l.Warn()
	case zerolog.ErrorLevel:
		return l.Error()
	case zerolog.FatalLevel:
		return l.Fatal()
	case zerolog.PanicLevel:
		return l.Panic()
	case zerolog.TraceLevel:
		return l.Trace()
	default:
		return nil
	}
}

// releaseCounters undoes the active-operations counter + WaitGroup
// increment that logEventBuilder makes before entering any of its
// early-return paths. Extracted as a helper to eliminate three copies
// of the same 3-6 line unwind block that the previous code carried in
// logEventBuilder's early-return paths.
func releaseCounters(s *Service, location string) {
	s.activeOps.Add(-1)
	s.wg.Done()
	untrackLocation(s, location)
}
