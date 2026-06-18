package ft8

import (
	"context"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// captureLinger is how long the capture device stays open after the last
// /v1/ft8/events subscriber disconnects, before it is released. It absorbs
// reconnect churn — a page reload, a momentary network blip, or flipping the
// Operating Mode tab away and back — so the audio device isn't torn down and
// reacquired on every brief gap. Package-level var so tests can dial it down.
var captureLinger = 5 * time.Second

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

	// hub fans each slot's decode + occupancy events out to /v1/ft8/events SSE
	// subscribers and caches the latest of each for late-subscriber replay.
	// Owned by the Service; closed on Stop.
	hub *hub

	mu        sync.Mutex
	started   bool
	stopped   bool
	parentCtx context.Context // captured at Start; parent of each capture run

	// Capture is acquired on demand, not at Start: a capture session runs only
	// while ≥1 SSE subscriber is connected to /v1/ft8/events, and is released
	// (after captureLinger, to absorb reconnect churn) when the last one leaves.
	// So enabling FT8 holds no audio device until the operator actually opens
	// the FT8 view, and frees it when they navigate away. Sessions never
	// overlap — acquire/release are serialised under mu, and release drains the
	// previous session's goroutines before mu is dropped.
	subCount      int                // live /v1/ft8/events subscribers
	capturing     bool               // a capture session is currently running
	captureCancel context.CancelFunc // cancels the current capture run
	lingerTimer   *time.Timer        // pending release after the last unsubscribe
	wg            sync.WaitGroup     // scheduler + decoder of the current session

	// stopOnce + stopDone serialise concurrent Stop calls so the "Stop
	// returned, therefore stopped" contract holds for every caller.
	// Mirrors internal/bridge.Service.
	stopOnce sync.Once
	stopDone chan struct{}

	// ---- FT8 transmit (ADR 0030 step e1) ----
	// The TX path is independent of capture: a keyer (PTT, injected from the
	// bridge in cmd/smd via SetTxKeyer so internal/ft8 never imports
	// internal/bridge) plus an on-demand output device acquired on arm. Guarded
	// by txMu, NOT s.mu — TX and capture don't interact, and the bridge enforces
	// single-flight at the hardware level regardless. Disarmed at construction;
	// the operator arms explicitly (the gate before any FT8 RF). See servicetx.go.
	//
	// Lock order where both are taken: txMu first, then s.mu (base()); capture
	// code never takes txMu, and Stop disarms TX outside s.mu — so there is no
	// s.mu→txMu nesting.
	newPlayer func(deviceName string, deviceIndex int) (txPlayer, error)

	txMu       sync.Mutex
	keyer      TxKeyer
	txArmed    bool
	txInFlight bool
	txClosed   bool   // set on Stop; refuses further arming
	txMessage  string // message of the in-flight transmission ("" = none)
	txOffsetHz float64
	txLastErr  string // i18n code of the last failed transmission ("" = none)
	exchPath   string // operator's antenna-path choice for the active exchange ("S"/"L"); logging-only
	txDevice   txPlayer
	txCtrl     *TxController
	txCancel   context.CancelFunc
	txWg       sync.WaitGroup

	// seqGate serialises "start a session" (StartQso/StartCallCq) against
	// "disarm + abandon" (disarmTx) so the armed-check and the sequencer commit
	// are atomic w.r.t. a concurrent disarm (review M3). Without it, a start can
	// observe txArmed=true, a disarm can then abandon the idle sequencer and
	// clear txArmed, and the start can still commit an active session — leaving
	// an active sequencer after TX is disarmed. Outermost lock; order is
	// seqGate → txMu (→ s.mu via base()); held only across the brief
	// check-and-commit, never across the ~13 s audio (that runs in the tx
	// goroutine, which takes only txMu).
	seqGate sync.Mutex

	// seq is the manual sequencer (ADR 0031 step e3): one active answer-a-CQ
	// exchange, driven per slot from decodeLoop, transmitting via seqTransmit.
	// Created in newService; nil-safe via its own methods.
	seq *Sequencer

	// qsoLogger logs a completed exchange (ADR 0029 step e4); injected via
	// SetQsoLogger before Start (cmd/smd wires it to qsoservice). nil = not
	// logged. Read only in the seq.onComplete callback (decodeLoop goroutine,
	// created after wiring — happens-before holds).
	qsoLogger func(ctx context.Context, c CompletedQso)
}

