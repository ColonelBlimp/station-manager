// Package refresher provides the bounded async-refresh worker that
// implements lookup.AsyncRefresher per ADR 0017.
//
// When the orchestrator's read pipeline serves a stale row, it
// schedules a background refresh via this worker. Refreshes run
// concurrently up to MaxInFlight; further schedule requests while
// at capacity are dropped with a log line rather than queued
// unbounded — under pathological load (rapid Tab pile-up during a
// flaky-internet outage) we'd rather drop refreshes than hoard
// goroutines.
//
// Lifecycle is the standard project shape:
//
//	Initialize() — validate config, set defaults
//	Start(ctx)   — bind to a parent ctx tied to daemon lifetime
//	Stop()       — cancel parent ctx, wait for in-flight refreshes
//
// Stop is idempotent and waits for every running refresh to either
// complete or notice its (now-cancelled) ctx; that's the daemon-
// shutdown drain contract.
package refresher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/safego"
)

// ServiceName is the DI bean ID used in iocdi wiring.
const ServiceName = "lookuprefreshservice"

// DefaultMaxInFlight is used when MaxInFlight is unset or non-positive.
// Picked to be generous for personal-operator scale while still
// bounded enough to prevent runaway goroutines on a flaky upstream.
const DefaultMaxInFlight = 4

// goroutineLabel is the name passed to safego.GoTracked so panics
// from refresh fns log with a recognizable origin.
const goroutineLabel = "lookup.refresher"

// Compile-time check that Service satisfies lookup.AsyncRefresher.
var _ lookup.AsyncRefresher = (*Service)(nil)

// Service is the bounded async-refresh worker.
//
// Schedule is safe to call from any goroutine, including before
// Start (drops with a warning) and after Stop (drops with a
// warning). The orchestrator's stale-hit branch calls Schedule
// without coordinating lifecycle — keeping the no-op-when-not-
// running behaviour here means the orchestrator stays simple.
type Service struct {
	LoggerService *logging.Service `di.inject:"loggingservice"`

	// MaxInFlight bounds the concurrent refresh count. Zero or
	// negative is treated as DefaultMaxInFlight. Operators can tune
	// via config (task #62 wires this from the operator config).
	MaxInFlight int

	parentCtx context.Context
	cancel    context.CancelFunc
	sem       chan struct{}
	wg        sync.WaitGroup

	// mu serialises Schedule's started/stopped check + the synchronous
	// wg.Add inside safego.GoTracked against Stop's "set stopped, then
	// wg.Wait" sequence. Without it, a Schedule that interleaves with
	// a concurrent Stop can issue wg.Add(1) AFTER Stop has already
	// observed counter == 0 inside wg.Wait — and sync.WaitGroup
	// semantics require an Add-with-positive-delta-while-counter-zero
	// to happen-before any concurrent Wait. The Go runtime detects
	// the violation and panics with "sync: WaitGroup misuse: Add
	// called concurrently with Wait." Holding mu across both the
	// state check and the GoTracked-internal Add closes that window;
	// Stop's mu.Lock then drains any in-flight Schedule before
	// reaching wg.Wait, so the Wait sees a complete counter.
	mu      sync.Mutex
	started bool // protected by mu
	stopped bool // protected by mu

	inFlight atomic.Int64
	dropped  atomic.Int64
}

// Initialize validates dependencies and sets defaults. Idempotent.
func (s *Service) Initialize() error {
	const op errors.Op = "refresher.Service.Initialize"
	if s.LoggerService == nil {
		return errors.New(op).WithMsg("logger service has not been set/injected")
	}
	if s.MaxInFlight <= 0 {
		s.MaxInFlight = DefaultMaxInFlight
	}
	return nil
}

// Start binds the worker to a parent context. ctx is typically the
// daemon's main lifecycle context — when it's cancelled (or Stop is
// called), the worker stops accepting new refreshes and waits for
// in-flight ones to finish.
//
// Idempotent — repeat calls are no-ops once started.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.MaxInFlight <= 0 {
		// Defensive — Initialize sets this, but a caller skipping
		// Initialize and going straight to Start gets the default
		// rather than a deadlock-on-zero-cap-channel.
		s.MaxInFlight = DefaultMaxInFlight
	}
	s.parentCtx, s.cancel = context.WithCancel(ctx)
	s.sem = make(chan struct{}, s.MaxInFlight)
	s.started = true
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().
			Int("max_in_flight", s.MaxInFlight).
			Msg("refresher: started")
	}
	return nil
}

