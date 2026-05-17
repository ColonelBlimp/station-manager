package ft8

import (
	"context"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Service is the FT8 subsystem (per ADR 0021). v1 is a SCAFFOLD —
// the decoder pipeline lands in subsequent sessions as a spec-
// implementation against the QEX 2020 protocol paper (Franke /
// Somerville / Taylor; see ADR 0021 § Licensing constraint and
// docs/v2-design/milestones.md § Milestone 4 for why the WSJT-X
// Fortran source tree is not the reference). What's here is the
// lifecycle skeleton and the package-boundary discipline so future
// commits have somewhere to land cleanly.
//
// Lifecycle: Initialize() validates config; Start(ctx) spawns the
// subsystem goroutines; Stop() cancels and waits. All idempotent
// per the project's service-lifecycle pattern.
//
// When cfg.Enabled is false, Initialize/Start/Stop are still safe to
// call but no goroutine is spawned and no audio devices are
// acquired. This is the default — FT8 stays opt-in for v1 while the
// implementation matures.
//
// Once decoder work lands, the goroutine spawned by Start owns the
// audio capture → DSP → decode pipeline. Until then it's a stub
// goroutine that just waits for ctx cancellation so the lifecycle
// contract is exercised end-to-end and Stop's wg.Wait() has
// something concrete to wait on. Replace the stub at the same
// commit that introduces the first real worker.
type Service struct {
	cfg    types.Ft8Config
	logger *logging.Service

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// stopOnce + stopDone serialise concurrent Stop calls so the
	// "Stop returned, therefore, stopped" contract holds for every
	// caller. Mirrors the pattern in internal/bridge.Service.
	stopOnce sync.Once
	stopDone chan struct{}
}

// New constructs a Service from the operator's FT8 config and a
// logger. Config is read once and snapshotted; runtime PUT /v1/config
// changes don't reach an existing Service. Operator restart picks
// up edits — the same pattern as internal/bridge and internal/email.
func New(cfg types.Ft8Config, logger *logging.Service) *Service {
	return &Service{
		cfg:      cfg,
		logger:   logger,
		stopDone: make(chan struct{}),
	}
}

// Initialize validates dependencies. Idempotent. Required by the
// project's service-lifecycle pattern; today the only check is
// "logger present" (config was validated at config.Load time, so
// we don't re-check here).
func (s *Service) Initialize() error {
	const op errors.Op = "ft8.Service.Initialize"
	if s.logger == nil {
		return errors.New(op).WithMsg("logger service has not been set")
	}
	return nil
}

// Start binds the subsystem to a parent context and (when Enabled)
// spawns the placeholder goroutine. ctx is typically the daemon's
// main lifecycle context — when it's cancelled (or Stop is called),
// the subsystem exits cleanly.
//
// Idempotent — repeat calls are no-ops once started.
//
// When cfg.Enabled is false, Start succeeds without spawning
// anything; suits the "operator without FT8 hardware" deployment
// shape, which is the v1 default.
//
// Stop-before-Start puts the Service in a terminal state — a
// subsequent Start returns silently rather than spinning up a stub
// that has nowhere to land its work.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	if s.started {
		return nil
	}
	s.started = true
	if !s.cfg.Enabled {
		s.logger.InfoWith().Msg("ft8: subsystem disabled (ft8.enabled=false); decoder not started")
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.runPlaceholder(runCtx)
	s.logger.InfoWith().Msg("ft8: subsystem started (SCAFFOLD — decoder pipeline not yet wired; see ADR 0021)")
	return nil
}

// Stop cancels the parent context, waits for in-flight goroutines,
// and returns. Idempotent under both sequential and concurrent
// calls — the first Stop runs the teardown; subsequent callers
// (whether sequential or racing) wait until the first has
// finished, then return nil. "Stop returned, therefore, stopped"
// holds for every caller.
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		defer close(s.stopDone)

		s.mu.Lock()
		cancel := s.cancel
		s.stopped = true
		s.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		s.wg.Wait()
		if s.logger != nil {
			s.logger.InfoWith().Msg("ft8: subsystem stopped")
		}
	})
	<-s.stopDone
	return nil
}

// Enabled reports whether the FT8 subsystem is configured to run.
// Used by api.Server (eventually) to decide whether to register any
// future FT8-facing routes. Nil-safe (returns false on a nil
// receiver).
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// runPlaceholder is the v1 stub goroutine — it does nothing except
// wait for ctx cancellation. Exists so Stop's wg.Wait() has
// something to wait on, and the lifecycle contract is exercised
// end-to-end even before the real decoder lands.
//
// **Replace at the same commit that introduces the first real
// worker** (audio capture loop / decoder pool / etc.). When that
//
//	happens, this method either grows into the real supervisor or is
//
// deleted in favour of a `go s.runSupervisor(ctx)`-style entry,
// matching whatever shape the spec-implementation work settles on.
func (s *Service) runPlaceholder(ctx context.Context) {
	defer s.wg.Done()
	<-ctx.Done()
}
