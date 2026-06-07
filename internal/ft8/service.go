package ft8

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// captureSource is the live-audio seam: it produces a stream of int16 PCM
// sample batches (12 kHz mono) and runs until Stop. The real implementation
// (miniaudio via malgo, behind //go:build cgo) is wired in a later step;
// the Service depends only on this interface so the whole capture → ring →
// scheduler → decode pipeline is testable without audio hardware or CGO.
type captureSource interface {
	// Start begins capture and returns the channel of sample batches. The
	// channel is closed when capture stops (Stop, ctx cancel, or a fatal
	// device error).
	Start(ctx context.Context) (<-chan []int16, error)
	// Stop halts capture and releases the device. Idempotent; safe to call
	// even if Start failed or was never called.
	Stop() error
}

// Service is the FT8 subsystem. When Enabled, Start acquires the capture
// source, drives it through the slot Scheduler, and decodes each completed
// 15-second slot via DecodeSlot — logging "heard this" lines (a decode is
// NOT a QSO; nothing is written to storage or the upload queue).
//
// Fail-soft throughout: a capture that won't start leaves the subsystem
// idle (logged, not fatal), and decode/scheduler goroutines run under
// safego so a panic can never take the daemon down. An FT8 failure must
// never stop the operator logging.
//
// Lifecycle: Initialize() validates deps; Start(ctx) spawns the subsystem
// goroutines; Stop() cancels, releases the device, and waits. All
// idempotent per the project's service-lifecycle pattern. Config is read
// once and snapshotted at construction — operator restart picks up edits,
// matching internal/bridge.
type Service struct {
	cfg    types.Ft8Config
	occCfg types.Ft8OccupancyConfig // resolved occupancy/offset-ranking tuning
	log    logging.Logger
	src    captureSource

	// latestOcc holds the most recent per-slot occupancy report for the
	// clear-offset picker. Lock-free single-writer (decodeLoop) / many-reader
	// (the SSE layer); nil until the first full slot is processed.
	latestOcc atomic.Pointer[OccupancyReport]

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// stopOnce + stopDone serialise concurrent Stop calls so the "Stop
	// returned, therefore stopped" contract holds for every caller.
	// Mirrors internal/bridge.Service.
	stopOnce sync.Once
	stopDone chan struct{}
}

// newService constructs a Service with an injected capture source. The
// exported daemon constructor (which builds the build-tag-selected real
// source) lands with the capture step; tests inject a fake source here.
func newService(cfg types.Ft8Config, log logging.Logger, src captureSource) *Service {
	var occOverride *types.Ft8OccupancyConfig
	if cfg.TX != nil {
		occOverride = cfg.TX.Occupancy
	}
	return &Service{
		cfg:      cfg,
		occCfg:   resolveOccupancyConfig(occOverride),
		log:      log,
		src:      src,
		stopDone: make(chan struct{}),
	}
}

// Initialize validates dependencies. Idempotent.
func (s *Service) Initialize() error {
	const op errors.Op = "ft8.Service.Initialize"
	if s.log == nil {
		return errors.New(op).WithMsg("logger has not been set")
	}
	if s.cfg.Enabled && s.src == nil {
		return errors.New(op).WithMsg("ft8.enabled=true but no capture source has been set")
	}
	return nil
}

