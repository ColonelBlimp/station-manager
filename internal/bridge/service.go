package bridge

import (
	"context"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Service is the bridge subsystem (ADR 0013, ADR 0019). Owns the
// internal pub/sub hub for rig events and (when Enabled) the
// serial+CAT pipeline goroutine that decodes AUTO-mode rig pushes
// into typed events.
//
// Lifecycle: Initialize() validates config; Start(ctx) spawns the
// publisher goroutine; Stop() cancels and waits. All idempotent per
// the project's service-lifecycle pattern. The pipeline holds the
// rig's serial port for the daemon's full lifetime — operators
// expect "smd is running, rig is connected" not "rig only acquired
// when a SPA tab happens to be open."
//
// Startup-time bridge-error events (unknown driver, port permission
// denied, etc.) fire to a hub-cached slot in addition to current
// subscribers, so a SPA tab opening AFTER the pipeline failed at
// startup still sees the toast. See hub.lastBridgeError.
//
// When cfg.Enabled is false, Initialize/Start/Stop are still safe to
// call but no goroutine is spawned and no events are published.
// Subscribe() returns a real (but never-published-to) channel. SSE
// handlers see an empty stream — correct behaviour for the master-
// smd / headless deployment shape where the daemon binary embeds
// the bridge subsystem but it stays inert.
type Service struct {
	cfg    types.BridgeConfig
	logger *logging.Service
	hub    *hub

	// openClient produces the serial client the pipeline reads from
	// and writes to. Defaults to a thin wrapper around serial.Open;
	// in-package tests substitute a fakeSerial to drive scenarios
	// without real hardware. Field rather than package-level var so
	// parallel tests don't race for ownership of a global hook.
	openClient func(serial.Config) (serial.Client, error)

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// activeClient is the live serial client owned by runPipeline.
	// Set after the port opens, cleared on pipeline exit. Used by
	// TriggerBootstrap so the SSE handler can write the rigdef's
	// READ command without owning its own client. Mu-guarded for
	// the pipeline-goroutine ↔ handler-goroutines race.
	//
	// Writes via this field are safe per serial.Client's contract
	// (WriteCommandBytes serialises through the port's writeMu).
	// Reads aren't done through this field — readLoop owns reads via
	// its own closure variable.
	activeClient serial.Client

	// bootstrapBytes is the pre-encoded READ command (per rigdef)
	// that TriggerBootstrap writes on each new SSE-open. Cached on
	// the Service so each Subscribe doesn't repeat cat.Encode.
	bootstrapBytes []byte

	// identityConfirmed records whether the connected rig has positively
	// identified as the configured driver (an IDENTITY push matching
	// def.Model). It gates the operator write paths — SendCommands and
	// StartTune refuse while false — so a wrong rig (driver typo), an
	// unrecognised ID, or a rig that never sends a parseable ID can't be
	// driven by commands or keyed for a tune (H2, review 2026-06-04). A
	// definite mismatch additionally halts the pipeline (exitPermanent);
	// the unrecognised / never-identified cases keep reading state for
	// display but stay write-blocked. Reset to false on pipeline teardown,
	// so each pipeline instance re-verifies. Mu-guarded.
	identityConfirmed bool

	// lastPublishedExitKey is the dedup token for the supervisor's
	// retry loop. Each exit-causing publish (publishExitBridgeError /
	// publishExitDisconnect) compares against this key and suppresses
	// the publish if it matches — so the operator sees ONE toast per
	// failure cycle even when the supervisor retries the same failed
	// open every 1–30s. Key is "<event-name>:<code>".
	//
	// runSupervisor clears the key when a pipeline run survives past
	// supervisorSteadyStateThreshold so the next failure surfaces
	// freshly. A different error code in mid-cycle (e.g. open succeeds
	// but INIT-write fails) also surfaces, because the key changes.
	//
	// Mu-guarded — publish helpers run on the pipeline goroutine while
	// runSupervisor reads/clears from its own context.
	lastPublishedExitKey string

	// stopOnce + stopDone serialise concurrent Stop calls so the
	// "Stop returned, therefore stopped" contract holds for every
	// caller. The first Stop runs the teardown work and closes
	// stopDone; subsequent (concurrent) Stops wait on stopDone before
	// returning. Without this the second concurrent caller could see
	// stopped=true and return while the first caller's wg.Wait /
	// hub.close were still in flight, breaking the idempotency
	// contract documented in Stop's doc-comment.
	stopOnce sync.Once
	stopDone chan struct{}

	// Timeout snapshot — captured at New from cfg.Timeouts with
	// package-var fallback for any zero/unset values. Read at runtime
	// by runSupervisor and readLoop, both running on goroutines spawned
	// by Start. Per-Service so future multi-rig can tune per rig.
	//
	// **Mutate ONLY before Start is called.** The supervisor goroutine
	// reads these fields without holding a lock, so post-Start mutation
	// would race under `-race`. Tests that want to dial timings down
	// have two safe entry points: (a) set the package-level defaults
	// (livenessTimeout, supervisorInitialBackoff, etc.) before calling
	// New — the values flow through resolveTimeout into these fields
	// at construction; (b) overwrite these fields directly between New
	// and Start when the test owns both lifecycle boundaries. Neither
	// pattern races the supervisor.
	livenessTimeout                time.Duration
	supervisorInitialBackoff       time.Duration
	supervisorMaxBackoff           time.Duration
	supervisorSteadyStateThreshold time.Duration

	// Tune-carrier state (ADR 0027), all mu-guarded. tuneActive is the
	// single-flight gate; tuneRestoreMode/Power are the pre-tune snapshot
	// captured at StartTune and restored on stop; tuneTimer is the hard
	// auto-off backstop. lastMode/lastPower are the rolling current-rig
	// snapshot fed by readLoop (frozen during a tune) — the source the
	// restore values are captured from, and the refuse-if-unknown gate.
	tuneActive       bool
	tuneRestoreMode  string
	tuneRestorePower int
	tuneTimer        *time.Timer
	lastMode         string
	lastPower        int

	// Resolved (config-overridable, hard-clamped) tune knobs, snapshotted at
	// New like the timeout fields above. tunePowerW ≤ maxTunePowerW;
	// tuneMaxDuration ≤ maxTuneDuration. Read by the tune controller; mutate
	// only before Start, same rule as the timeout fields.
	tunePowerW      int
	tuneMaxDuration time.Duration
}

// New constructs a Service from the operator's bridge config and a
// logger. Config is read once and snapshotted; runtime PUT /v1/config
// changes don't reach an existing Service. Operator restart picks up
// edits — same pattern as internal/email.
func New(cfg types.BridgeConfig, logger *logging.Service) *Service {
	// Clamp the tune knobs to their safe ceilings at construction (ADR 0027)
	// — config may lower them or raise up to the ceiling, but can't exceed it.
	// Warn so an operator whose value was capped sees why.
	tunePower, powerClamped := resolveTunePower(cfg.Tune.PowerW)
	tuneDur, durClamped := resolveTuneDuration(cfg.Tune.MaxDurationMs)
	if powerClamped && logger != nil {
		logger.WarnWith().
			Int("requested_w", cfg.Tune.PowerW).
			Int("clamped_w", tunePower).
			Msg("bridge: tune power_w above safe ceiling; clamped")
	}
	if durClamped && logger != nil {
		logger.WarnWith().
			Int("requested_ms", cfg.Tune.MaxDurationMs).
			Int("clamped_ms", int(tuneDur.Milliseconds())).
			Msg("bridge: tune max_duration_ms above safe ceiling; clamped")
	}
	return &Service{
		cfg:      cfg,
		logger:   logger,
		hub:      newHub(),
		stopDone: make(chan struct{}),
		openClient: func(c serial.Config) (serial.Client, error) {
			return serial.Open(c)
		},
		livenessTimeout:                resolveTimeout(cfg.Timeouts.LivenessMs, livenessTimeout),
		supervisorInitialBackoff:       resolveTimeout(cfg.Timeouts.BackoffInitialMs, supervisorInitialBackoff),
		supervisorMaxBackoff:           resolveTimeout(cfg.Timeouts.BackoffMaxMs, supervisorMaxBackoff),
		supervisorSteadyStateThreshold: resolveTimeout(cfg.Timeouts.SteadyStateThresholdMs, supervisorSteadyStateThreshold),
		tunePowerW:                     tunePower,
		tuneMaxDuration:                tuneDur,
	}
}

// resolveTimeout converts a config-supplied milliseconds value to a
// time.Duration, falling back to the package-level default when the
// config value is zero (the operator omitted the key — daemon picks
// the built-in default). Centralised so the four call sites in New
// stay terse.
func resolveTimeout(cfgMs int, defaultDur time.Duration) time.Duration {
	if cfgMs > 0 {
		return time.Duration(cfgMs) * time.Millisecond
	}
	return defaultDur
}

// Initialize validates dependencies. Idempotent. Required by the
// project's service-lifecycle pattern; today the only check is
// "logger present" (config was already validated by config.Load via
// validateBridge, so we don't re-check here).
func (s *Service) Initialize() error {
	const op errors.Op = "bridge.Service.Initialize"
	if s.logger == nil {
		return errors.New(op).WithMsg("logger service has not been set")
	}
	return nil
}

// Start binds the subsystem to a parent context and (when Enabled)
// spawns the publisher goroutine. ctx is typically the daemon's main
// lifecycle context — when it's cancelled (or Stop is called), the
// subsystem stops publishing and waits for in-flight work to finish.
//
// The pipeline goroutine spawns eagerly so the rig's serial port is
// held for the daemon's full lifetime. Startup-time bridge-error
// events that fire before any SSE client connects are cached by the
// hub and replayed to the first late subscriber, so a SPA tab
// opening after a config typo still sees the toast.
//
// Idempotent — repeat calls are no-ops once started.
//
// When cfg.Enabled is false, Start succeeds without spawning anything.
// The hub is still active so Subscribe doesn't error, but no events
// will ever flow. SSE handlers see an empty stream.
//
// Stop-before-Start puts the Service in a terminal state — a
// subsequent Start returns silently rather than spinning up a
// pipeline that has nowhere to publish (hub is closed).
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
		s.logger.InfoWith().Msg("bridge: subsystem disabled (bridge.enabled=false); no rig acquired, no events emitted")
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.runSupervisor(runCtx)
	return nil
}

