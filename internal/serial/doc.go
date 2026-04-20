// Package serial is a thin, concurrency-aware wrapper around go.bug.st/serial
// for HF transceiver CAT control. It performs byte-level line-oriented I/O
// with a configurable delimiter; CAT protocol framing (Yaesu, Icom, Kenwood)
// lives in internal/cat, not here.
//
// # API shape
//
// Byte-oriented methods (WriteCommandBytes, ReadResponseBytes, ExecBytes) are
// the real API. The string-oriented wrappers (WriteCommand, ReadResponse,
// Exec) convert without validating UTF-8. Prefer the byte variants for new
// code — CAT parsing inspects specific byte offsets and Icom CI-V is binary.
//
// # Line delimiter
//
// Writes auto-append Config.LineDelimiter if the command's last byte is not
// already the delimiter. Reads are framed on the delimiter and returned
// stripped of it. Lines longer than 4096 bytes are dropped with a best-effort
// notification on the Errors() channel; the reader keeps running for
// subsequent well-formed lines.
//
// # Concurrency: one reader, many writers
//
// Writes are safe to call concurrently from any number of goroutines; the
// underlying port is serialised internally. Reads are not — framed responses
// come from a single background goroutine and must be consumed by at most
// one goroutine at a time (via ReadResponse, ReadResponseBytes, Exec, or
// ExecBytes). The canonical pattern is a single dedicated reader goroutine
// that forwards lines to the rest of the application; writers elsewhere
// call WriteCommandBytes freely.
//
// # Errors
//
// Every public operation wraps failures with an internal/errors.Op tag
// (serial.Open, serial.ReadResponseBytes, serial.WriteCommandBytes, etc.).
// Use errors.Is to test for the package sentinel ErrClosed or for
// context.Canceled / context.DeadlineExceeded — all are preserved through
// the wrapping chain.
//
// The Errors() channel carries at most one terminal error from the
// background reader loop and is closed when the reader exits. A graceful
// Close may close it without producing a value. A typical supervisor reads
// from it to trigger reconnection:
//
//	go func() {
//	    if err, ok := <-port.Errors(); ok && err != nil {
//	        // log and trigger reconnect
//	    }
//	}()
//
// # Lifecycle
//
// Close is idempotent. A closed Port cannot be reopened; construct a new
// one via Open.
//
// See example_test.go for a runnable usage example. cmd/catcli is a small
// diagnostic binary for manual rig poking.
package serial
