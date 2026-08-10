package ft8

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
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

// catReconcileInterval is how often the CAT-gate reconcile loop checks rig
// liveness to acquire the mic once CAT comes up (rig powered on after the FT8
// view was already open) or release it when CAT drops mid-session. Only runs
// when a CAT gate is installed (SetCatGate). A second or two of latency on
// mic acquire/release tracking the rig's power state is immaterial. Package-
// level var so tests can dial it down (or drive reconcileCat directly).
var catReconcileInterval = 2 * time.Second

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
	captureGen    uint64             // bumped per session start; loop-exit callbacks revalidate ownership against it (review ed13a9c6)
	releasing     bool               // a release is draining s.wg with s.mu dropped (F2)
	captureCancel context.CancelFunc // cancels the current capture run
	lingerTimer   *time.Timer        // pending release after the last unsubscribe
	wg            sync.WaitGroup     // scheduler + decoder of the current session

	// catLive gates capture acquisition on the rig/CAT being live: no live rig →
	// no microphone, even with the FT8 view open (the boot-time mic-grab bug —
	// smd autostarts, the SPA reopens to FT8, and the daemon grabbed the mic with
	// the rig off). Injected via SetCatGate in cmd/smd, and ONLY when the bridge
	// is enabled — nil means no gate (no CAT configured, or tests), preserving
	// pure demand-driven capture. The acquire-time check lives in
	// startCaptureLocked; the dynamic half (acquire when CAT comes up, release
	// when it drops) is the catReconcile loop. Read under s.mu where it gates;
	// catLive() itself takes the bridge lock, so the lock order is s.mu → bridge
	// (the bridge never calls back into ft8).
	catLive func() bool

	// dialSource attributes each captured slot to the dial frequency it was
	// heard on (SetDialSource). Handed to the scheduler when a capture session
	// starts; nil (no CAT) leaves slots unattributed AND untracked, which is
	// what tells decodeLoop to publish them anyway. Read under s.mu, but unlike
	// catLive it is INVOKED from the scheduler goroutine with no ft8 lock held,
	// so it adds no lock nesting at all.
	dialSource func() (float64, bool)

	// captureListener reports the capture-session lifecycle outward (ADR 0064:
	// cmd/smd wires it to bridge.SetFt8CaptureLive so the FT8 meter poll lives
	// and dies with the session). Called under s.mu at the three flip sites —
	// same s.mu → bridge lock order as catLive, and the bridge never calls
	// back into ft8. Nil (no bridge / tests) is fine.
	captureListener func(live bool)

	bgCancel context.CancelFunc // cancels subsystem-lifetime loops (catReconcile)
	bgWg     sync.WaitGroup     // subsystem-lifetime loops; drained by Stop

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

	txMu sync.Mutex
	// idleInhibitor + idleRelease: the desktop is asked to stop idling for as
	// long as TX is armed (see idleinhibit.go). idleRelease is non-nil EXACTLY
	// while an inhibition is held, so it doubles as the "held" flag — one
	// variable, so the two cannot disagree.
	idleInhibitor IdleInhibitor
	idleRelease   func()
	keyer         TxKeyer
	txArmed       bool
	txDisarmCause string // cause of the last real teardown ("" while armed); rides TxState
	txInFlight    bool
	txClosed      bool   // set on Stop; refuses further arming
	txMessage     string // message of the in-flight transmission ("" = none)
	txOffsetHz    float64
	txDialMHz     float64 // dial of the in-flight transmission, for the keyed-time decode-log TX line
	// armDialMHz is the dial the daemon read when TX was ARMED (0 = unknown). The
	// pre-key gate compares against it, so the frequency binding holds on every
	// keying path — including with no session and no capture running. Distinct from
	// sessionDialMHz, which is what a completed contact is LOGGED on.
	armDialMHz float64
	// sessionDialMHz is the dial the DAEMON read when the active session started
	// (0 = it had none). The TX-safety invariant compares it against a live read
	// before every rung — see seqTransmit. Pinned in sessionTxGate, the shared
	// preamble of every Start*; NOT the client-supplied dial carried for logging.
	sessionDialMHz float64
	txLastErr      string // i18n code of the last failed transmission ("" = none)
	// exchPath is the operator's antenna-path choice for the active exchange
	// ("S"/"L"); logging-only. Lifecycle (review 2026-07-20 #5 + round 12 #3):
	// atomically CONSUMED before each session start (restored on a rejected
	// start — consuming first means the path is at its default before the new
	// session is ever visible-active, so a selection for it can't be stomped)
	// and generation-checked back to default at each logged contact. exchPathGen
	// counts explicit SetExchangePath calls so a rejected start's restore or a
	// delayed completion's reset applies only if no newer selection landed
	// (latest selection wins — codex review). Accepted residue: a caller-mode
	// answerer that fails mid-exchange (no RR73, drop back to CQ) leaves the
	// previous choice in place for the next answerer — adjust it as the pile-up
	// moves.
	exchPath    string
	exchPathGen uint64
	txDevice    txPlayer
	txCtrl      *TxController
	txCancel    context.CancelFunc
	txWg        sync.WaitGroup

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

	// decodeSink observes every slot's decodes (e.g. the PSK Reporter uploader);
	// injected via SetDecodeSink before Start. nil = no observer. Called from the
	// decode goroutine after the SSE publish. DI keeps internal/ft8 free of the
	// consumer (one-way import), same as qsoLogger.
	decodeSink func(DecodeReport)

	// txSlots is a small ring of the StartUTCs of recent slots the FT8 TX controller
	// keyed (via txCtrl.onTransmit, recorded only after PTT engages). decodeLoop skips
	// decode + occupancy for those slots: the captured audio is our own transmission
	// (rig TX-audio bleed), which the decoder would surface as ghost Band Activity rows
	// and the raw-spectrum energy detector would read as a busy band right on our own
	// offset — the occupancy sibling of the self-decode filter. A ring (not one slot)
	// so a second TX keyed close behind can't evict the first before its slot is
	// processed (the occupancy check runs ~1 s after the slot ends). Guarded by txSlotMu.
	txSlotMu sync.Mutex
	txSlots  [3]string
	txSlotIx int

	// decLog is the optional JTDX-style ALL.TXT decode log (ft8.decode_log.enabled).
	// Opened at capture-start and closed at capture-release (and on an unexpected
	// capture-loop exit), so it follows the demand-driven capture lifecycle.
	// atomic.Pointer because the RX writer (decode goroutine) and the TX writer (tx
	// goroutine) both Load it without a shared lock; nil = disabled/idle (its
	// methods are nil-safe no-ops). workingDir is the daemon's resolved working dir,
	// the default decode-log location (set by NewService).
	decLog     atomic.Pointer[DecodeLog]
	workingDir string
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
		hub:       newHub(log),
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
	// No auto-work policy install (ADR 0066): the config knob seeds the SPA
	// toggle only; arming consumes the per-start staged intent + session mode.
	// Stamp the antenna choice onto the per-attempt CompletedQso when the final
	// rung succeeds. Keeping the stamp on the QSO (rather than in a Service
	// singleton) prevents overlapping completion callbacks from stealing it.
	s.seq.prepareComplete = s.stampCompletionPath
	// On a completed exchange, hand it to the injected logger (e4). Reads
	// s.qsoLogger at call time (set via SetQsoLogger before Start), so the
	// daemon wires logging after construction.
	s.seq.onComplete = func(c CompletedQso) {
		// Clear the completed exchange's live selection only if no newer operator
		// selection has landed since its per-QSO stamp. A new session may already be
		// active here: an unconditional clear would erase that session's choice.
		s.resetCompletionPath(c)
		if c.AntPath == "" { // defensive: direct/unit-test completions default short
			c.AntPath = antPathShort
		}
		// c.LogbookID is already stamped by the sequencer at construction from the
		// pinned session logbook (ADR 0055) — nothing to add here.
		if s.qsoLogger != nil {
			s.qsoLogger(s.base(), c)
		} else {
			// finalrung logs "QSO complete" on this same path, so a silent drop
			// here leaves the log AFFIRMING a QSO that was never handed anywhere.
			// cmd/smd always wires the sink before Start — reaching this branch is
			// a wiring bug, hence Error, not a runtime condition to degrade over.
			s.log.ErrorWith().Str("their_call", c.TheirCall).
				Msg("ft8: completed QSO discarded — no QSO sink wired")
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

	// CAT gate installed → run the reconcile loop for the subsystem's lifetime so
	// capture tracks the rig powering on/off. bgCtx is a child of the daemon ctx
	// cancelled by Stop (parentCtx itself isn't ours to cancel). No gate → no loop;
	// capture stays purely demand-driven.
	if s.catLive != nil {
		bgCtx, cancel := context.WithCancel(ctx)
		s.bgCancel = cancel
		safego.GoTracked(bgCtx, "ft8.catReconcile", s.onPanic, func() {
			s.runCatReconcile(bgCtx)
		}, true, &s.bgWg)
	}

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
		// A teardown was pending — cancel it, the operator is back.
		//
		// Then FALL THROUGH to the acquire rather than returning (2026-07-25
		// review fix): a pending timer no longer implies a live capture session.
		// onSubscriberRemoved now schedules the timer regardless of s.capturing
		// (it carries the attended-only TX disarm), so this branch is also reached
		// after a session whose capture loop died — where returning early would
		// leave the reconnecting subscriber with NO capture, defeating the
		// documented "re-open the FT8 view to restart" recovery. startCaptureLocked
		// is the right arbiter: its F1 guard no-ops when a session is genuinely
		// still live, so the reuse case is unchanged.
		s.lingerTimer.Stop()
		s.lingerTimer = nil
	}
	s.startCaptureLocked()
}

