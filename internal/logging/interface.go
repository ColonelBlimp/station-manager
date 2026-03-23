package logging

// Logger exposes structured logging event builders and context creation.
// Usage pattern: logger.InfoWith().Str("user_id", id).Int("count", 5).Msg("processed")
// Create scoped loggers via With():
//
//	req := logger.With().Str("request_id", id).Logger()
//
// Then use req.InfoWith()/ErrorWith() etc. String-format helpers are intentionally
// not provided; prefer structured logs for queryability.
type Logger interface {
	TraceWith() LogEvent
	DebugWith() LogEvent
	InfoWith() LogEvent
	WarnWith() LogEvent
	ErrorWith() LogEvent
	FatalWith() LogEvent
	PanicWith() LogEvent

	// With for context logger creation: creates a new logger with pre-populated
	// fields that will be included in all subsequent logs.
	With() LogContext
}
