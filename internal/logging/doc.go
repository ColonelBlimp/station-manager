// Package logging provides a thin, concurrency-safe wrapper over rs/zerolog
// with a structured-first API, safe lifecycle management, and file rotation.
//
// # Key features
//
//   - Structured logging only: prefer typed fields over printf-style helpers
//   - Context loggers via With() for per-request scoping
//   - Graceful shutdown that waits for in-flight logs (bounded timeout)
//   - File rotation via lumberjack and configurable console formatting
//   - Error chain enrichment: Err/AnErr calls automatically emit the full
//     error chain, the root cause, operation identifiers, and a
//     human-readable history. See "Error chain enrichment" below.
//
// # Typical usage
//
//	svc := &logging.Service{ConfigService: cfg}
//	if err := svc.Initialize(); err != nil { panic(err) }
//	defer svc.Close()
//
//	svc.InfoWith().Str("user_id", id).Msg("processed")
//	req := svc.With().Str("request_id", rid).Logger()
//	req.ErrorWith().Err(err).Msg("failed")
//
// # Error chain enrichment
//
// When you attach an error via Err(err) or AnErr(key, err), the logger
// emits additional structured fields describing the full error chain in
// addition to zerolog's standard "error" field:
//
//	error_chain    — array of each frame's full Error() string, outermost to
//	                 root. Note this is RECURSIVE: a DetailedError's Error()
//	                 returns its own message PLUS the remaining cause tail, so
//	                 each entry repeats the chain from that frame down (deeper
//	                 entries are progressively shorter suffixes). For the
//	                 per-frame operation identifiers alone, use error_ops.
//	error_root     — the root cause message (last element of the chain)
//	error_history  — the joined chain as a single string with " -> " between
//	                 frames; human-readable for console output
//	error_ops      — array of operation identifiers per chain frame; empty
//	                 string for frames that are not DetailedError
//	                 (stdlib errors or fmt.Errorf wrappers)
//	error_root_op  — the root frame's operation identifier if it is a
//	                 DetailedError, otherwise absent
//
// For AnErr(key, err) the field names are prefixed with the key — for
// example, AnErr("db_err", err) emits db_err_chain, db_err_root,
// db_err_history, db_err_ops, and db_err_root_op.
//
// Example log output (JSON, abbreviated):
//
//	{
//	  "level": "error",
//	  "msg":   "operation failed",
//	  "error": "startup failed",
//	  "error_chain": [
//	    "server.Start: startup failed: db.Open: failed to connect to database: db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
//	    "db.Open: failed to connect to database: db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
//	    "db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused"
//	  ],
//	  "error_ops":     ["server.Start", "db.Open", "db.Connect"],
//	  "error_root":    "db.Connect: dial tcp 127.0.0.1:5432: connect: connection refused",
//	  "error_root_op": "db.Connect",
//	  "error_history": "<error_chain joined with ' -> '>"
//	}
//
// Operation identifiers (error_ops, error_root_op) are populated only
// when errors are created via
// github.com/ColonelBlimp/station-manager/internal/errors (the
// DetailedError type). Standard-library wrapped errors (fmt.Errorf with
// %w, errors.Unwrap chains) are traversed correctly for their message
// strings but contribute empty strings to error_ops because they carry
// no operation context. See docs/reviews/archive/internal-errors.md and
// docs/reviews/archive/internal-logging.md for the full pattern rationale.
package logging