// onSubscriberRemoved is called when a /v1/ft8/events subscriber disconnects.
// When the last one leaves (count→0) it schedules the attendance teardown after
// captureLinger rather than acting immediately, so a quick reconnect reuses the
// live session.
//
// Deliberately NOT gated on s.capturing (2026-07-25 review): this timer carries
// the attended-only TX disarm, which must run whenever the operator leaves —
// including when capture is already down. TX arms independently of capture
// (armTx checks only keyer / TxReady / output device), and capturing can go false
// on its own via onCaptureLoopExit (a dead capture loop) or a failed acquisition.
// Gating here therefore left TX ARMED with nobody attending: no timer was
// scheduled, so nothing ever disarmed, and a queued TransmitNext — which waits on
// the UTC boundary itself, not on the capture scheduler — could key after the
// operator closed the browser. Scheduling unconditionally is cheap: disarmTx is
// idempotent, and onLingerExpired still gates the DEVICE release on capturing.
func (s *Service) onSubscriberRemoved() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subCount > 0 {
		s.subCount--
	}
	if s.subCount != 0 || s.stopped || s.lingerTimer != nil {
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
	// No !s.capturing check (2026-07-25 review): the disarm below is the
	// attended-only guarantee and must run even with capture already down —
	// otherwise a session whose capture loop died (onCaptureLoopExit clears
	// capturing without disarming) leaves TX armed once the operator leaves. The
	// DEVICE release further down stays gated on capturing, which is the only
	// part that genuinely depends on it. Stop() owns the stopped case (disarmTx(true)).
	if s.subCount > 0 || s.stopped {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Attended-only: the last /v1/ft8/events subscriber is gone past the linger
	// window (the browser was closed, not briefly reloaded), so no operator is
	// attending FT8 — TX must not stay armed across browser sessions. Disarm FIRST,
	// and OUTSIDE s.mu, so dropping PTT + abandoning any active QSO (the safety
	// action) is never delayed by releaseCaptureLocked's decode-log close, which can
	// block on a stalled disk. disarmTx takes its own txMu and waits on an in-flight
	// transmission to drain (txWg); idempotent — a no-op when TX isn't armed.
	s.disarmTx(disarmUnattended)

	// Then release the capture device. Re-check under s.mu: a subscriber that
	// reconnected during the disarm window keeps the session (skip the release);
	// it re-arms TX, consistent with attended-only.
	s.mu.Lock()
	if s.subCount == 0 && !s.stopped && s.capturing {
		s.releaseCaptureLocked()
	}
	s.mu.Unlock()
}

// SetCatGate installs the CAT-liveness predicate that gates capture acquisition
// (no live rig → no microphone). When set, the device is acquired only while the
// predicate returns true AND a subscriber is present; it is released if CAT drops
// mid-session and (re)acquired when CAT returns. cmd/smd wires this to
// bridge.Service.RigConnected, and ONLY when the bridge is enabled — leaving it
// nil (no gate, pure demand-driven capture) for a no-CAT setup. Call before Start;
// read under s.mu where it gates.
func (s *Service) SetCatGate(catLive func() bool) {
	s.mu.Lock()
	s.catLive = catLive
	s.mu.Unlock()
}

// SetDialSource installs the rig dial-frequency reader that attributes each
// captured slot to the frequency it was heard on (Scheduler.SetDialSource, and
// OccupancyReport.DialMHz for why it matters). cmd/smd wires this to
// bridge.Service.CurrentDialMHz, and only when the bridge is enabled — a no-CAT
// setup leaves it nil and its occupancy reports simply carry no frequency. Call
// before Start; it is read when a capture session builds its scheduler.
func (s *Service) SetDialSource(dial func() (float64, bool)) {
	s.mu.Lock()
	s.dialSource = dial
	s.mu.Unlock()
}

// runCatReconcile is the dynamic half of the CAT gate (startCaptureLocked is the
// acquire-time half): it periodically aligns the capture session with rig
// liveness — acquiring the mic once CAT comes up with a subscriber waiting (the
// operator powered the rig after opening the FT8 view) and releasing it when CAT
// drops mid-session. Started by Start only when a gate is installed; exits on
// bgCtx cancel (Stop).
func (s *Service) runCatReconcile(ctx context.Context) {
	t := time.NewTicker(catReconcileInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reconcileCat()
		}
	}
}

