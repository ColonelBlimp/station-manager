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
// The traversal unwraps one frame at a time via stdlib errors.Unwrap. Each
// frame is classified with smerrors.DetailedFrame — a DIRECT type assertion, not
// the chain-searching AsDetailedError, so a stdlib wrapper around a
// DetailedError gets its own ("" op) frame and the nested DetailedError isn't
// counted twice (review 2026-06-19 M1). It guards against excessive depth and
// repeated messages to avoid cycles.
func buildErrorChain(err error) (chain []string, ops []string, root string, rootOp string) {
	const maxDepth = 50
	visited := 0
	seen := map[string]bool{}

	for err != nil && visited < maxDepth {
		visited++

		if dErr, ok := smerrors.DetailedFrame(err); ok {
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
	// See docs/reviews/archive/internal-logging.md finding 4.6.
	//
	// The logger snapshot here is used only for the level check; the
	// tracked path below re-loads the logger under the read lock to
	// guard against concurrent Close() which may have stored nil.
	logger := s.logger.Load()
	if logger == nil || logger.GetLevel() > level {
		return newLogEvent(nil)
	}

	// Event is going to be logged. Acquire the read lock FIRST and re-check
	// under it, then register the in-flight op: wg.Add must NOT run concurrently
	// with Close's wg.Wait (a documented WaitGroup misuse that can panic), and
	// holding the read lock blocks Close from starting its wait until we've added
	// (review 2026-06-19 H1 — mirrors the context-builder ordering in event.go).
	// The lock-free snapshot above is only a level fast-path; the authoritative
	// checks are these, under the lock.
	s.mu.RLock()

	if !s.isInitialized.Load() {
		s.mu.RUnlock()
		return newLogEvent(nil)
	}

	// Re-load the logger under the lock — Close() replaces the logger
	// pointer with nil, and the short-circuit snapshot above may be stale.
	logger = s.logger.Load()
	if logger == nil {
		s.mu.RUnlock()
		return newLogEvent(nil)
	}

	event := eventForLevel(logger, level)
	if event == nil {
		s.mu.RUnlock()
		return newLogEvent(nil)
	}

	// Register the in-flight op while STILL holding the read lock (so Close
	// cannot have started wg.Wait), then capture debug location. Balanced by
	// finish() on the event's terminal Msg/Msgf/Send.
	s.activeOps.Add(1)
	s.wg.Add(1)
	location := trackLocation(s, 2)

	s.mu.RUnlock()

	return newTrackedLogEvent(event, s, location)
}

// eventForLevel returns the zerolog.Event for the given level from the
// given logger. Returns nil for zerolog.NoLevel or any unrecognized
// level (which the caller should treat as an untracked no-op).
//
// Extracted so the 7-case level dispatch isn't duplicated between
// logEventBuilder and newTrackedContextLogEvent — see
// docs/reviews/archive/internal-logging.md finding 4.4.
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
