// Package safego provides a panic-recovering wrapper for long-lived
// goroutines (forwarder workers, SSE publishers, future enrichment
// workers). The recovered panic is handed to a caller-supplied handler
// for structured logging; if respawn is enabled, fn is re-invoked
// after a short cooldown.
//
// Design rationale in docs/v2-design/forwarding.md §9.
//
// # Caller contract (load-bearing — review 2026-06-19)
//
// Two obligations are easy to miss and have caused real bugs:
//
//  1. A panic is recovered OUTSIDE fn's body, so code AFTER the panic point does
//     not run. Anything that must happen on every exit — releasing a semaphore,
//     clearing in-flight state, sending on a channel, publishing status, invoking
//     a completion callback — must be in a `defer` INSIDE fn, not written as
//     ordinary statements after the panic-prone call. (ft8.tx's transmit state
//     wedged in-flight because its cleanup wasn't deferred.)
//  2. With GoTracked, wg.Add(1) runs synchronously at the call site, but that only
//     helps if the goroutine is launched BEFORE any other path can reach the
//     matching wg.Wait(). If the caller exposes waitable in-flight state (a cancel
//     func, an "active" flag) before calling GoTracked, a concurrent shutdown can
//     pass Wait with a zero counter. Call GoTracked while still holding the lock
//     that gates the waiter (see internal/lookup/refresher for the pattern).
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

// SetRespawnCooldownForTest overrides the respawn cooldown and returns a restore
// func. Exported so a subsystem's tests (in another package) can exercise their
// panic → respawn wiring without waiting the multi-second production cooldown.
// Test-only by intent; production never calls it.
func SetRespawnCooldownForTest(d time.Duration) (restore func()) {
	old := respawnCooldown.Load()
	respawnCooldown.Store(int64(d))
	return func() { respawnCooldown.Store(old) }
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

// GoTrackedPreAdded runs fn ONCE in a tracked goroutine for a caller that has ALREADY
// performed wg.Add(1) — the load-bearing pattern for workers that must be counted before
// any concurrent Stop/Wait can observe the WaitGroup (the bridge RF-safety workers register
// their Add under the same lock that gates Stop). Unlike GoTracked it does NOT Add (the
// caller owns the count) and does NOT respawn (the caller's panicPolicy owns any guarded
// rescheduling — a blind respawn of an RF-safety worker is exactly the wrong thing).
//
// On a panic in fn, onPanic records it AND panicPolicy runs, so a worker's safe-state
// (latch a TX alarm, keep TX blocked, …) is applied even if the logging handler itself
// panics — invokePanicHandler swallows an onPanic fault, and panicPolicy is run guarded so
// a fault in the policy can't escape wg.Done either. panicPolicy is skipped on a clean
// return; either callback may be nil.
//
// The caller keeps its wg.Add under its own lock, and MUST register any replacement worker
// under that same lock after re-checking `stopped`, while the panicked worker is still
// counted — that ordering is what closes the Stop/Wait race.
func GoTrackedPreAdded(name string, onPanic PanicHandler, fn func(), panicPolicy func(), wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		if !runOnceRecovered(name, onPanic, fn) {
			return // clean return — the panic policy is only for a panicking exit
		}
		if panicPolicy != nil {
			// Guard the policy: a fault in the safe-state work is reported but must
			// not escape (it would bypass wg.Done via the outer defer only, but a
			// re-panic here would still be a second recovery site — keep it explicit).
			func() {
				defer func() {
					if r := recover(); r != nil {
						invokePanicHandler(name+".panicPolicy", r, onPanic)
					}
				}()
				panicPolicy()
			}()
		}
	}()
}

// runOnceRecovered runs fn with panic recovery, invoking onPanic on a panic (its own fault
// swallowed by invokePanicHandler). Returns whether fn panicked.
func runOnceRecovered(name string, onPanic PanicHandler, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			invokePanicHandler(name, r, onPanic)
		}
	}()
	fn()
	return false
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
				invokePanicHandler(name, r, onPanic)
			}()
			fn()
		}()
		if !panicked || !respawn {
			return
		}
		// time.NewTimer + explicit Stop on the ctx-cancel branch so a
		// fast-cancel during cooldown doesn't leave a live timer
		// behind until expiry. Bounded leak (one timer per respawn,
		// bounded by cooldown) but every other long-lived select in
		// the daemon uses NewTimer; consistency wins over brevity.
		t := time.NewTimer(time.Duration(respawnCooldown.Load()))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// invokePanicHandler calls onPanic with its own deferred recover, so
// a panic *inside* the handler (a closed-logger write, a nil-method
// call) degrades to a silent skip instead of escaping past the outer
// defer in runWithRespawn and crashing the daemon. The intent of
// safego is "long-lived workers can't take the process down"; a
// misbehaving onPanic must not be the loophole that breaks that
// promise.
func invokePanicHandler(name string, panicValue any, onPanic PanicHandler) {
	if onPanic == nil {
		return
	}
	defer func() { _ = recover() }()
	onPanic(name, panicValue, debug.Stack())
}