// reconcileCat acquires/releases capture to match CAT liveness. Periodic and
// idempotent, so any check-then-act race self-corrects on the next tick. catLive()
// is sampled outside s.mu (it takes the bridge lock); the locked sections re-check
// the conditions that matter (startCaptureLocked re-runs the gate itself; the
// release path re-checks liveness) so a stale snapshot can't act wrongly.
func (s *Service) reconcileCat() {
	if s.catLive == nil {
		return
	}
	live := s.catLive()

	// CAT dropped while capturing → drop the mic now. Disarm TX first and OUTSIDE
	// s.mu (same ordering as onLingerExpired): the rig is gone, so PTT / any active
	// exchange must end before the device is released, and disarmTx takes txMu then
	// s.mu. Re-check !catLive() under the lock so a CAT blip that recovered within
	// the tick doesn't needlessly tear the session down.
	s.mu.Lock()
	dropMic := s.capturing && !live && s.started && !s.stopped
	s.mu.Unlock()
	if dropMic {
		s.disarmTx(disarmCatLost)
		s.mu.Lock()
		if s.capturing && !s.stopped && !s.catLive() {
			s.log.InfoWith().Msg("ft8: rig/CAT dropped — releasing capture")
			s.releaseCaptureLocked()
		}
		s.mu.Unlock()
		return
	}

	// CAT came up while a subscriber is waiting → acquire (the boot/rig-off case,
	// resolved once the operator powers the rig). startCaptureLocked re-runs the
	// gate, so a stale `live` can't force an acquire.
	s.mu.Lock()
	if live && !s.capturing && s.subCount > 0 && s.lingerTimer == nil &&
		s.started && !s.stopped && s.cfg.Enabled {
		s.startCaptureLocked()
	}
	s.mu.Unlock()
}