// newService constructs a Service with an injected capture source. The
// exported daemon constructor (which builds the build-tag-selected real
// source) lands with the capture step; tests inject a fake source here.
func newService(cfg types.Ft8Config, log logging.Logger, src captureSource) *Service {
	var occOverride *types.Ft8OccupancyConfig
	if cfg.TX != nil {
		occOverride = cfg.TX.Occupancy
	}
	s := &Service{
		cfg:       cfg,
		occCfg:    resolveOccupancyConfig(occOverride),
		log:       log,
		src:       src,
		hub:       newHub(),
		stopDone:  make(chan struct{}),
		newPlayer: newTxPlayer, // build-tagged; CGO-free build returns ErrTxUnavailable
	}
	// The sequencer transmits via seqTransmit (current-slot late-dt) and fans its
	// state out on the ft8-qso SSE; both reference s, so wire it after s exists.
	s.seq = newSequencer(
		s.seqTransmit,
		func(st QsoStatus) { s.hub.publish(hubEvent{name: EventQso, payload: st}) },
		types.ResolveFt8MaxRepeats(cfg.TX),
		log,
	)
	// On a completed exchange, hand it to the injected logger (e4). Reads
	// s.qsoLogger at call time (set via SetQsoLogger before Start), so the
	// daemon wires logging after construction.
	s.seq.onComplete = func(c CompletedQso) {
		// Stamp the operator's antenna-path choice (logging-only) onto the
		// completed exchange just before the sink builds the QSO record. The
		// sequencer is path-agnostic; the choice lives on the Service (set via
		// the /v1/ft8/qso/path endpoint, defaulting to short per exchange).
		c.AntPath = s.exchangePath()
		if s.qsoLogger != nil {
			s.qsoLogger(s.base(), c)
		}
	}
	return s
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

// Start binds the subsystem to a parent context and marks it ready. ctx is
// typically the daemon's main lifecycle context. Idempotent — repeat calls
// are no-ops once started, and Stop-before-Start is terminal.
//
// Start does NOT acquire the audio device: capture is demand-driven and starts
// on the first /v1/ft8/events subscriber (see onSubscriberAdded). When
// cfg.Enabled is false, Start succeeds and the subsystem stays inert — no
// subscriber will ever trigger capture. Start never returns an error that
// would abort daemon startup; a capture that won't start later (no device,
// device busy, or the CGO-free build whose capture is unavailable) is logged
// and leaves the subsystem idle.
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
	s.parentCtx = ctx
	s.log.InfoWith().Msg("ft8: subsystem ready; capture starts on first /v1/ft8/events subscriber")
	return nil
}

// onSubscriberAdded is called when a /v1/ft8/events subscriber connects. The
// first subscriber (0→1) acquires the capture device; if a release is pending
// in its linger window, the live session is simply kept (timer cancelled).
// No-op when disabled, not yet started, or already stopped — capture only ever
// runs in the enabled, running window. Must hold no caller locks.
func (s *Service) onSubscriberAdded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subCount++
	if !s.started || s.stopped || !s.cfg.Enabled || s.subCount != 1 {
		return
	}
	if s.lingerTimer != nil {
		// A release was pending but capture is still live — keep it.
		s.lingerTimer.Stop()
		s.lingerTimer = nil
		return
	}
	s.startCaptureLocked()
}

// onSubscriberRemoved is called when a /v1/ft8/events subscriber disconnects.
// When the last one leaves (count→0) it schedules a release after captureLinger
// rather than tearing the device down immediately, so a quick reconnect reuses
// the live session.
func (s *Service) onSubscriberRemoved() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subCount > 0 {
		s.subCount--
	}
	if s.subCount != 0 || s.stopped || !s.capturing || s.lingerTimer != nil {
		return
	}
	s.lingerTimer = time.AfterFunc(captureLinger, s.onLingerExpired)
}

// onLingerExpired releases the capture device once the linger window passes
// with no subscribers. The subCount re-check makes it robust against a
// reconnect that raced the timer (onSubscriberAdded stops the timer, but if it
// had already fired we still see the new subscriber here and keep the session).
func (s *Service) onLingerExpired() {
	s.mu.Lock()
	s.lingerTimer = nil
	if s.subCount > 0 || s.stopped || !s.capturing {
		s.mu.Unlock()
		return
	}
	s.releaseCaptureLocked()
	s.mu.Unlock()

	// Attended-only: the last /v1/ft8/events subscriber is gone past the linger
	// window (the browser was closed, not briefly reloaded), so no operator is
	// attending FT8 — TX must not stay armed across browser sessions. Disarm
	// OUTSIDE s.mu: disarmTx takes its own txMu and may wait on an in-flight
	// transmission to drain (txWg); holding s.mu across that would stall subscriber
	// / capture accounting. Idempotent — a no-op on the capture-only path (TX not
	// armed); when armed it drops PTT and abandons any active sequenced QSO.
	s.disarmTx(false)
}