// Start binds the subsystem to a parent context and, when Enabled, acquires
// the capture source and spawns the scheduler + decode worker. ctx is
// typically the daemon's main lifecycle context. Idempotent — repeat calls
// are no-ops once started, and Stop-before-Start is terminal.
//
// When cfg.Enabled is false, Start succeeds without acquiring anything (the
// default deployment). When Enabled but the capture source won't start
// (no device, device busy, or this is the CGO-free build whose capture is
// unavailable), Start logs a warning and leaves the subsystem idle — it
// never returns an error that would abort daemon startup.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.started {
		return nil
	}
	s.started = true

	if !s.cfg.Enabled {
		s.log.InfoWith().Msg("ft8: subsystem disabled (ft8.enabled=false); decoder not started")
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	samples, err := s.src.Start(runCtx)
	if err != nil {
		cancel()
		s.log.WarnWith().Err(err).Msg("ft8: capture unavailable; subsystem idle")
		return nil
	}

	sch := NewScheduler(samples, s.log)
	safego.GoTracked(runCtx, "ft8.scheduler", s.onPanic, func() {
		_ = sch.Run(runCtx)
	}, false, &s.wg)
	safego.GoTracked(runCtx, "ft8.decoder", s.onPanic, func() {
		s.decodeLoop(sch.Slots())
	}, false, &s.wg)

	s.log.InfoWith().
		Str("device", s.cfg.Device).
		Bool("osd", s.osdEnabled()).
		Msg("ft8: subsystem started; decoding live slots")
	return nil
}

// osdEnabled resolves the OSD decode option. nil (config absent) → true, the
// default; applyDefaults normally fills it, so nil here only happens if a
// Service is built without going through config load.
func (s *Service) osdEnabled() bool {
	return s.cfg.EnableOSD == nil || *s.cfg.EnableOSD
}

// Stop cancels the run context, releases the capture device, and waits for
// the scheduler + decode goroutines to drain. Idempotent under sequential
// and concurrent calls. An in-flight decode (go-ft8 is not cancellable) is
// allowed to finish before Stop returns — bounded by one slot's decode time.
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		defer close(s.stopDone)

		s.mu.Lock()
		cancel := s.cancel
		started := s.started
		s.stopped = true
		s.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		if started && s.cfg.Enabled && s.src != nil {
			if err := s.src.Stop(); err != nil {
				s.log.WarnWith().Err(err).Msg("ft8: capture stop error")
			}
		}
		s.wg.Wait()
		s.log.InfoWith().Msg("ft8: subsystem stopped")
	})
	<-s.stopDone
	return nil
}

// Enabled reports whether the FT8 subsystem is configured to run. Nil-safe.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// decodeLoop consumes completed slots and decodes each, then computes the
// per-slot occupancy report (the clear-offset picker's input) from the same
// samples + decodes and publishes it as the latest. DecodeSlot is itself
// fail-soft (it recovers per-slot panics), so a bad slot can't break the
// loop; the safego wrapper around this loop is belt-and-braces. Occupancy is
// cheap (one averaged FFT per slot, ~tens of ms) next to the decode, so it
// adds negligible time to the slot budget.
func (s *Service) decodeLoop(slots <-chan Slot) {
	osd := s.osdEnabled()
	for slot := range slots {
		msgs := DecodeSlot(slot.Samples, osd, s.log)
		rep := Occupancy(SlotRefFromTime(slot.StartUTC), slot.Samples, msgs, s.occCfg)
		s.latestOcc.Store(&rep)
		s.log.DebugWith().
			Str("slot", rep.Slot.StartUTC).
			Int("occupied", len(rep.Occupied)).
			Int("suggested", len(rep.Suggested)).
			Msg("ft8 occupancy computed")
	}
}

// LatestOccupancy returns the most recent per-slot occupancy report, or nil if
// no full slot has been processed yet. Safe for concurrent use — the SSE layer
// (the ft8-occupancy event and its replay cache) reads it here.
func (s *Service) LatestOccupancy() *OccupancyReport {
	if s == nil {
		return nil
	}
	return s.latestOcc.Load()
}

// onPanic logs a panic that escaped one of the subsystem goroutines. safego
// has already recovered it — the daemon stays up; this records what happened.
func (s *Service) onPanic(name string, panicValue any, stack []byte) {
	s.log.ErrorWith().
		Str("goroutine", name).
		Interface("panic", panicValue).
		Bytes("stack", stack).
		Msg("ft8: subsystem goroutine panicked (recovered)")
}