// startCaptureLocked acquires the capture source and spawns the scheduler +
// decode worker for one session. Fail-soft: a source that won't start leaves
// capturing=false (the subsystem stays idle, logged) rather than erroring.
// Caller holds s.mu.
func (s *Service) startCaptureLocked() {
	// Already capturing — keep the live session, don't start a second (F1). A
	// reconnect during a release's linger/disarm window (onLingerExpired clears
	// lingerTimer, drops s.mu across disarmTx, ~13s) can drive subCount 0→1 with
	// lingerTimer already nil and reach here while session 1 is still live.
	// Starting a second session would orphan the first device+pump (malgoSource.
	// Start overwrites its fields) and later deadlock the release drain. Keeping
	// the live session is exactly what the linger design intends.
	if s.capturing {
		return
	}
	// CAT-liveness gate: never grab the microphone when the rig is off / CAT
	// isn't live, even with a subscriber present. The catReconcile loop reacquires
	// once CAT comes up. A nil gate (no CAT configured, or tests) means
	// demand-driven as before. catLive() takes the bridge lock; safe under s.mu
	// (lock order s.mu → bridge; the bridge never calls back into ft8).
	if s.catLive != nil && !s.catLive() {
		s.log.InfoWith().Msg("ft8: capture deferred — rig/CAT not live (will acquire when CAT comes up)")
		return
	}
	runCtx, cancel := context.WithCancel(s.parentCtx)
	samples, err := s.src.Start(runCtx)
	if err != nil {
		cancel()
		s.log.WarnWith().Err(err).Msg("ft8: capture unavailable; subsystem idle")
		return
	}
	s.captureCancel = cancel
	s.setCapturingLocked(true)
	s.captureGen++
	gen := s.captureGen

	// RX audio-level meter tee (audiolevel.go): measures peak/RMS on the way
	// past and forwards untouched — deliberately OUTSIDE the scheduler/slot
	// path that the TX + attribution invariants guard.
	samples = s.teeAudioLevel(runCtx, samples)

	// Open the JTDX ALL.TXT decode log on the FIRST session and keep it for the
	// SERVICE lifetime (reviews 9aafc206 + 220bc363: per-session open/close had
	// two mutually-exclusive hazards — closing under s.mu blocked the service
	// on slow storage, closing after unlock let a replacement open the SAME
	// configured file while the old lumberjack instance still drained, racing
	// rotation and interleaving appends). The path is config-snapshotted and
	// cannot change mid-run, so one instance serves every session; a late line
	// from a dying session is a timestamped entry in the right file. Closed
	// only in Stop, after the goroutines drain. Fail-soft: openDecodeLog
	// returns nil on error, leaving the writer a no-op — and the ==nil guard
	// retries on the next session start.
	if dl := s.cfg.DecodeLog; dl != nil && dl.Enabled && s.decLog.Load() == nil {
		s.decLog.Store(openDecodeLog(dl.Path, s.workingDir, s.log))
	}

	// The scheduler + decoder are a COUPLED pair (Scheduler.Run closes its slots
	// channel on exit), so they can't be respawned independently (respawn=false).
	// Instead each installs onCaptureLoopExit: if either exits while the session
	// is still live (a safego-recovered panic, or an unexpected early return), the
	// subsystem is marked not-capturing + a terminal error logged, so the operator
	// isn't left with a live-looking but dead capture (review 2026-06-19 M2).
	sch := NewScheduler(samples, s.log)
	// Dead-stream watchdog (deadsource.go): a desktop audio reshuffle can leave
	// the capture stream dangling with no error anywhere — the watchdog turns
	// that into an automatic release + reacquire.
	sch.SetOnDeadSource(s.onDeadCaptureSource)
	// Slot→frequency attribution (OccupancyReport.DialMHz). Installed before Run,
	// as SetDialSource requires; nil with no CAT, which is the honest no-op.
	sch.SetDialSource(s.dialSource)
	// The dial guard's trigger: the scheduler is what actually notices a move, on
	// every audio batch. Handed off to a goroutine because onDialMoved takes seqGate
	// and waits on an in-flight transmission, and the scheduler loop must keep
	// servicing slot boundaries — a blocked scheduler drops slots.
	sch.SetOnDialMoved(func(from, to float64) {
		safego.Go(runCtx, "ft8.dialguard", s.onPanic, func() { s.onDialMoved(from, to) }, false)
	})
	safego.GoTracked(runCtx, "ft8.scheduler", s.onPanic, func() {
		defer s.onCaptureLoopExit(runCtx, gen, "ft8.scheduler")
		_ = sch.Run(runCtx)
	}, false, &s.wg)
	safego.GoTracked(runCtx, "ft8.decoder", s.onPanic, func() {
		defer s.onCaptureLoopExit(runCtx, gen, "ft8.decoder")
		s.decodeLoop(sch.Slots())
	}, false, &s.wg)

	s.log.InfoWith().
		Str("device", s.cfg.Device).
		Bool("osd", s.osdEnabled()).
		Msg("ft8: subscriber present; capture started, decoding live slots")
}

// onDeadCaptureSource handles the scheduler's dead-source verdict (deadsource.go):
// the capture stream is delivering no live audio, so replace the session — release
// (destroying the dangling OS stream) and let the release's tail re-acquire for
// the still-present subscriber, creating a fresh stream that links to the current
// device nodes. Runs the release on its OWN goroutine (bgWg): it is called from
// the scheduler goroutine, and releaseCaptureLocked drains that very goroutine —
// inline would deadlock. The guards make a late firing (session already being
// torn down, subsystem stopping) a no-op; the monitor's once-latch means at most
// one restart per session, so restarts can't stack.
func (s *Service) onDeadCaptureSource(reason string) {
	safego.GoTracked(s.parentCtx, "ft8.capture-restart", s.onPanic, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.capturing || s.releasing || s.stopped {
			return
		}
		s.log.WarnWith().Str("reason", reason).
			Msg("ft8: capture stream dead (no live audio reaching the scheduler) — restarting capture session")
		s.releaseCaptureLocked()
	}, false, &s.bgWg)
}