// Stop cancels the parent context, waits for in-flight goroutines,
// and closes the hub so any open SSE subscribers see a clean stream
// end. Idempotent under both sequential and concurrent calls — the
// first Stop runs the teardown; subsequent callers (whether
// sequential or racing) wait until the first has finished, then
// return nil. "Stop returned, therefore stopped" holds for every
// caller.
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
		s.hub.close()
		if s.logger != nil {
			s.logger.InfoWith().Msg("bridge: subsystem stopped")
		}
	})
	<-s.stopDone
	return nil
}

// Enabled reports whether the bridge subsystem is configured to run.
// Used by api.Server to decide whether to register `/v1/rig/events`.
// Nil-safe (returns false on a nil receiver).
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// Subscribe registers a new SSE subscriber. Returns the receive-only
// channel + idempotent unsubscribe. Channel closes on unsubscribe,
// slow-subscriber eviction, or Service.Stop.
//
// If the hub has cached a startup-time bridge-error (e.g. unknown
// driver, port permission denied that fired before this subscriber
// connected), it is replayed as the new subscriber's first event so
// the SPA tab still toasts the operator-actionable message.
//
// Subscribing to a stopped (or never-started) Service returns an
// already-closed channel so the SSE handler's range loop exits
// immediately.
func (s *Service) Subscribe() (<-chan Event, func()) {
	return s.hub.subscribe()
}

// TriggerBootstrap writes the rigdef's READ command to the rig so a
// freshly-connected SSE subscriber gets a current identity + state
// snapshot rather than waiting for the operator to wiggle the dial
// (per ADR 0019 active-snapshot model). Called by the SSE handler
// immediately after Subscribe.
//
// Safe-by-design no-op when the pipeline isn't running (bridge
// disabled, pipeline mid-shutdown, pipeline never started after a
// terminal serial error). The SSE stream still works in that case;
// the SPA's catState just shows defaults until the rig pushes
// naturally on operator action.
//
// Errors from the underlying write are returned but don't break the
// SSE connection — a bootstrap failure for one subscriber doesn't
// affect the hub fan-out for others.
func (s *Service) TriggerBootstrap(ctx context.Context) error {
	s.mu.Lock()
	cl := s.activeClient
	bb := s.bootstrapBytes
	s.mu.Unlock()
	if cl == nil || len(bb) == 0 {
		return nil
	}
	return cl.WriteCommandBytes(ctx, bb)
}
