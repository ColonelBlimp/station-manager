# Station Manager: logging package

A thin, concurrency-safe wrapper around rs/zerolog with a structured-first API, safe lifecycle (Initialize/Close), file rotation via lumberjack, and rich error history enrichment.

## Highlights
- Structured logs only: compose fields with typed helpers
- Context loggers via With() for request/job scoping
- Graceful shutdown: waits for in-flight logs with a bounded timeout
- File rotation (size/age/backups) and optional console formatting
- Error history enrichment: automatically logs full error chains and operations

## Quick start

```go
svc := &logging.Service{ConfigService: cfgSvc}
if err := svc.Initialize(); err != nil { panic(err) }
defer svc.Close()

svc.InfoWith().Str("user_id", id).Int("count", 3).Msg("processed")

req := svc.With().Str("request_id", reqID).Logger()
req.DebugWith().Str("path", path).Msg("incoming request")

// Error history enrichment
if err != nil {
    svc.ErrorWith().Err(err).Msg("operation failed")
}
```

## Error history enrichment
When you attach an error with Err/AnErr, the logger emits:
- error: the standard zerolog error field (string)
- error_chain: array of messages from outermost -> root
- error_root: the root cause message
- error_history: the joined chain string (outer -> ... -> root)
- error_ops: array of operation identifiers per chain element (if using DetailedError; empty strings for non-DetailedError links)
- error_root_op: the root operation identifier (if available)

For AnErr("db_err", err), the keys are prefixed accordingly (db_err_chain, db_err_root, db_err_history, db_err_ops, db_err_root_op).

Example output (JSON, abbreviated):

```json
{
  "level":"error",
  "msg":"operation failed",
  "error":"startup failed",
  "error_chain":[
    "startup failed",
    "failed to connect to database",
    "dial tcp 127.0.0.1:5432: connect: connection refused"
  ],
  "error_ops":["server.Start","db.Open","db.Connect"],
  "error_root":"dial tcp 127.0.0.1:5432: connect: connection refused",
  "error_root_op":"db.Connect",
  "error_history":"startup failed -> failed to connect to database -> dial tcp 127.0.0.1:5432: connect: connection refused"
}
```

## Configuration (types.LoggingConfig)
Relevant fields (non-exhaustive):
- `Level`: `trace|debug|info|warn|error|fatal|panic`
- `WithTimestamp`: include timestamp field
- `SkipFrameCount`: enable caller info with given skip frames when > 0
- `ConsoleLogging` / `FileLogging`: enable writers; if both false, file logging is enabled by default
- `RelLogFileDir`: relative directory for log files (validated for safety; created on init)
- `LogFileMaxBackups`, `LogFileMaxAgeDays`, `LogFileMaxSizeMB`, `LogFileCompress`
- `ConsoleNoColor`, `ConsoleTimeFormat`
- `ShutdownTimeoutMS`, `ShutdownTimeoutWarning`

## Lifecycle and concurrency
- `Initialize()`: validates config, ensures directory, sets up writers, applies level, timestamp/caller, stores logger
- `Close()`: stops accepting new logs, waits up to `ShutdownTimeoutMS` for in-flight events, optionally warns, closes file writer
- All event builders use internal reference counting to avoid races during `Close()`

## Context loggers

```go
req := svc.With().Str("request_id", id).Logger()
req.InfoWith().Str("route", "/v1/items").Int("count", 10).Msg("processed")
```

## Dump helper

```go
svc.Dump(struct{ A int; B string }{A:1, B:"x"})
```
Safely logs nested structures at Debug level with cycle protection and depth limits.

## Testing
- Unit tests cover lifecycle, concurrent usage, event builders, Dump, and error history enrichment.

## Notes
- Error ops are included when errors are created via github.com/Station-Manager/errors.DetailedError.
- Standard library wrapped errors (errors.Unwrap / fmt.Errorf("%w")) are traversed for messages, but have empty ops.