// onCaptureLoopExit handles a scheduler/decoder goroutine exit (review 2026-06-19
// M2). When runCtx is already cancelled the exit is a normal teardown
// (release/Stop) and this is a no-op. Otherwise the loop died unexpectedly — a
// safego-recovered panic or a premature return — so it marks the subsystem
// not-capturing, cancels the session (winding down the coupled sibling
// goroutine), and logs a terminal error so the operator isn't left with a
// live-looking but dead capture. It runs in a defer so it fires on a recovered
// panic too (safego recovers outside the goroutine body). There is no automatic
// restart: re-opening the FT8 view re-acquires capture. Does NOT wait on s.wg,
// so it can't deadlock a concurrent releaseCaptureLocked drain.
func (s *Service) onCaptureLoopExit(runCtx context.Context, gen uint64, who string) {
	if runCtx.Err() != nil {
		return // normal teardown: release/Stop cancelled the session first
	}
	s.mu.Lock()
	// Ownership revalidation UNDER the lock (review ed13a9c6 P1): both loops
	// defer this handler and both can pass the unlocked fast-path above before
	// either locks. The second callback can then run after a REPLACEMENT
	// session has started — and without this check it tore the replacement
	// down: capturing=false, the replacement's cancel, its source stopped, its
	// audio-publish token retired (permanently muting the live meter). A
	// callback owns exactly the generation it was spawned under; anything else
	// is a no-op. The ctx re-check catches the same-generation double (first
	// callback already cancelled), sparing a harmless-but-noisy double clear.
	if gen != s.captureGen || runCtx.Err() != nil {
		s.mu.Unlock()
		return
	}
	wasCapturing := s.capturing
	s.setCapturingLocked(false)
	if s.captureCancel != nil {
		s.captureCancel()
		s.captureCancel = nil
	}
	// Stop the source HERE (review 2026-07-20 #4): capturing is now false, so a
	// later releaseCaptureLocked early-returns without ever calling src.Stop —
	// the CGO source's device would stay acquired (mic held with the loop dead)
	// and the next acquisition would overwrite the un-Closed capture, leaking
	// its backend context. Same under-s.mu placement as releaseCaptureLocked;
	// Stop waits only on the source's own pump (ctx already cancelled above),
	// never on s.wg, so it cannot deadlock a concurrent release drain.
	if err := s.src.Stop(); err != nil {
		s.log.WarnWith().Err(err).Msg("ft8: capture stop error (loop exit)")
	}
	// Invalidate the replay cache for the dead session — a later subscriber
	// must not be handed this session's last slot as if it were live.
	s.hub.clearActivity()
	// Close the decode log so the dead session doesn't leak the open file or let a
	// later TX write to a stale log (releaseCaptureLocked would early-return now
	// that capturing=false). Safe even though the sibling loop may still be winding
	// down: only the decode goroutine writes RX, and it is the one exiting here.
	//
	s.mu.Unlock()
	// The decode log is deliberately NOT touched here: it is service-lifetime
	// (see startCaptureLocked — reviews c5bbbcbf, 9aafc206 and 220bc363 each
	// found a different defect in per-session teardown of it; the mechanism,
	// not the placement, was wrong). Stop owns the close.
	if wasCapturing {
		s.log.ErrorWith().Str("goroutine", who).
			Msg("ft8: capture loop exited unexpectedly; capture stopped — re-open the FT8 view to restart")
	}
}

