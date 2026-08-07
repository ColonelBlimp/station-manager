package bridge

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
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

	// bootstrapCIV records whether the active rig speaks icom_civ, so
	// TriggerBootstrap spaces the snapshot read frames (CI-V half-duplex,
	// last-read-in-a-burst-wins — see writeSnapshotReads). Set with
	// bootstrapBytes at pipeline start, cleared on exit.
	bootstrapCIV bool

	// pollBytes is the pre-encoded POLL read-list (ADR 0035) — the steady-state
	// Icom state-mirror reads (non-operating VFO, mode+data, split) the poll loop
	// fires on civPollInterval. Empty when the rigdef declares no POLL command
	// (every Kenwood rig, and any Icom that doesn't opt in), which is the
	// data-driven gate: no POLL command → no poll loop. Set at pipeline start.
	pollBytes []byte

	// lastBroadcastAt is the time of the most recent unsolicited Transceive
	// broadcast from the rig (mu-guarded), updated by readLoop. The poll loop's
	// collision back-off reads it: a poll tick is skipped while broadcasts are
	// streaming (a dial-turn storm). Only broadcasts update it — the poll's own
	// replies (transponded to the controller) must not, or the poll would
	// suppress itself.
	lastBroadcastAt time.Time

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

	// noDataStrikes counts CONSECUTIVE no-data liveness timeouts in readLoop
	// (reset to 0 on any successful read, and on a fresh pipeline). It exists so
	// RigConnected can tell a merely-quiet rig from a genuinely dead one: a
	// passive no-data disconnect leaves activeClient/identityConfirmed set (they
	// clear only on pipeline exit), and the readLoop's INIT+READ probe recovers a
	// quiet-but-alive rig within ONE liveness cycle (one strike, then reset). A
	// rig that's actually gone but left the serial port open accrues strikes with
	// no recovery. RigConnected trips at noDataStrikeLimit so the FT8 capture gate
	// releases the mic on a real mid-session drop without flickering on the normal
	// per-cycle probe. atomic so readLoop updates it without taking s.mu per read.
	noDataStrikes atomic.Int32

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
	writeWatchdog                  time.Duration
	civReadGap                     time.Duration
	civAckTimeout                  time.Duration
	civPollInterval                time.Duration
	civPollQuiet                   time.Duration
	ft8MeterPollInterval           time.Duration
	ft8MeterPollTimeout            time.Duration

	// ADR 0064 FT8 meter poll state, under mu: the capture-session gate
	// (SetFt8CaptureLive), the last decoded RM4/RM5 answer, and whether the
	// sustained-loss notice has fired for the current loss episode.
	ft8CaptureLive   bool
	meterAnswerAt    time.Time
	meterAnswerStale bool

	// CI-V wait-for-ACK command-path state (ADR 0034). The IC-7300 confirms a
	// set-command with a bare FB/FA ACK and never broadcasts the change, so
	// SendCommands (icom_civ) writes each frame and waits for its ACK here.
	// cmdMu serialises command batches — one outstanding command at a time,
	// matching the half-duplex bus. pendingAck is the per-frame waiter channel
	// (buffered 1): SendCommands installs it under mu before each write and the
	// readLoop delivers the FB(true)/FA(false) to it via deliverAck. Nil when no
	// command is in flight.
	cmdMu      sync.Mutex
	pendingAck chan bool

	// keyMu serialises the full key + release sequences for the keyed-transmission
	// features (tune carrier + FT8 TX), across all protocols (review 2026-06-16).
	// KeyFt8Tx / StartTune / releaseFt8Tx / releaseTune each hold it for their
	// whole body, so at most one keyed-transmission state transition runs at a
	// time: no double-release (operator stop racing the auto-off backstop) and no
	// new key starting while a release is still settling/restoring (which could
	// fire a stale tx_off or restore over the new transmission). The release sets
	// active=false only at the end (finishTune/finishFt8Tx), so a second release
	// blocked here observes !active afterwards and no-ops. Lock order is keyMu →
	// cmdMu → mu; nothing acquires keyMu while holding cmdMu or mu.
	keyMu sync.Mutex

	// clientsMu orders a subscriber join/leave and its EventRigClients count
	// publication as ONE unit (2026-07-19 review P3): snapshotting the count
	// outside the publish let a join read 2, a concurrent leave publish 1,
	// then the join publish the stale 2 — leaving a lone tab's multi-tab
	// banner stuck with no later transition to correct it. Held only in
	// Subscribe/unsubscribe; the hub has its own internal lock and never
	// calls back into these paths, so ordering is clientsMu → hub lock.
	clientsMu sync.Mutex

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
	// tuneGen is the tune path's TX-transition generation (mu-guarded),
	// bumped on every key/finish/disconnect-clear. The auto-off backstop
	// captures the generation it was armed for and refuses to release or
	// re-arm against a DIFFERENT generation — without it, a stale failed-
	// release retry callback could attach to (and prematurely unkey) a
	// newer transmission (2026-07-18 TX-safety review, finding 6).
	tuneGen   uint64
	lastMode  string
	lastPower int

	// Rolling current-rig dial snapshot, mu-guarded, fed by readLoop alongside
	// lastMode/lastPower. lastVfoA/lastVfoB are the per-VFO frequencies (Hz);
	// 0 means that VFO has not been decoded this session — knownness is
	// per-VFO (2026-07-19 review P2), so CurrentDialMHz reports ok=false when
	// the SELECTED VFO is unknown rather than falling back to the other one.
	// lastSelectedVfo ("A"/"B") is which one is operating.
	// lastSelectedVfoKnown is separate from the string because rigs whose
	// definitions report SELECT (Yaesu) send the VFO frequencies and selection
	// as separate replies. After a disconnect, an early VFO-A reply must not be
	// treated as authoritative until the fresh SELECT reply arrives. Rigs with
	// no SELECT state (the IC-7300's implicit operating-VFO-A model) start each
	// snapshot with A known. selectedVfoExplicit is immutable after New.
	//
	// CurrentDialMHz resolves the selected VFO's freq for FT8 logging — the rig
	// is the authority for the logged frequency, NOT the SPA's start-time
	// snapshot (which is captured once and reused across a Call-CQ pile-up, so
	// it goes stale). Cleared on disconnect with the rest of the snapshot. NOT
	// frozen during tune/TX — the dial does not move while keyed.
	lastVfoA             int64
	lastVfoB             int64
	lastSelectedVfo      string
	lastSelectedVfoKnown bool
	selectedVfoExplicit  bool

	// Resolved (config-overridable, hard-clamped) tune knobs, snapshotted at
	// New like the timeout fields above. tunePowerW ≤ maxTunePowerW;
	// tuneMaxDuration ≤ maxTuneDuration. Read by the tune controller; mutate
	// only before Start, same rule as the timeout fields.
	tunePowerW      int
	tuneMaxDuration time.Duration

	// tuneRestoreSettle is the pause between unkeying and restoring mode+power
	// on tune stop (task #270). Snapshotted at New like the knobs above; mutate
	// only before Start. The carrier is already down during this pause, so it's
	// a responsiveness knob, not a safety one.
	tuneRestoreSettle time.Duration

	// FT8-TX keying state (ADR 0030), all mu-guarded — the second caller of the
	// guaranteed-stop machinery alongside tune. ft8TxActive is the single-flight
	// gate (mutually exclusive with tuneActive: one keyed transmission at a time
	// on one rig/one PTT); ft8TxRestoreMode is the pre-TX mode captured at
	// KeyFt8Tx and restored on unkey; ft8TxTimer is the hard auto-off backstop
	// that drops PTT if the FT8 controller ever goes silent. Power is left at the
	// operator's setting — no clamp (FT8 is a normal operating mode, not a tune
	// carrier). Reuses lastMode (above) for the restore snapshot and
	// tuneRestoreSettle for the post-unkey mode-restore settle.
	ft8TxActive      bool
	ft8TxRestoreMode string
	ft8TxTimer       *time.Timer
	// ft8TxGen mirrors tuneGen for the FT8-TX path (finding 6).
	ft8TxGen uint64
	// ft8Meters accumulates the rig's pushed ALC/PO/SWR readings for the
	// transmission in flight; ft8MeterLast holds the summary of the one that
	// most recently ended. Both are guarded by mu and cleared together with the
	// TX flags, so a reading can never be filed against a finished
	// transmission. See meters.go.
	ft8Meters    map[meterKey]*meterSample
	ft8MeterLast ft8MeterSummary
	// meterLatest is the rig's CURRENT reading per meter, recorded whether or
	// not anything is transmitting — observation is a layer below the
	// per-transmission diagnostic, so a consumer (a browser meter display) never
	// needs a transmission to come alive. meterSeen gates the once-per-pipeline
	// "this rig pushes meters" announcement. Both mu-guarded, both reset at
	// pipeline teardown by resetMeterObservation.
	meterLatest map[string]int
	meterSeen   bool
	// meterSel is the MS METER SW selection — which meter the pushed RM0 value
	// represents. Empty until the rig answers MS (it is in the READ burst).
	meterSel string

	// Drive-collapse detection (drivealarm.go), all mu-guarded.
	// meterSeenSinceTx is the instrument-alive evidence: at least one meter push
	// has arrived since the last transmission ended. Scoped that way rather than
	// per-pipeline like meterSeen, because an instrument that dies mid-session
	// must stop counting as known-good. driveLastMeterAt dates the most recent
	// push (set at key-down too, so silence is measured from the key).
	// driveAlarmed keeps it to one alarm per transmission; driveTimer is the
	// idle-timeout, generation-gated on ft8TxGen like the auto-off backstop.
	// driveSilence is this service's silence threshold, seeded from
	// driveSilenceTimeout — a field rather than the package var directly, like
	// confirmTimeout, so a test can shorten it without a pending timer racing
	// the restore of a global.
	meterSeenSinceTx bool
	driveLastMeterAt time.Time
	driveAlarmed     bool
	driveSilence     time.Duration
	driveTimer       *time.Timer

	// driveWatchArmed records that THIS transmission is actually being watched, so
	// a transmission that measured nothing is not mistaken for one that measured
	// normal output. driveAlarmStanding outlives a transmission: it is what makes
	// the recovery report answer a standing alarm rather than firing every slot.
	driveWatchArmed    bool
	driveAlarmStanding bool

	// driveWatchOutcome is THIS transmission's drive-watch state, reported on its
	// meters record; driveWatchState is the last state REPORTED to the operator and
	// is what the transition machine compares against.
	//
	// Two fields rather than one, deliberately. driveWatchOutcome is cleared by
	// disarmDriveWatch, so a transmission that never reaches the arm point (a failed
	// key write, a teardown racing it) omits the field instead of inheriting the
	// previous transmission's answer. driveWatchState must survive that clear, or
	// every state would look new on the next transmission and the transition lines
	// would degenerate into one per transmission — which is the whole thing the
	// design avoids (691 transmissions on 2026-08-01 against 460 warns in the entire
	// 15-day log).
	driveWatchOutcome string
	driveWatchState   string

	// drivePendingLog holds transitions recorded but not yet emitted (guarded by
	// s.mu); driveLogMu admits ONE emitter at a time so they reach the log in the
	// order the state changed.
	//
	// Both are needed because the state changes under s.mu and the emit happens
	// after the unlock. Without the queue, the read loop in observeMeter and
	// whoever is keying could record A then B and emit B then A, leaving "drive
	// detection restored" as the last line while the state was dark — inverting the
	// one fact this reporting exists for (codex P1 on 1273752d).
	drivePendingLog []driveWatchTransition
	driveLogMu      sync.Mutex

	// ft8MeterGen is the generation of the transmission the meter accumulator
	// belongs to, set where that generation is established (ft8tx.go) rather than
	// read at flush time — finishFt8Tx increments ft8TxGen BEFORE flushing, so the
	// live counter would label the summary with the NEXT transmission's number and
	// the join between the meters record, the drive alarm and the transition lines
	// would silently point at the wrong slot.
	ft8MeterGen uint64

	// driveSelTainted records that the rig's meter was switched AWAY from PO at
	// some point during this transmission, so its meter stream stopped being about
	// RF part-way through. The arm-time gate cannot cover this: the selection can
	// move while keyed (smd.log 2026-07-31 04:04:13 reports both meter_alc and
	// meter_po inside ONE transmission), which would otherwise leave a live
	// silence timer judging a stream that no longer measures output.
	//
	// It suppresses BOTH verdicts, and both directions matter. No alarm, because
	// the silence is meaningless; and no recovery, because "output was confirmed
	// normal" is a positive claim this transmission cannot support — retiring a
	// standing alarm on no evidence is the more dangerous of the two.
	driveSelTainted bool

	// Gap measurement over the keyed window — the instrument that answers whether
	// absent drive reliably leaves a silence wider than driveSilence (finding 1 of
	// 2026-07-30). Shares the detector's window exactly, opened and closed by
	// armDriveWatch/disarmDriveWatch: a measurement of a DIFFERENT window than the
	// one being judged would be worse than none. meterGapWindowAt zero means no
	// window is open, which is how a transmission that never keyed is kept from
	// reporting a gap measured against the zero time.
	// meterGapSealed freezes the result at the tx_off write, because the release
	// path then waits for confirmation, settles and restores mode with PTT already
	// down — counting that would inflate a healthy transmission's widest silence
	// past the very threshold it is measured against.
	meterGapWindowAt time.Time
	meterGapLastAt   time.Time
	meterGapMax      time.Duration
	meterGapKeyedFor time.Duration
	meterGapSealed   bool

	// TX-uncertainty + stuck-TX alarm state (ADR 0051, all mu-guarded except
	// where noted). txUncertain: an unkey (or possibly-keyed failed write) has
	// not been positively confirmed — the rig MAY be transmitting; key paths
	// refuse while set. txAlarmActive: the persistent operator alarm is
	// standing (raised on confirm-timeout / liveness-loss-while-keyed /
	// unconfirmable teardown; cleared only by positive RX confirmation).
	// txConfirmGen gates stale confirm-timeout callbacks (same pattern as
	// tuneGen/ft8TxGen). txConfirmViaRigData is armed ONLY for a successfully
	// written unkey on a non-ACK protocol whose rigdef has no TX-status query.
	// It prevents unrelated state from clearing uncertainty raised by a failed
	// CI-V unkey, liveness loss, or another alarm source.
	txUncertain         bool
	txAlarmActive       bool
	txConfirmGen        uint64
	txConfirmTimer      *time.Timer
	txConfirmViaRigData bool
	// txConfirmDone is closed when the current confirmation cycle resolves
	// (confirmed idle OR alarmed) so the release paths can gate their
	// best-effort restore on POSITIVE confirmation (2026-07-19 review P1:
	// restoring full power on a fixed settle could raise a still-keyed
	// carrier from tune power to operator power). Replaced per cycle by
	// beginTxConfirm; nil when no cycle is pending.
	txConfirmDone chan struct{}
	// txConfirmAfterFrame (mu-guarded) is the rxFrameCount watermark at the
	// moment the current confirmation cycle began: the any-rig-data fallback
	// (defs without a TX-status query) may only confirm on frames decoded
	// AFTER it - a frame that arrived before the unkey went out proves
	// nothing about the unkey (8bd88c1b review, ordering finding).
	txConfirmAfterFrame uint64

	// txStopRetrying single-flights the short re-unkey sequence that runs when
	// the rig positively reports it is still transmitting, or when a CI-V
	// defensive unkey receives no ACK. Without this, repeated evidence/failures
	// could stack retry goroutines.
	txStopRetrying bool

	// txAlarmProbeGen gates the alarm re-probe loop the way txConfirmGen gates
	// the confirm timeout: the loop reads it before every probe and exits when
	// it no longer matches, so a cleared-then-re-raised alarm never leaves two
	// loops running. Incremented on every raise AND on every clear.
	txAlarmProbeGen uint64
	// confirmTimeout is this service's copy of the package-level
	// txConfirmTimeout default, snapshotted at New(). Immutable for the
	// service's life, so the background goroutines that consult it (the
	// auto-off backstops → releaseX → waitTxConfirm) read per-instance state
	// instead of a package var. Tests shorten the knob before constructing a
	// Service; while the goroutines read the var directly, a timer armed by one
	// test could still fire during a LATER test that was writing it — a real
	// -race failure (~1 run in 12) that made the suite, and CI, intermittently
	// red for reasons unrelated to the code under test (2026-07-23).
	confirmTimeout time.Duration
	// lastTxStatus (mu-guarded) is the previous TXSTATUS value seen, so
	// observeTxStatus can log TRANSITIONS only. Without it the rig's answers
	// are invisible whenever txUncertain is false — which is most of the time,
	// and is exactly the window in which a control line keying the rig behind
	// our back would show up. Empty before the first observation.
	lastTxStatus string
	// runCtx is Start's derived context, kept so event-driven background work
	// (the tx-alarm re-probe loop) can be cancelled by Stop. nil before Start.
	runCtx context.Context

	// rxFrameCount counts successfully decoded rig frames (readLoop-owned
	// increments, atomic for cross-goroutine reads by the confirm machinery).
	rxFrameCount atomic.Uint64
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
	tuneSettle, settleClamped := resolveTuneRestoreSettle(cfg.Tune.RestoreSettleMs)
	if powerClamped && logger != nil {
		logger.WarnWith().
			Int("requested_w", cfg.Tune.PowerW).
			Int("clamped_w", tunePower).
			Msg("bridge: tune power_w above safe ceiling; clamped")
	}
	// The ceiling clamp knows nothing of the rig's own floor: a configured
	// power the active rigdef cannot encode (e.g. 1 W under the FTdx10's
	// PC min of 5) would fail EVERY StartTune at encode time while config
	// still advertised tune:true (2026-07-19 review P3). Fall back to the
	// default, which TuneSupported dry-runs. cfg.Cat is nil-able (bridge
	// disabled / not yet configured) — no driver, no floor to check.
	driver := ""
	if cfg.Cat != nil {
		driver = cfg.Cat.Driver
	}
	if def, ok := cat.Lookup(driver); ok && !validateTunePowerForDef(def, tunePower) {
		if logger != nil {
			logger.WarnWith().
				Int("requested_w", tunePower).
				Int("fallback_w", defaultTunePowerW).
				Str("driver", def.ID).
				Msg("bridge: tune power_w not encodable for this rig (below its minimum?); using default")
		}
		tunePower = defaultTunePowerW
	}
	if durClamped && logger != nil {
		logger.WarnWith().
			Int("requested_ms", cfg.Tune.MaxDurationMs).
			Int("clamped_ms", int(tuneDur.Milliseconds())).
			Msg("bridge: tune max_duration_ms above safe ceiling; clamped")
	}
	if settleClamped && logger != nil {
		logger.WarnWith().
			Int("requested_ms", cfg.Tune.RestoreSettleMs).
			Int("clamped_ms", int(tuneSettle.Milliseconds())).
			Msg("bridge: tune restore_settle_ms above ceiling; clamped")
	}
	// Resolve + clamp the supervisor backoff (see resolveBackoff for why). The
	// helper is shared with ResolveTimeouts so the /v1/config effective view can
	// never disagree with the running service (50e35d review P2). New keeps the
	// operator warning here: it wants the RAW initial to show what was capped, and
	// it must not fire on every /v1/config GET.
	rawBackoffInitial := resolveTimeout(cfg.Timeouts.BackoffInitialMs, supervisorInitialBackoff)
	backoffInitial, backoffMax := resolveBackoff(cfg.Timeouts.BackoffInitialMs, cfg.Timeouts.BackoffMaxMs)
	if rawBackoffInitial > backoffMax && logger != nil {
		logger.WarnWith().
			Int("initial_ms", int(rawBackoffInitial.Milliseconds())).
			Int("max_ms", int(backoffMax.Milliseconds())).
			Msg("bridge: effective backoff initial exceeds max; clamped to max")
	}
	// Some rigs report VFO-A/VFO-B frequencies and SELECT as separate frames.
	// Their dial is unknown until SELECT arrives. Definitions without SELECT
	// use the operating-VFO-A model, so A is known from the first frequency.
	selectedVfoExplicit := false
	if def, ok := cat.Lookup(driver); ok {
		selectedVfoExplicit = rigDefinitionHasStateTag(def, "SELECT")
	}
	selectedVfo := "A"
	if selectedVfoExplicit {
		selectedVfo = ""
	}
	return &Service{
		cfg:      cfg,
		logger:   logger,
		hub:      newHub(logger),
		stopDone: make(chan struct{}),
		openClient: func(c serial.Config) (serial.Client, error) {
			return serial.Open(c)
		},
		livenessTimeout:                resolveTimeout(cfg.Timeouts.LivenessMs, livenessTimeout),
		supervisorInitialBackoff:       backoffInitial,
		supervisorMaxBackoff:           backoffMax,
		supervisorSteadyStateThreshold: resolveTimeout(cfg.Timeouts.SteadyStateThresholdMs, supervisorSteadyStateThreshold),
		writeWatchdog:                  resolveTimeout(cfg.Timeouts.WriteWatchdogMs, writeWatchdog),
		civReadGap:                     clampDuration(resolveTimeout(cfg.Timeouts.CivReadGapMs, civReadGap), civReadGapMax),
		civAckTimeout:                  resolveTimeout(cfg.Timeouts.CivAckMs, civAckTimeout),
		civPollInterval:                resolveTimeout(cfg.Timeouts.CivPollIntervalMs, civPollInterval),
		civPollQuiet:                   resolveTimeout(cfg.Timeouts.CivPollQuietMs, civPollQuiet),
		ft8MeterPollInterval:           resolveTimeout(cfg.Timeouts.Ft8MeterPollIntervalMs, ft8MeterPollInterval),
		ft8MeterPollTimeout:            resolveTimeout(cfg.Timeouts.Ft8MeterPollTimeoutMs, ft8MeterPollTimeout),
		confirmTimeout:                 txConfirmTimeout,
		driveSilence:                   driveSilenceTimeout,
		tunePowerW:                     tunePower,
		tuneMaxDuration:                tuneDur,
		tuneRestoreSettle:              tuneSettle,
		lastSelectedVfo:                selectedVfo,
		lastSelectedVfoKnown:           !selectedVfoExplicit,
		selectedVfoExplicit:            selectedVfoExplicit,
	}
}