// Stop cancels the parent context and waits for in-flight refreshes
// to finish. Idempotent.
//
// Refresh fns receive the cancelled ctx via the parent context
// passed to Schedule's fn — well-behaved fns honour cancellation
// (e.g., propagate ctx into upstream HTTP calls) and return promptly.
// Stop has no hard deadline; if a fn ignores ctx, Stop blocks until
// it returns. This is intentional — the alternative (force-kill
// after timeout) doesn't compose with goroutines, and the project's
// daemon shutdown is operator-triggered (no SLA pressure).
//
// Concurrency contract: Stop holds mu briefly to flip stopped and
// snapshot cancel, then releases it before wg.Wait. The Lock-Unlock
// pair acts as a barrier — any Schedule that beat Stop to the lock
// has already completed its wg.Add by the time Stop's Lock returns,
// so wg.Wait sees the full counter. Schedules arriving after Stop's
// Unlock observe stopped=true and drop without touching the
// WaitGroup.
func (s *Service) Stop() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().
			Int64("dropped_total", s.dropped.Load()).
			Msg("refresher: stopped")
	}
	return nil
}

// Schedule runs fn in the background up to MaxInFlight concurrent
// fns. Returns immediately. fn receives the parent context — when
// the daemon shuts down (Stop is called), the ctx is cancelled.
//
// Drop semantics:
//
//   - Before Start or after Stop: dropped with a warning.
//   - At MaxInFlight capacity: dropped with a warning. The next Tab
//     against the same key will trigger another refresh attempt
//     per ADR 0017's implicit-fall-through model.
//
// Panic safety: fn is run under safego.GoTracked, which recovers
// any panic and logs it via onPanic. The semaphore slot is released
// in a deferred block so a panicking fn doesn't leak the slot.
//
// Concurrency contract: the started/stopped check, the semaphore
// acquire, and the synchronous wg.Add inside safego.GoTracked all
// run under mu. Stop's matching Lock waits for any in-flight
// Schedule to release before reaching wg.Wait, so we never call
// wg.Add(1) after Stop has observed counter == 0 in wg.Wait — the
// race that the Go runtime would detect as "WaitGroup misuse: Add
// called concurrently with Wait."
func (s *Service) Schedule(fn func(ctx context.Context)) {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		s.dropped.Add(1)
		if s.LoggerService != nil {
			s.LoggerService.WarnWith().Msg("refresher: schedule rejected — service not running")
		}
		return
	}

	select {
	case s.sem <- struct{}{}:
		// got a slot
	default:
		s.mu.Unlock()
		s.dropped.Add(1)
		if s.LoggerService != nil {
			s.LoggerService.WarnWith().
				Int("max_in_flight", s.MaxInFlight).
				Int64("dropped_total", s.dropped.Load()).
				Msg("refresher: schedule dropped — at capacity")
		}
		return
	}

	s.inFlight.Add(1)
	// safego.GoTracked does wg.Add(1) synchronously, then spawns
	// the goroutine. Holding mu across this call is the load-bearing
	// piece of the lifecycle race fix — Stop's mu.Lock can't sneak
	// between our state check and the Add.
	safego.GoTracked(s.parentCtx, goroutineLabel, s.onPanic, func() {
		defer func() {
			<-s.sem
			s.inFlight.Add(-1)
		}()
		fn(s.parentCtx)
	}, false /* respawn — refresh fns are one-shot */, &s.wg)
	s.mu.Unlock()
}

// InFlight returns the current count of in-flight refreshes. Useful
// for diagnostics / future metrics endpoint.
func (s *Service) InFlight() int64 { return s.inFlight.Load() }

// Dropped returns the cumulative count of schedule requests rejected
// (capacity full or service not running).
func (s *Service) Dropped() int64 { return s.dropped.Load() }

// onPanic is the safego panic handler — logs the recovered panic
// with stack. Refresh fns that panic don't crash the daemon; the
// next Tab will trigger another refresh attempt.
func (s *Service) onPanic(name string, p any, stack []byte) {
	if s.LoggerService == nil {
		return
	}
	s.LoggerService.ErrorWith().
		Str("worker", name).
		Str("panic", fmt.Sprintf("%v", p)).
		Bytes("stack", stack).
		Msg("refresher: scheduled fn panicked")
}