// startCaptureLocked acquires the capture source and spawns the scheduler +
// decode worker for one session. Fail-soft: a source that won't start leaves
// capturing=false (the subsystem stays idle, logged) rather than erroring.
// Caller holds s.mu.
func (s *Service) startCaptureLocked() {
	runCtx, cancel := context.WithCancel(s.parentCtx)
	samples, err := s.src.Start(runCtx)
	if err != nil {
		cancel()
		s.log.WarnWith().Err(err).Msg("ft8: capture unavailable; subsystem idle")
		return
	}
	s.captureCancel = cancel
	s.capturing = true

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
		Msg("ft8: subscriber present; capture started, decoding live slots")
}

// releaseCaptureLocked cancels the current capture session, releases the
// device, and drains the scheduler + decode goroutines before returning, so
// the next acquisition never overlaps a still-running session. No-op when not
// capturing. Caller holds s.mu (the drain happens under the lock, which serial-
// ises a racing onSubscriberAdded behind it — bounded by one in-flight decode).
func (s *Service) releaseCaptureLocked() {
	if !s.capturing {
		return
	}
	if s.captureCancel != nil {
		s.captureCancel()
		s.captureCancel = nil
	}
	if err := s.src.Stop(); err != nil {
		s.log.WarnWith().Err(err).Msg("ft8: capture stop error")
	}
	s.wg.Wait()
	s.capturing = false
	// Invalidate the Band Activity / occupancy replay cache: with the session
	// ended, a later subscriber must not be handed this session's last slot (it
	// would show stale decodes when the rig is off and capture can't reacquire).
	s.hub.clearActivity()
	s.log.InfoWith().Msg("ft8: no subscribers; capture released")
}

// osdEnabled resolves the OSD decode option. nil (config absent) → true, the
// default; applyDefaults normally fills it, so nil here only happens if a
// Service is built without going through config load.
func (s *Service) osdEnabled() bool {
	return s.cfg.EnableOSD == nil || *s.cfg.EnableOSD
}

// Stop marks the subsystem stopped, releases any live capture device, and
// waits for its scheduler + decode goroutines to drain. Idempotent under
// sequential and concurrent calls. An in-flight decode (go-ft8 is not
// cancellable) is allowed to finish before Stop returns — bounded by one
// slot's decode time.
func (s *Service) Stop() error {
	s.stopOnce.Do(func() {
		defer close(s.stopDone)

		// Disarm TX first: drops PTT if a transmission is mid-flight and closes
		// the output device. Done before taking s.mu — disarm serialises on txMu
		// and waits on the TX goroutine, so it must not nest under s.mu.
		s.disarmTx(true)

		s.mu.Lock()
		s.stopped = true
		if s.lingerTimer != nil {
			s.lingerTimer.Stop()
			s.lingerTimer = nil
		}
		s.releaseCaptureLocked() // cancels + stops + drains; no-op if idle
		s.mu.Unlock()

		// Disconnect any ft8 SSE subscribers so they return promptly rather
		// than waiting on the daemon's graceful timeout.
		s.hub.close()
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
	// Previously-recommended top offset, carried across slots for the
	// clear-offset hysteresis (stickySuggested): the ★ recommendation stays put
	// while it remains clear instead of hopping to a marginally wider gap each
	// slot. Loop-local, so it resets naturally when a new capture session starts.
	prevTop := 0
	for slot := range slots {
		msgs := DecodeSlot(slot.Samples, osd, s.log)
		ref := SlotRefFromTime(slot.StartUTC)

		// Publish the decode feed (live Band Activity) and the occupancy report
		// (TX-offset picker) for the same slot. Independent SSE events on one
		// stream; order between them doesn't matter to the SPA.
		s.hub.publish(hubEvent{name: EventDecode, payload: newDecodeReport(ref, msgs)})

		rep := Occupancy(ref, slot.Samples, msgs, s.occCfg)
		rep.Suggested = stickySuggested(rep.Suggested, rep.Occupied, s.occCfg, prevTop)
		if len(rep.Suggested) > 0 {
			prevTop = rep.Suggested[0]
		} else {
			prevTop = 0
		}
		s.hub.publish(hubEvent{name: EventOccupancy, payload: rep})

		// Drive the manual sequencer (ADR 0031): feed this slot's decodes to the
		// active exchange and let it transmit the next rung in the current
		// (opposite-parity) slot. No-op when no QSO is active.
		s.seq.OnSlot(ref, msgs, time.Now().UTC())

		s.log.DebugWith().
			Str("slot", ref.StartUTC).
			Int("decodes", len(msgs)).
			Int("occupied", len(rep.Occupied)).
			Int("suggested", len(rep.Suggested)).
			Msg("ft8 slot processed")
	}
}

// LatestOccupancy returns the most recent per-slot occupancy report, or nil if
// no full slot has been processed yet. Safe for concurrent use.
func (s *Service) LatestOccupancy() *OccupancyReport {
	if s == nil || s.hub == nil {
		return nil
	}
	return s.hub.latestOccupancy()
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