// rigDefinitionHasStateTag reports whether a rig definition can explicitly
// produce tag. It keeps dial-selection policy data-driven rather than tied to a
// manufacturer/protocol switch.
func rigDefinitionHasStateTag(def cat.RigDefinition, tag string) bool {
	for _, state := range def.States {
		for _, marker := range state.Markers {
			if marker.Tag == tag {
				return true
			}
		}
	}
	return false
}

// resolveTimeout converts a config-supplied milliseconds value to a
// time.Duration, falling back to the package-level default when the
// config value is zero (the operator omitted the key — daemon picks
// the built-in default). Centralised so the call sites in New
// stay terse.
func resolveTimeout(cfgMs int, defaultDur time.Duration) time.Duration {
	if cfgMs > 0 {
		return time.Duration(cfgMs) * time.Millisecond
	}
	return defaultDur
}

// resolveBackoff resolves the supervisor's initial + max backoff from config
// (package defaults where a value is zero) and clamps the initial to the max on
// the EFFECTIVE values. `backoff_max_ms: 50` with the initial omitted would
// otherwise sleep the 1 s default before the 50 ms cap could apply — config
// validation only compares the two RAW settings, so it never fires on that mixed
// case (2026-07-23 review P2). Clamping here rather than in config is deliberate:
// the defaults are bridge-package constants the config layer has no business
// knowing. Shared by New and ResolveTimeouts so the effective view served by
// /v1/config matches what the supervisor actually runs (50e35d review P2).
func resolveBackoff(initialMs, maxMs int) (initial, max time.Duration) {
	initial = resolveTimeout(initialMs, supervisorInitialBackoff)
	max = resolveTimeout(maxMs, supervisorMaxBackoff)
	if initial > max {
		initial = max
	}
	return initial, max
}

