package logging

import (
	"io"
	"sync"

	"github.com/rs/zerolog"
)

// NewForWriter returns a fully-initialized Service that writes JSON log records
// to w, bypassing the config/working-dir/lumberjack path Initialize uses.
//
// It exists so packages OUTSIDE internal/logging can make assertions about what
// the daemon logs. Before it, they could not: the zerolog logger lives in an
// unexported atomic.Pointer, Noop() returns the Logger interface rather than a
// *Service, and the only alternatives were a real Initialize() writing to a temp
// file (workable but indirect) or the zero-value &logging.Service{}, which is a
// silent no-op. Several logging-coverage findings (docs/reviews/*-logging-gaps.md)
// specify behaviour that can only be pinned by reading the emitted records, so
// the seam is a prerequisite for testing them rather than a convenience.
//
// Deliberate properties, each load-bearing:
//
//   - It consumes initOnce, so a later Initialize() on the same Service cannot
//     replace the capture logger mid-test. Note the precise guarantee:
//     Initialize's own nil-ConfigService guard runs BEFORE initOnce, so calling
//     it on a bare capture Service still RETURNS that error — what cannot happen
//     is the logger being swapped. Pinned by
//     TestNewForWriter_InitializeCannotReplaceCaptureLogger.
//   - Every level is enabled (TraceLevel), so a test can assert on Debug records
//     without also configuring a level.
//   - Writer ownership stays with the CALLER. fileWriter is left nil, so Close()
//     skips the file-close branch entirely and never touches w.
//   - Writes are serialized through an internal mutex. zerolog itself does not
//     synchronize, and the natural test sink (*bytes.Buffer) is not safe for
//     concurrent use — without this, any test whose subject logs from more than
//     one goroutine would race rather than fail honestly.
//   - No Timestamp/Caller decoration. Tests parse records field-by-field, and a
//     wall-clock field would make output non-deterministic for no gain. A test
//     needing one should build its own logger.
//
// A nil w is treated as io.Discard rather than panicking, so a caller that
// forgets to wire the sink still gets a working (if silent) Service.
//
// The zero-value &logging.Service{} is unaffected and remains a no-op.
func NewForWriter(w io.Writer) *Service {
	if w == nil {
		w = io.Discard
	}
	s := &Service{}
	s.initOnce.Do(func() {
		logger := zerolog.New(&lockedWriter{w: w}).Level(zerolog.TraceLevel)
		s.logger.Store(&logger)
		s.isInitialized.Store(true)
	})
	return s
}

// lockedWriter serializes concurrent writes to the wrapped writer. It guards
// only the write; it does not own or close w.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
