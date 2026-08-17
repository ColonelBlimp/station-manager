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
	"github.com/ColonelBlimp/station-manager/internal/lifecycle"
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

	// life is the ADR-0070 Supervisor — the lifecycle authority (replaces parentCtx/cancel + wg + the
	// mu-guarded started/stopped flags). refreshLane (Cancellable) tracks the per-Schedule refresh
	// goroutines: Admit does the Running check + count ATOMICALLY against Stop's seal (the old
	// Add-vs-Wait race fix, now on the supervisor's mutex), and Stop cancels the context (in-flight
	// refreshes notice) then waits the lane. Constructed lazily via ensureLifecycle because Service is
	// built through a struct literal (di.inject), not New — so a pre-Start Schedule/Stop cannot hit
	// nil supervisor fields.
	life        *lifecycle.Supervisor
	refreshLane *lifecycle.Lane
	lifeOnce    sync.Once

	// sem bounds concurrent refreshes; created in launch (before the Running commit) and immutable
	// thereafter — read by Schedule (channel ops need no mutex).
	sem chan struct{}

	inFlight atomic.Int64
	dropped  atomic.Int64
}

// ensureLifecycle lazily builds the supervisor + refresh lane exactly once. Called at the head of
// Initialize, Start, Schedule and Stop so a caller that skips Initialize, or calls Schedule/Stop
// before Start, never dereferences a nil supervisor. RegisterLane runs here (before any Start), so
// the lane is always registered before the transition.
func (s *Service) ensureLifecycle() {
	s.lifeOnce.Do(func() {
		s.life = lifecycle.New()
		s.refreshLane = s.life.RegisterLane("refresh", lifecycle.Cancellable)
	})
}

// Initialize validates dependencies and sets defaults. Idempotent.
func (s *Service) Initialize() error {
	s.ensureLifecycle()
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
	s.ensureLifecycle()
	if ctx == nil {
		ctx = context.Background()
	}
	// No acquire (nothing fallible to open); launch resolves the MaxInFlight default + creates the
	// semaphore + logs. Idempotent once Running; a nil no-op once Stopped (terminal).
	return s.life.Start(ctx, nil, s.launch)
}

// launch resolves the MaxInFlight default, creates the concurrency semaphore, and logs the start. It
// runs INSIDE the supervisor's serialized start transition (once), so the default resolution can't
// race two concurrent first Starts (codex P2 on 3cb6a9a8) — the old s.mu covered this. The sem is
// immutable after the Running commit. There is no long-lived worker to Track — refresh goroutines are
// admitted per Schedule on the refresh lane.
func (s *Service) launch(_ context.Context, _ *lifecycle.StartScope) {
	if s.MaxInFlight <= 0 {
		// Defensive — Initialize sets this, but a caller skipping Initialize and going straight to
		// Start gets the default rather than a deadlock-on-zero-cap-channel.
		s.MaxInFlight = DefaultMaxInFlight
	}
	s.sem = make(chan struct{}, s.MaxInFlight)
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().
			Int("max_in_flight", s.MaxInFlight).
			Msg("refresher: started")
	}
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
// Concurrency contract: the supervisor's seal (Stopping) closes admission under the same mutex
// Admit reads, then Stop cancels the context and waits the refresh lane. A Schedule that beat the
// seal has already done its lane Admit (count++) and is awaited; one arriving after the seal is
// refused and never touches the lane counter — the Admit/seal atomicity that replaces the old
// mu-guarded started/stopped-check + wg.Add barrier.
func (s *Service) Stop() error {
	s.ensureLifecycle()
	return s.life.Stop(s.teardown)
}

// teardown runs once, after the supervisor sealed admission (Schedule now drops), cancelled the
// context (in-flight refreshes notice), and waited the refresh lane (they finished). It just records
// the stop; there is no resource to close.
func (s *Service) teardown() error {
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
// Panic safety: fn is run under safego.GoCompletion, which recovers any panic and logs it via
// onPanic. The semaphore slot is released in the completion callback so a panicking fn doesn't leak
// the slot.
//
// Concurrency contract: the refresh lane's Admit does the Running check + the count in one atomic
// step against Stop's seal (both under the supervisor's mutex), so the pre-migration race — a
// started/stopped check passing just before Stop, then a wg.Add landing after Stop's wg.Wait saw
// zero — cannot occur: once sealed, Admit refuses, and Stop's lane-wait sees a complete counter.
func (s *Service) Schedule(fn func(ctx context.Context)) {
	s.ensureLifecycle()
	// Admit performs the Running check + the lane count ATOMICALLY against Stop's seal (the old
	// started/stopped-check + wg.Add-under-mu race fix, now on the supervisor's mutex). Refused before
	// Start / once sealed ⇒ drop with a warning.
	done, ok := s.refreshLane.Admit()
	if !ok {
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
		done() // release this admit — every successful Admit releases EXACTLY once (here, on capacity)
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
	// The refresh binds to the supervisor context (cancelled at Stop, so a well-behaved fn returns
	// promptly). GoCompletion signals the lane's done ONCE on permanent exit — the OTHER release path
	// for this admit — with the semaphore + in-flight release folded in.
	ctx := s.life.Context()
	safego.GoCompletion(ctx, goroutineLabel, s.onPanic, func() { fn(ctx) }, false /* one-shot */, func() {
		<-s.sem
		s.inFlight.Add(-1)
		done()
	})
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