// clampDuration caps d at max (and floors a negative at zero). Used for the
// CI-V read gap so an operator typo can't stall every state snapshot.
func clampDuration(d, max time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > max {
		return max
	}
	return d
}

// ResolveTimeouts returns the EFFECTIVE bridge timeouts — operator values where
// set, built-in package defaults where zero, the civ-read-gap ceiling applied —
// as a fully populated config in milliseconds. It mirrors the resolution New
// applies into the Service (the same resolveTimeout / clampDuration helpers and
// package-default symbols), so the value served here is exactly what a running
// Service would use. Exposed so the /v1/config GET can serve effective bridge
// timeouts without materialising them into config.json (config.md §15
// sparse-but-served): the file stays sparse, the API reports the resolved view.
func ResolveTimeouts(c types.BridgeTimeoutsConfig) types.BridgeTimeoutsConfig {
	ms := func(d time.Duration) int { return int(d / time.Millisecond) }
	// Same resolve+clamp New applies, so the served initial/max obey the same
	// initial ≤ max invariant the supervisor runs under (50e35d review P2).
	backoffInitial, backoffMax := resolveBackoff(c.BackoffInitialMs, c.BackoffMaxMs)
	return types.BridgeTimeoutsConfig{
		LivenessMs:             ms(resolveTimeout(c.LivenessMs, livenessTimeout)),
		BackoffInitialMs:       ms(backoffInitial),
		BackoffMaxMs:           ms(backoffMax),
		SteadyStateThresholdMs: ms(resolveTimeout(c.SteadyStateThresholdMs, supervisorSteadyStateThreshold)),
		WriteWatchdogMs:        ms(resolveTimeout(c.WriteWatchdogMs, writeWatchdog)),
		CivReadGapMs:           ms(clampDuration(resolveTimeout(c.CivReadGapMs, civReadGap), civReadGapMax)),
		CivAckMs:               ms(resolveTimeout(c.CivAckMs, civAckTimeout)),
		CivPollIntervalMs:      ms(resolveTimeout(c.CivPollIntervalMs, civPollInterval)),
		CivPollQuietMs:         ms(resolveTimeout(c.CivPollQuietMs, civPollQuiet)),
		Ft8MeterPollIntervalMs: ms(resolveTimeout(c.Ft8MeterPollIntervalMs, ft8MeterPollInterval)),
		Ft8MeterPollTimeoutMs:  ms(resolveTimeout(c.Ft8MeterPollTimeoutMs, ft8MeterPollTimeout)),
	}
}

