package logging

import (
	stderrs "errors"
	"strings"

	smerrors "github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/rs/zerolog"
)

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

	// Short-circuit level check BEFORE incrementing counters or taking
	// any lock. In the common case — a filtered-out Debug event at
	// runtime level Info, for example — this returns an untracked
	// no-op after only two atomic reads (s.logger.Load and
	// logger.GetLevel), avoiding the counter increment, WaitGroup.Add,
	// location capture, and RWMutex RLock the tracked path requires.
	// See docs/reviews/internal-logging.md finding 4.6.
	//
	// The logger snapshot here is used only for the level check; the
	// tracked path below re-loads the logger under the read lock to
	// guard against concurrent Close() which may have stored nil.
	logger := s.logger.Load()
	if logger == nil || logger.GetLevel() > level {
		return newLogEvent(nil)
	}

	// Event is going to be logged. Commit: increment counters (every
	// early-return path below must decrement them via releaseCounters
	// to keep the WaitGroup balanced), capture debug location if the
	// logging_debug build tag is set, then acquire the read lock.
	s.activeOps.Add(1)
	s.wg.Add(1)
	location := trackLocation(s, 2)

	s.mu.RLock()

	// TOCTOU: between the short-circuit check and the lock acquisition
	// above, Close() could have run and flipped isInitialized to false.
	if !s.isInitialized.Load() {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil)
	}

	// Re-load the logger under the lock — Close() replaces the logger
	// pointer with nil, and the short-circuit snapshot above may be
	// stale at this point.
	logger = s.logger.Load()
	if logger == nil {
		s.mu.RUnlock()
		releaseCounters(s, location)
		return newLogEvent(nil)
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
