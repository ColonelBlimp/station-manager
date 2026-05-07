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

	started atomic.Bool
	stopped atomic.Bool

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
	const op errors.Op = "refresher.Service.Start"
	if !s.started.CompareAndSwap(false, true) {
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
	if s.LoggerService != nil {
		s.LoggerService.InfoWith().
			Int("max_in_flight", s.MaxInFlight).
			Msg("refresher: started")
	}
	_ = op
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
func (s *Service) Stop() error {
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
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
func (s *Service) Schedule(fn func(ctx context.Context)) {
	if !s.started.Load() || s.stopped.Load() {
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
	safego.GoTracked(s.parentCtx, goroutineLabel, s.onPanic, func() {
		defer func() {
			<-s.sem
			s.inFlight.Add(-1)
		}()
		fn(s.parentCtx)
	}, false /* respawn — refresh fns are one-shot */, &s.wg)
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