// ResolveTune returns the EFFECTIVE tune-carrier params — defaults where zero,
// clamped to the hard safety ceilings — reusing the same resolveTune* helpers
// New uses, so the served view matches runtime. Same sparse-but-served rationale
// as ResolveTimeouts; the ceilings stay non-overridable in this package. driver
// selects the active rigdef so the def-floor fallback matches New (2026-07-19
// review P3); an unknown driver serves the plain clamp.
func ResolveTune(c types.BridgeTuneConfig, driver string) types.BridgeTuneConfig {
	powerW, _ := resolveTunePower(c.PowerW)
	if def, ok := cat.Lookup(driver); ok && !validateTunePowerForDef(def, powerW) {
		powerW = defaultTunePowerW
	}
	dur, _ := resolveTuneDuration(c.MaxDurationMs)
	settle, _ := resolveTuneRestoreSettle(c.RestoreSettleMs)
	return types.BridgeTuneConfig{
		PowerW:          powerW,
		MaxDurationMs:   int(dur / time.Millisecond),
		RestoreSettleMs: int(settle / time.Millisecond),
	}
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
	// Retained so background work started later by an EVENT rather than by
	// Start — currently the tx-alarm re-probe loop — can be cancelled by Stop
	// instead of running out its own bounded schedule while the port closes
	// underneath it.
	s.runCtx = runCtx
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

// noDataStrikeLimit is how many CONSECUTIVE no-data liveness timeouts mark the
// rig as dropped for RigConnected. Normal static operation produces at most one
// strike per cycle (timeout → probe → the rig answers → reset), so a limit of 2
// never trips on a quiet-but-alive rig while a genuinely dead rig (no probe
// recovery) trips after ~2 liveness cycles. A small const — the exact value just
// trades mic-release latency on a real drop against tolerance of one flaky probe.
const noDataStrikeLimit int32 = 2

// RigConnected reports whether the rig is connected, identity-confirmed, AND
// currently responding — the "CAT is live" signal. It is the liveness core of
// TxReady WITHOUT the tune/FT8-TX single-flight flags (irrelevant to "is the rig
// powered and talking"), PLUS a no-data guard: a passive disconnect (rig went
// silent, serial port still open) leaves activeClient/identityConfirmed set —
// they clear only on pipeline exit — so without the strike check this would stay
// true through a real mid-session drop and the FT8 capture gate would never
// release the mic. The FT8 subsystem gates microphone acquisition on this
// (ft8.Service.SetCatGate in cmd/smd). Nil-safe: an absent/disabled bridge reads
// "not connected."
func (s *Service) RigConnected() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rigWritableLocked()
}

