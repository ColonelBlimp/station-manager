// Package safego provides a panic-recovering wrapper for long-lived
// goroutines (forwarder workers, SSE publishers, future enrichment
// workers). The recovered panic is handed to a caller-supplied handler
// for structured logging; if respawn is enabled, fn is re-invoked
// after a short cooldown.
//
// Design rationale in docs/v2-design/forwarding.md §9.
//
// Package layout note: the forwarding design originally proposed
// internal/utils/safego.go, but internal/logging already imports
// internal/utils, so a *logging.Service parameter in utils would
// create a cycle. safego lives in its own package and takes a
// callback (PanicHandler) instead of a concrete logger, so it has no
// dependency on logging at all — callers wire up the log format they
// want.
package safego

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// PanicHandler receives the recovered panic. The goroutine name is
// passed through from Go's `name` parameter so the handler can
// attribute the panic in structured logs. Stack is captured via
// runtime/debug.Stack.
type PanicHandler func(name string, panicValue any, stack []byte)

// respawnCooldown is the sleep between a recovered panic and a
// respawn attempt. Long enough that a deterministic panic loop
// doesn't hoard CPU; short enough that transient panics are picked
// up again within a reasonable window. Atomic so tests can dial it
// down from another goroutine without racing against the goroutines
// that read it.
var respawnCooldown atomic.Int64 // nanoseconds

func init() {
	respawnCooldown.Store(int64(5 * time.Second))
}

// Go runs fn in a new goroutine with panic recovery.
//
// If fn panics, onPanic is invoked with the recovered value and a
// stack trace; if respawn is true, Go re-invokes fn after
// respawnCooldown in the same goroutine. Respawns are cancelled if
// ctx is Done before the cooldown elapses — shutdown signals
// propagate into the respawn loop, not just into fn itself.
//
// Loop-based respawn (rather than recursive self-call) keeps the
// stack short and makes panics easier to read in logs: every
// goroutine spawned by Go has a single creation site.
//
// fn should be restart-safe when respawn=true — a panicking fn that
// holds state in local variables will start fresh on each respawn.
// Worker loops that drive off shared state (DB rows, config) are
// restart-safe by construction.
//
// Use respawn=false for one-shot goroutines where a panic should be
// reported but not automatically retried (e.g. a boot-time task).
// Use respawn=true for long-lived workers whose absence silently
// breaks a feature.
//
// For workers whose lifecycle the caller needs to wait on at
// shutdown, use GoTracked instead — Go does no WaitGroup management
// and a respawn after a panic will outlive any Add/Done bracketing
// done at the call site.
func Go(ctx context.Context, name string, onPanic PanicHandler, fn func(), respawn bool) {
	go runWithRespawn(ctx, name, onPanic, fn, respawn)
}

// GoTracked is Go with explicit shutdown accounting. wg.Add(1) is
// called synchronously at the call site; wg.Done() is called once
// when the goroutine permanently exits (normal return, panic with
// respawn=false, or ctx cancellation during a respawn cooldown).
//
// This closes the underflow window the original "wg.Add inside the
// closure" pattern in cmd/smd/main.go suffered from: the
// WaitGroup count never drops to zero between a panic and its
// respawn, because the goroutine never actually ends — only fn
// loops. wg.Wait() therefore reflects "is any worker still running
// or about to respawn," not "is fn currently between attempts."
func GoTracked(ctx context.Context, name string, onPanic PanicHandler, fn func(), respawn bool, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWithRespawn(ctx, name, onPanic, fn, respawn)
	}()
}

// runWithRespawn is the shared body of Go and GoTracked. Loops
// fn-with-recovery until either fn returns normally, fn panics with
// respawn=false, or ctx is cancelled during the cooldown.
func runWithRespawn(ctx context.Context, name string, onPanic PanicHandler, fn func(), respawn bool) {
	for {
		panicked := false
		func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				panicked = true
				if onPanic != nil {
					onPanic(name, r, debug.Stack())
				}
			}()
			fn()
		}()
		if !panicked || !respawn {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(respawnCooldown.Load())):
		}
		if ctx.Err() != nil {
			return
		}
	}
}
