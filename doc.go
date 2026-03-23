// Package logging provides a thin, concurrency-safe wrapper over rs/zerolog
// with a structured-first API, safe lifecycle management, and file rotation.
//
// Key features
//   - Structured logging only: prefer typed fields over printf-style helpers
//   - Context loggers via With() for per-request scoping
//   - Graceful shutdown that waits for in-flight logs (bounded timeout)
//   - File rotation via lumberjack and configurable console formatting
//   - Error history enrichment: for any Err/AnErr, the logger includes
//     the full error chain (outermost -> root), the root cause string, a
//     joined human-readable history, the operations chain (when using
//     Station-Manager DetailedError), and the root operation if available.
//
// Typical usage
//
//	svc := &logging.Service{ConfigService: cfg}
//	if err := svc.Initialize(); err != nil { panic(err) }
//	defer svc.Close()
//
//	svc.InfoWith().Str("user_id", id).Msg("processed")
//	req := svc.With().Str("request_id", rid).Logger()
//	req.ErrorWith().Err(err).Msg("failed")
package logging