// TxActive reports whether a tune carrier or FT8 transmission is CURRENTLY keyed
// (the same single-flight snapshot SendCommands guards on). Used to refuse a
// daemon restart while actively transmitting — the operator must stop TX first.
// Deliberately EXCLUDES txUncertain: an unconfirmed/stuck TX is exactly the case
// where a RECOVERY restart must stay possible (2026-07-21 stuck-TX incident, where
// the operator restarts to recover). Nil-safe (absent/disabled bridge → false).
func (s *Service) TxActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tuneActive || s.ft8TxActive
}

// rigWritableLocked is THE live/write-ready predicate every mutating entry
// point agrees on (2026-07-18 TX-safety review, finding 3): port captured,
// identity confirmed (H2), and the liveness strike count below the disconnect
// limit — a rig the bridge already reads as non-responsive (RigConnected
// false) must not be keyable or commandable either; before this, TxReady /
// KeyFt8Tx / StartTune / SendCommands ignored strikes and would happily key a
// connection the readLoop had declared dead. Caller holds s.mu (the strike
// counter itself is atomic; folding it here keeps one predicate, not four).
func (s *Service) rigWritableLocked() bool {
	return s.activeClient != nil && s.identityConfirmed &&
		s.noDataStrikes.Load() < noDataStrikeLimit
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
	// Advisory multi-tab awareness (EventRigClients): announce the subscriber count
	// ONLY when it signals a multi-tab situation, so the common single-tab case (and
	// every single-subscriber path) never sees this event — the SPA defaults to
	// "one tab, no banner". On join we announce once the count reaches >= 2 (a second
	// tab appeared, so both should warn); on leave we announce whenever >= 1 tab
	// remains, which can only happen after a teardown that had been multi-tab, so the
	// remaining tab's banner clears. A lone tab from start to finish gets nothing.
	// clientsMu holds mutation + count + publish together so concurrent
	// join/leave pairs publish in a consistent order (2026-07-19 review P3 —
	// an interleaved stale count could otherwise be the LAST event).
	s.clientsMu.Lock()
	ch, unsub := s.hub.subscribe()
	if n := s.hub.subscriberCount(); n >= 2 {
		s.publishClientCount(n)
	}
	s.clientsMu.Unlock()
	wrapped := func() {
		s.clientsMu.Lock()
		unsub() // idempotent
		if n := s.hub.subscriberCount(); n >= 1 {
			s.publishClientCount(n)
		}
		s.clientsMu.Unlock()
	}
	return ch, wrapped
}