// releaseCaptureLocked cancels the current capture session, releases the device,
// and drains the scheduler + decode goroutines before returning, so the next
// acquisition never overlaps a still-running session. No-op when not capturing,
// or when a release is already draining. Caller holds s.mu.
//
// The drain (s.wg.Wait) runs with s.mu DROPPED (F2): holding it across the wait
// deadlocks a capture loop that died on its OWN (a USB unplug closes the source →
// Scheduler.Run returns → onCaptureLoopExit passes its runCtx.Err()==nil check,
// then blocks entering s.mu.Lock() — held by this release, which is waiting on
// that very goroutine). Same unlock-then-Wait shape as Stop / the bridge. Across
// the drop, `capturing` stays TRUE and `releasing` is set, so a racing
// onSubscriberAdded → startCaptureLocked no-ops (F1 guard) instead of starting a
// second session on the shared s.wg, and a re-entrant release returns early. A
// subscriber that reconnected during the drain is re-acquired at the end.
func (s *Service) releaseCaptureLocked() {
	if !s.capturing || s.releasing {
		return
	}
	s.releasing = true
	// Cancel + stop the source under s.mu so a loop that then checks runCtx.Err()
	// takes onCaptureLoopExit's no-op teardown branch (skips s.mu).
	if s.captureCancel != nil {
		s.captureCancel()
		s.captureCancel = nil
	}
	if err := s.src.Stop(); err != nil {
		s.log.WarnWith().Err(err).Msg("ft8: capture stop error")
	}

	// Drain with s.mu dropped (F2 — see the doc comment).
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()

	s.setCapturingLocked(false)
	s.releasing = false
	// The decode log survives the release — service-lifetime, closed in Stop
	// (see startCaptureLocked). A reacquired session reuses the same instance.
	// Invalidate the Band Activity / occupancy replay cache: with the session
	// ended, a later subscriber must not be handed this session's last slot (it
	// would show stale decodes when the rig is off and capture can't reacquire).
	s.hub.clearActivity()
	s.log.InfoWith().Msg("ft8: no subscribers; capture released")

	// A subscriber that reconnected while we drained (s.mu was dropped) is now
	// present with no capture — re-acquire for it. Skipped when stopping (Stop's
	// release must not re-acquire); startCaptureLocked's CAT gate also defers
	// reconcileCat's drop-mic release when the rig is off.
	if s.subCount > 0 && !s.stopped {
		s.startCaptureLocked()
	}
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
		s.disarmTx(disarmShutdown)

		s.mu.Lock()
		s.stopped = true
		if s.lingerTimer != nil {
			s.lingerTimer.Stop()
			s.lingerTimer = nil
		}
		s.releaseCaptureLocked() // cancels + stops + drains; no-op if idle
		bgCancel := s.bgCancel
		s.mu.Unlock()

		// Drain the capture goroutines OUTSIDE s.mu. If Stop raced an in-flight
		// linger-expiry release, that release's releaseCaptureLocked no-op'd above
		// (the `releasing` guard) and is draining s.wg on the time.AfterFunc
		// goroutine — which nothing else waits on (unlike reconcileCat, covered by
		// bgWg). stopped=true blocks any re-acquire and multiple WaitGroup waiters
		// are fine, so Stop honours its "drains the scheduler + decode goroutines"
		// contract regardless of which release owns the drain. A no-op when Stop's
		// own release already drained.
		s.wg.Wait()

		// Stop the CAT-reconcile loop (if running) and wait for it to exit —
		// OUTSIDE s.mu, since the loop takes s.mu each tick (waiting under the lock
		// would deadlock).
		if bgCancel != nil {
			bgCancel()
		}
		s.bgWg.Wait()

		// Close the SERVICE-LIFETIME decode log, with every writer drained
		// (scheduler/decoder via s.wg above; a straggler TX write no-ops —
		// DecodeLog serialises writes against Close). Stop is the ONLY closer:
		// per-session teardown of this log produced three consecutive review
		// findings (c5bbbcbf, 9aafc206, 220bc363) before the lifecycle moved
		// here — see startCaptureLocked.
		if dl := s.decLog.Swap(nil); dl != nil {
			dl.Close()
		}

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

// decodeLoop consumes completed slots: it decodes each, publishes the decode feed
// (live Band Activity) and the per-slot occupancy report (the clear-offset picker's
// input) from the same samples + decodes, and drives the sequencer. A slot we
// transmitted in (wasTxSlot) is skipped for BOTH decode and occupancy — its captured
// audio is our own signal — but still emits an empty decode report so the SPA's slot
// clock keeps ticking, and leaves the prior RX-slot occupancy report standing.
// DecodeSlot is itself fail-soft (it recovers per-slot panics), so a bad slot can't
// break the loop; the safego wrapper around this loop is belt-and-braces. Occupancy
// is cheap (one averaged FFT per slot, ~tens of ms) next to the decode, so it adds
// negligible time to the slot budget.
func (s *Service) decodeLoop(slots <-chan Slot) {
	// ONE stateful decoder per capture session (this loop's lifetime, one
	// goroutine): cross-slot hash/A7 state lets "<...>" references resolve to
	// calls heard in earlier slots, and dies with the session so no stale
	// context survives a release/re-acquire (see slotDecoder). Mid-session a
	// QSY resets it (the dial-moved case below) and delivery gaps advance it.
	dec := newSlotDecoder(s.osdEnabled(), s.log)
	// Previous delivered slot's boundary + dial, for the omitted-slot advance
	// and the dropped-QSY context check below. Zero until the session's first
	// slot arrives.
	var prevSlotStart time.Time
	var prevSlotDial float64
	// Previously-recommended top offset, carried across slots for the
	// clear-offset hysteresis (stickySuggested): the ★ recommendation stays put
	// while it remains clear instead of hopping to a marginally wider gap each
	// slot. Loop-local, so it resets naturally when a new capture session starts.
	prevTop := 0
	// Frequency the previous slot was attributed to, so a band change between
	// slots resets the hysteresis above. 0 = unattributed (no CAT), which never
	// changes and so never resets — matching the pre-attribution behaviour.
	prevDial := 0.0
	for slot := range slots {
		ref := SlotRefFromTime(slot.StartUTC)
		txSlot := s.wasTxSlot(ref.StartUTC)

		// A slot whose dial MOVED spans two frequencies, so its decodes cannot be
		// placed either — and decodes are the more dangerous half. An A→B→A
		// excursion (band button, corrected inside the window) captures stations
		// heard on B while the rig ends on A; every consumer downstream then reads
		// the CURRENT dial and gets A. The SPA would render those as workable and
		// key an answer on A at a station that is not there, and the PSK Reporter
		// sink stamps dial+offset at sink time, publishing wrong spots to a public
		// network. So a moved slot is treated exactly like a TX slot: no decode,
		// which leaves msgs empty — nothing to the decode log, nothing to the spot
		// sink — while the empty report below still ticks the SPA's slot clock.
		// The SEQUENCER is a separate skip below, deliberately: empty is not the
		// same as nothing there. It reads an empty slot as "they said nothing",
		// which is a claim it acts on. Conflating the two in this comment is what
		// hid that gap for a round (codex P1 on 97565b03).
		//
		// Deliberately keyed on MOVED, not on the wider unplaceable below: a dial
		// that was never known does not imply a band change, and suppressing
		// decodes for it would blind Band Activity on any rig whose frequency the
		// bridge cannot read, which is a far worse failure than an unattributed
		// occupancy panel.
		// Ending the SESSION on a dial move used to live here and is gone: it
		// abandoned whichever session was active when this slot was PROCESSED, not
		// the one that was live during the window it describes — so a QSY followed
		// by Call CQ inside the same slot had its brand-new, perfectly valid
		// session killed at the next boundary (codex P1 on c6b8a15d). It also
		// bypassed AbandonQso's seqGate and in-flight cancellation.
		//
		// TX safety is now the INVARIANT check in seqTransmit — the rig must still
		// be on the dial the session pinned — which cannot be defeated by a missed,
		// late or mis-attributed transition, and leaves a session started on the new
		// dial alone. This flag now governs only what we PUBLISH from this slot.
		dialMoved := slot.DialChanged

		// Decoder context check. A DIAL-MOVED slot resets the decoder — the QSY
		// replaced the receiver context, and the band-blind hash table must not
		// cross it (see slotDecoder.reset; review cd1757a7cda2 P1). But the
		// flag rides the one slot that spans the move, and the scheduler can
		// DROP that slot (emitSlot's best-effort send) or never emit its
		// boundary — the next delivered slot is then cleanly attributed to the
		// NEW band with no flag anywhere. So a dial DIFFERENCE between
		// consecutive DELIVERED slots resets too (review 75f40264fe2b P1).
		// Enumerated consequences: a CAT-unreadable window (A→0→A) costs two
		// conservative resets, recall-only; a delivered QSY resets twice (the
		// moved slot, then 0→B), harmless on already-empty state; a no-CAT
		// rig's dial is always 0, never differs, and keeps full cross-slot
		// state.
		contextChanged := dialMoved ||
			(!prevSlotStart.IsZero() && slot.DialMHz != prevSlotDial)
		if contextChanged {
			dec.reset()
		} else if !prevSlotStart.IsZero() {
			// Same context, lossy channel: the scheduler skips a boundary
			// serviced over two seconds late and drops slots on a full channel
			// (Dropped()), and the decoder's A7 buckets are parity-indexed, so
			// an odd-length gap would swap them for the rest of the session.
			// Advance once per OMITTED physical slot (AC8, review cd1757a7cda2
			// P2); advancing rather than resetting is deliberate — the hash
			// table must survive a lossy channel, and a skip on an
			// already-empty bucket costs ~0.1 ms, so no cap is needed.
			missed := int(slot.StartUTC.Sub(prevSlotStart).Round(SlotDuration)/SlotDuration) - 1
			for i := 0; i < missed; i++ {
				dec.skip()
			}
		}
		prevSlotStart = slot.StartUTC
		prevSlotDial = slot.DialMHz

		// Skip decode + occupancy for a slot we transmitted in: the captured audio is
		// our own TX (rig bleed). Decoding it wastes ~1 s and can surface garbled bleed
		// as ghost Band Activity rows; the raw-spectrum energy detector would mark our
		// own offset "busy" and flicker the readout busy↔clear in lockstep with TX/RX
		// (the occupancy sibling of the self-decode filter). WSJT-X likewise doesn't
		// decode its own TX slot. A TX slot still ADVANCES the stateful decoder
		// (dec.skip — a zero-slot decode whose output is nothing) so the
		// parity-keyed A7 hint buckets stay aligned across it. Either way the
		// slot's real reason is still what publishes below (empty report,
		// suppression line).
		var rich []goft8.DecodedMessage
		switch {
		case dialMoved:
			// Unattributable window: nothing to decode; its reset ran above.
		case txSlot:
			dec.skip()
		default:
			rich = dec.decode(slot.Samples)
		}
		// THE BRANCH POINT (design §4 prerequisite 2): `rich` is the complete
		// go-ft8 result — every parse status, own-TX included. The evidence
		// branch taps it HERE, upstream of every curated filter, when the
		// evidence.db writer lands. Everything below is the curated branch.
		//
		// curateDecodes = parse-status filter + own-transmission drop. The
		// callsign is the ACTIVE session's pinned call (ADR 0055, pin-at-arm) —
		// no per-slot DB lookup, no fallback: idle → "" → no own-drop (nothing
		// of ours is on the air).
		msgs := curateDecodes(rich, s.seq.ActiveCallsign())

		// JTDX ALL.TXT RX lines (ft8.decode_log) — independent of the daemon log
		// level. nil-safe no-op when the decode log is disabled (and on a TX slot,
		// msgs is empty so nothing is written).
		s.decLog.Load().WriteRx(slot.StartUTC, msgs)

		// Drive the manual sequencer (ADR 0031) BEFORE publishing the decode
		// (review 2026-07-20 #2): the published decode is actionable — a start
		// request it triggers could otherwise race the still-pending OnSlot for
		// the same slot and double-drive it. Sequencer-first also spends the
		// late-window budget on the rung instead of on occupancy math. No-op
		// when no QSO is active; on our own TX slot the decodes are empty and
		// the parity-matched handler bails.
		//
		// A dial-moved slot is NOT driven at all — not even with empty decodes. We
		// could not hear that window, and "we heard nothing" is a claim the
		// sequencer acts on (repeat the rung, then key). Silence has to be
		// observed, not assumed. Unlike the session-ending rule this replaced, this
		// is safe to apply unconditionally: skipping one slot costs a session
		// started meanwhile nothing but that slot.
		if !dialMoved {
			s.seq.OnSlot(ref, msgs, time.Now().UTC())
		}

		// Publish the decode feed every slot (empty on our TX slots) so the SPA's slot
		// clock stays live; the decode + occupancy publish independent SSE events on
		// one stream, order between them doesn't matter to the SPA.
		report := newDecodeReport(ref, slot.DialMHz, msgs)
		s.hub.publish(hubEvent{name: EventDecode, payload: report})
		if s.decodeSink != nil {
			s.decodeSink(report)
		}

		// Occupancy needs MORE than the decodes do: it must be ATTRIBUTED to a
		// band to be rendered at all, so a dial that was never known disqualifies
		// it even though nothing moved. A CAT-attached session must never publish
		// occupancy it cannot place — there the operator CAN transmit and the
		// picker's suggested[0] feeds the TX offset, so showing an unplaceable
		// report is worse than showing nothing: the next slot is 15 s away. An
		// UNTRACKED session (no CAT) still publishes, because FT8 transmit is
		// refused without a writable rig — the keyer's TxReady and the dial read
		// share that precondition — so its occupancy panel is display-only and
		// cannot steer anything.
		unplaceable := slot.DialTracked && slot.DialMHz == 0
		// The sticky-offset carry resets on ANY change of frequency, not just a
		// mid-window move — a settled slot on a new frequency still ends the old
		// band's hysteresis. stickySuggested re-vets the carried offset against the
		// new band's occupancy so the ★ is always genuinely clear either way, but
		// preferring an offset because it was good on 20 m is arbitrary on 40 m.
		// (This once claimed a boundary QSY yields two cleanly-attributed slots
		// with no move to catch it. That was true of the round-5 endpoint sampling
		// and became false when per-batch sampling landed: consecutive samples
		// bracket every instant, so exactly one slot is always flagged. The stale
		// claim was cited back as evidence for a P1 that does not exist — hence the
		// correction rather than a deletion.)
		if slot.DialMHz != prevDial {
			prevTop = 0
		}
		prevDial = slot.DialMHz

		var rep OccupancyReport
		if !txSlot && !unplaceable {
			rep = Occupancy(ref, slot.Samples, msgs, s.occCfg)
			// Stamp the frequency the audio was actually captured on, so the
			// report is attributable no matter how late it is consumed.
			rep.DialMHz = slot.DialMHz
			rep.Suggested = stickySuggested(rep.Suggested, rep.Occupied, s.occCfg, prevTop)
			if len(rep.Suggested) > 0 {
				prevTop = rep.Suggested[0]
			} else {
				prevTop = 0
			}
			s.hub.publish(hubEvent{name: EventOccupancy, payload: rep})
		}

		// Ship-gate finding 4 (ft8-logging-gaps): a SUPPRESSED slot says so at
		// a level the operator's production log actually carries. The Debug
		// record below has the fields but Debug is filtered at the default
		// level, so "Band Activity went blank and the ladder didn't advance"
		// stayed unexplainable from smd.log — the same invisible-safety-action
		// class as the dial guard's session half (dogfood 2026-07-27). One
		// line, naming the rule AND its scope, because the two rules withhold
		// different things. A TX slot is deliberately excluded: our own
		// transmission is expected every other slot of a run, and at Info it
		// would bury the two lines that matter. Info per the gaps doc: rate is
		// bounded by slots. Tests: slotsuppression_test.go.
		if (dialMoved || unplaceable) && !txSlot {
			rule, scope := "unplaceable", "occupancy"
			if dialMoved {
				rule, scope = "dial_moved", "decode+sequencer+occupancy"
			}
			s.log.InfoWith().
				Str("slot", ref.StartUTC).
				Str("rule", rule).
				Str("suppressed", scope).
				Float64("dial_mhz", slot.DialMHz).
				Msg("ft8: slot suppressed")
		}

		s.log.DebugWith().
			Str("slot", ref.StartUTC).
			Bool("tx_slot", txSlot).
			// "why is the occupancy panel empty" is the first on-air question a
			// skipped slot raises, and these three answer it: no dial at all,
			// the operator tuning through the window, or CAT gone quiet.
			Float64("dial_mhz", slot.DialMHz).
			Bool("dial_moved", dialMoved).
			Bool("unplaceable", unplaceable).
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

// SetCaptureListener injects the capture-session lifecycle listener (ADR
// 0064). Call before Start, like the other seams.
func (s *Service) SetCaptureListener(fn func(live bool)) {
	s.captureListener = fn
}

// setCapturingLocked flips the capture flag and reports a REAL transition to
// the listener — change-only, so the linger's reconnect continuity and the
// several release paths cannot double-report. Caller holds s.mu.
func (s *Service) setCapturingLocked(live bool) {
	if s.capturing == live {
		return
	}
	s.capturing = live
	if s.captureListener != nil {
		s.captureListener(live)
	}
}