// publishClientCount fans the given rig-event subscriber count out to every tab.
// Advisory only. Callers hold clientsMu, which orders the count snapshot and
// this broadcast against concurrent joins/leaves — without it a stale count
// could be the LAST event published and stick a lone tab's multi-tab banner
// (2026-07-19 review P3).
func (s *Service) publishClientCount(n int) {
	s.hub.publish(Event{Name: EventRigClients, Payload: RigClientsPayload{Count: n}})
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
	civ := s.bootstrapCIV
	keyed := s.tuneActive || s.ft8TxActive
	s.mu.Unlock()
	if cl == nil || len(bb) == 0 {
		return nil
	}
	// CI-V only: while a transmission is keyed, a multi-frame bootstrap would
	// hold cmdMu for seconds directly ahead of an emergency tx_off on the same
	// mutex (2026-07-18 TX-safety review, finding 5). Skip — the subscriber
	// still gets live pushes, and the next bootstrap/poll fills the snapshot
	// once the carrier is down. Yaesu bootstraps are one cmdMu-free write and
	// stay as they were. Checked OUTSIDE the lock for the cheap fast path AND
	// re-checked INSIDE the closure (a1d031cf review): a key completing between
	// the snapshot above and cmdMu acquisition would otherwise still run the
	// long snapshot with TX active.
	if civ && keyed {
		s.logger.DebugWith().Msg("bridge: skipping CI-V bootstrap snapshot while TX is keyed")
		return nil
	}
	// CI-V: serialise behind cmdMu so a new subscriber's bootstrap READ burst
	// can't interleave an in-flight command/key ACK sequence (review M2).
	return s.underCmdMuCIV(civ, func() error {
		if civ {
			s.mu.Lock()
			nowKeyed := s.tuneActive || s.ft8TxActive
			s.mu.Unlock()
			if nowKeyed {
				s.logger.DebugWith().Msg("bridge: skipping CI-V bootstrap snapshot while TX is keyed (post-lock)")
				return nil
			}
		}
		return s.writeSnapshotReads(ctx, cl, civ, bb)
	})
}
