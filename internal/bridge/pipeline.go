package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/rigserial"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// livenessTimeout is the default data-flow silence window after
// which the bridge concludes the rig is unresponsive and emits a
// rig-disconnected event (per ADR 0010 passive liveness). Operators
// can override per-deployment via `bridge.timeouts.liveness_ms` in
// config.json — Service.New snapshots the override at construction
// time. Defaults to 5s (changed from 30s on 2026-05-16): on rigs
// whose USB-serial layer doesn't surface kernel disconnects when the
// rig is powered off (FTdx10 family), 30s was too long to notice a
// short rig-off / rig-on cycle. The no-data branch's INIT+READ probe
// makes false-positives during legitimate idle self-recovering, so a
// shorter default is safe.
//
// Still a package-level var so tests can dial it down (read by
// Service.New when cfg.Timeouts.LivenessMs is zero).
var livenessTimeout = 5 * time.Second

// Supervisor backoff defaults. Package vars so tests can dial them
// down to milliseconds without sleeping for real seconds. Operators
// override via `bridge.timeouts.{backoff_initial_ms,backoff_max_ms,
// steady_state_threshold_ms}` in config.json.
var (
	supervisorInitialBackoff       = 1 * time.Second
	supervisorMaxBackoff           = 30 * time.Second
	supervisorSteadyStateThreshold = 10 * time.Second
)

// writeWatchdog bounds a single serial write (passed to
// serial.Config.WriteTimeoutMS). go.bug.st/serial has no write deadline, so a
// hung port.Write would otherwise block the writer goroutine — and the tune
// guaranteed-stop — forever; on overrun the port closes and the supervisor
// reopens. Generous on purpose (a hang backstop, not a per-write SLA); operators
// override via `bridge.timeouts.write_watchdog_ms`. Package var so tests can
// dial it down (review 2026-06-04 H4).
var writeWatchdog = 2 * time.Second

// civReadGap is the default inter-frame delay between the read frames of a CI-V
// state snapshot. CI-V is half-duplex: a back-to-back read-freq + read-mode
// burst makes the rig answer only the last read (bench 2026-06-15 — fresh SPA
// tab showed stale freq, current mode), so the snapshot reads are spaced. Only
// the icom_civ path uses it; operators override via
// `bridge.timeouts.civ_read_gap_ms`. Package var so tests can dial it to zero.
var civReadGap = 50 * time.Millisecond

// civReadGapMax caps the configured gap so a typo (e.g. 50000) can't stall
// every snapshot for a minute. A snapshot of N reads waits (N-1)×gap total, so
// the ceiling stays well under the liveness window.
const civReadGapMax = 2 * time.Second

// identityReprobeInterval bounds how often readLoop re-issues the READ snapshot
// (which carries the ID query) while the rig is decoding frames but its identity
// is still unconfirmed. The connect-time READ's ID reply can be lost — the rig
// isn't always ready to answer the first query right after power-on, and a
// dropped first frame in the burst takes ID (it leads READ) with it. AI-mode
// state pushes then keep the link alive frame-by-frame, so the no-data liveness
// probe — readLoop's ONLY other READ re-issue — never fires, and identity stays
// unconfirmed (every write path blocked, FT8 capture deferred) for the whole
// session even though the rig is plainly talking. Re-probing on this cadence
// re-asks "who are you?" until an IDENTITY frame arrives, recovering within one
// interval without flooding the bus. READ is a pure query burst, so this never
// keys TX — H2 is unaffected (writes stay blocked until the ID actually
// confirms). Package var so tests can dial it down.
var identityReprobeInterval = 1 * time.Second

// civAckTimeout is the default wait for a CI-V command's FB/FA ACK after writing
// each frame (ADR 0034 wait-for-ACK). The IC-7300 ACKs in ~20ms and never
// broadcasts a commanded change, so the command path adopts state on FB / errors
// on FA rather than waiting for a push. 500ms is generous headroom over the
// measured round-trip. Only the icom_civ path uses it; operators override via
// `bridge.timeouts.civ_ack_ms`. Package var so tests can dial it down.
var civAckTimeout = 500 * time.Millisecond

// civPollInterval is the default steady-state cadence of the Icom state-mirror
// poll (ADR 0035): how often the daemon fires the rigdef's POLL read-list to
// mirror the fields Transceive never pushes (non-operating VFO, mode data flag,
// split). 1s keeps the slow-field display lag ≤ ~1s while staying light on the
// bus. Only icom_civ rigs that declare a POLL command poll; operators override
// via `bridge.timeouts.civ_poll_interval_ms`. Package var so tests can dial it.
var civPollInterval = 1 * time.Second

// civPollQuiet is the default collision back-off: a poll tick is skipped if a
// Transceive broadcast arrived within this window (the rig is mid dial-turn
// storm on the half-duplex bus). Freq is pushed in real-time during a spin
// anyway, and the skipped gap-read recovers on the next tick — so deferring
// avoids the benign collisions seen on the bench (2026-06-15) at no cost.
var civPollQuiet = 250 * time.Millisecond

// pipelineExitClass tells runSupervisor what to do after runPipeline
// returns. Classified at the failure site rather than divined from
// the exit code, so retry policy lives at the call site (where the
// "is this fixable by trying again?" judgement is local) instead of
// being a giant switch in the supervisor.
type pipelineExitClass int

const (
	// exitContextCancelled — parent ctx done (Service.Stop or daemon
	// shutdown). Supervisor returns silently.
	exitContextCancelled pipelineExitClass = iota
	// exitPermanent — config / rigdef errors that retrying can't fix
	// (unknown driver, missing INIT, identity mismatch on first
	// match, etc.). Supervisor publishes the existing bridge-error
	// once via the dedup helper, then exits without retrying.
	exitPermanent
	// exitTransient — runtime failures that may resolve themselves
	// (port file not yet present, INIT write to a powered-off rig,
	// terminal serial error from a cable yank). Supervisor sleeps
	// with backoff and re-enters runPipeline.
	exitTransient
)

// initCommandName is the rigdef command-table entry that arms the
// rig's AUTO-mode push state. For Yaesu/Kenwood that's `AI1;` — sent
// once at pipeline startup and silent on the wire (no response
// expected). Both shipping rigdefs define it; rigdefs that lack it
// surface a loud bridge-error at startup.
const initCommandName = "INIT"

// readCommandName is the rigdef command-table entry that requests a
// full identity + state snapshot. Sent on each new SSE-open via
// TriggerBootstrap so a freshly-connected SPA tab gets current rig
// state without waiting for the operator to wiggle the dial (per ADR
// 0019 active-snapshot model). For Yaesu/Kenwood that's
// `ID;FA;FB;ST;VS;MD0;MD1;PC;` — 8 framed responses the readLoop
// decodes and publishes as a sequence of partial rig-state events.
const readCommandName = "READ"

// pollCommandName is the optional steady-state state-mirror read-list (ADR
// 0035). A rigdef that declares it (icom_civ rigs whose Transceive push is
// structurally incomplete) gets a low-rate poll of the un-pushed fields; a
// rigdef without it (every Yaesu, whose AI push is complete) runs no poll loop.
// The presence of the command is the data-driven on/off switch.
const pollCommandName = "POLL"

// runPipeline opens the serial port via s.openClient, looks up the
// rig def via cat.Lookup, sends the rigdef's INIT command, then
// loops decoding push lines and publishing typed events to the hub.
// Returns a classified exit so runSupervisor can decide whether to
// retry (transient runtime fault), give up (operator-actionable
// permanent error), or terminate (parent ctx cancelled).
//
// Termination semantics:
//
//   - Parent ctx cancelled (Service.Stop / daemon shutdown):
//     exitContextCancelled, no publish (deliberate shutdown).
//   - The liveness timeout (5s default) without a line from the rig: publish rig-disconnected
//     once and keep waiting in the read loop. If data resumes,
//     subsequent rig-state events implicitly tell the SPA the rig
//     is alive again (bridge.svelte.ts flips rigResponding=true on
//     any rig-state arrival per ADR 0009).
//   - Terminal serial error (port closed/EIO/permission revoked):
//     publish rig-disconnected with the error reason, return
//     exitTransient. The supervisor reopens the port after a
//     backoff sleep — covers cable yank / power-spike recovery
//     during a session.
//   - Initial open failure / INIT write to a powered-off rig:
//     publish bridge-error, return exitTransient. The supervisor
//     retries with backoff so first-boot ordering (daemon up
//     before rig) self-heals when the operator switches on.
//   - Config / rigdef errors (unknown driver, missing INIT/READ,
//     bad serial config): publish bridge-error, return
//     exitPermanent. The supervisor gives up — retrying won't fix
//     an operator typo.
func (s *Service) runPipeline(ctx context.Context) pipelineExitClass {
	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		s.logger.ErrorWith().
			Str("driver", s.cfg.Cat.Driver).
			Msg("bridge: unknown CAT driver; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeUnknownDriver, map[string]string{"driver": s.cfg.Cat.Driver})
		return exitPermanent
	}

	serialCfg, err := buildSerialConfig(*s.cfg.Serial, def.Serial)
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("bridge: serial config build failed; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeSerialConfigInvalid, map[string]string{"error": errMessage(err)})
		return exitPermanent
	}
	// Bound serial writes with the bridge's hang backstop (review H4). This is
	// a bridge-policy timeout, not a rigdef-derived serial parameter, so it's
	// applied here rather than inside buildSerialConfig.
	serialCfg.WriteTimeoutMS = int(s.writeWatchdog / time.Millisecond)

	// On Unix the OS can't set RTS/DTR before opening the port, so a rigdef that
	// de-asserts them (e.g. the IC-7300, to keep USB SEND from keying PTT) can
	// still see a brief assert pulse at open — go.bug.st/serial can't guarantee
	// otherwise (review 2026-06-19 H1). Warn once per connect so the operator
	// knows the rig-side mitigation (USB SEND = OFF) is the real fix.
	if rigserial.OpenMayPulseLines(serialCfg) {
		s.logger.WarnWith().Str("port", serialCfg.PortName).
			Msg("bridge: OS may briefly assert RTS/DTR at open; ensure the rig's PTT-on-control-line (e.g. IC-7300 USB SEND) is OFF")
	}

	// Pre-encode INIT and READ before touching hardware. Both encodes
	// are pure (no I/O); if either rigdef entry is missing, fail fast
	// without acquiring the port. Encode-after-open would mean a
	// permission-denied stall on a misconfigured rigdef. Reordered
	// per internal-bridge-pipeline.md review #4.
	initBytes, err := cat.Encode(def, initCommandName)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("driver", def.ID).
			Str("command", initCommandName).
			Msg("bridge: rigdef has no INIT command; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeMissingInit, map[string]string{"driver": def.ID})
		return exitPermanent
	}
	readBytes, err := cat.Encode(def, readCommandName)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("driver", def.ID).
			Str("command", readCommandName).
			Msg("bridge: rigdef has no READ command; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeMissingRead, map[string]string{"driver": def.ID})
		return exitPermanent
	}
	// POLL is optional (ADR 0035): only rigdefs whose push is structurally
	// incomplete declare it. Absent → no poll loop (pollBytes stays nil). A
	// declared-but-unencodable POLL is a rigdef bug, but non-fatal — log and run
	// push-only rather than refusing the whole pipeline.
	var pollBytes []byte
	if cat.HasCommand(def, pollCommandName) {
		if pollBytes, err = cat.Encode(def, pollCommandName); err != nil {
			s.logger.WarnWith().Err(err).Str("driver", def.ID).
				Msg("bridge: rigdef POLL command failed to encode; running push-only (no state-mirror poll)")
			pollBytes = nil
		}
	}
	// METERPOLL is optional exactly like POLL (ADR 0064): only rigs whose CAT
	// answers explicit meter reads during TX declare it. Absent → no FT8 meter
	// poll loop; declared-but-unencodable is a rigdef bug, non-fatal.
	var meterPollBytes []byte
	if cat.HasCommand(def, meterPollCommandName) {
		if meterPollBytes, err = cat.Encode(def, meterPollCommandName); err != nil {
			s.logger.WarnWith().Err(err).Str("driver", def.ID).
				Msg("bridge: rigdef METERPOLL command failed to encode; running without FT8 meter poll")
			meterPollBytes = nil
		}
	}

	client, err := s.openClient(serialCfg)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("port", serialCfg.PortName).
			Int("baud", serialCfg.BaudRate).
			Msg("bridge: serial open failed; pipeline not started")
		// Refine the open failure into an actionable code when serial recognises
		// the cause (the raw OS error is unhelpful jargon); the SPA renders the
		// specific fix keyed on the code. Fall back to the generic code otherwise.
		port := map[string]string{"port": serialCfg.PortName}
		switch {
		case stderr.Is(err, serial.ErrPermissionDenied):
			s.publishExitBridgeError(BridgeErrCodeSerialPermissionDenied, port)
		case stderr.Is(err, serial.ErrPortBusy):
			s.publishExitBridgeError(BridgeErrCodeSerialPortBusy, port)
		case stderr.Is(err, serial.ErrPortNotFound):
			s.publishExitBridgeError(BridgeErrCodeSerialPortNotFound, port)
		default:
			s.publishExitBridgeError(BridgeErrCodeSerialOpenFailed, map[string]string{"port": serialCfg.PortName, "error": errMessage(err)})
		}
		// Port file may appear later (FTdx10 / FT-710 with built-in
		// USB CAT exposes /dev/ttyUSBn only when powered on) — let
		// the supervisor retry.
		return exitTransient
	}
	defer func() {
		// Teardown owns the TX transition (ADR 0051): holding keyMu means any
		// in-flight key/release completes FIRST (its TX1 lands before our
		// final TX0 below — never after), and a key still waiting on keyMu
		// finds activeClient nil once we clear it and refuses. This closes the
		// teardown-TX0 → pending-TX1 → close race the 2026-07-18 review's
		// finding 2 identified; the old comment called the late write
		// "benign", which was true for ordinary commands and exactly wrong
		// for TX1. Lock order keyMu → cmdMu → mu is preserved
		// (unkeyOnTeardown takes cmdMu/mu inside).
		s.keyMu.Lock()
		defer s.keyMu.Unlock()
		// Clear activeClient under mu BEFORE closing the port: a late caller
		// that races us takes the mu, sees nil, and returns the no-op branch.
		s.mu.Lock()
		s.activeClient = nil
		s.bootstrapBytes = nil
		s.bootstrapCIV = false
		s.pollBytes = nil
		// Identity must be re-verified on the next pipeline instance — a
		// hot-swapped rig (or a reconnect after the wrong rig was fixed)
		// must not inherit the previous run's confirmation (H2).
		s.identityConfirmed = false
		s.mu.Unlock()
		// Guaranteed stop on the daemon's OWN exit too (F1): unkey while the
		// port is still writable, before Close. See unkeyOnTeardown. The fault
		// flag discriminates the teardown cause (2026-07-19 review P1): a live
		// ctx here means the pipeline is dying of an ERROR — the link just
		// proved untrustworthy, so a write-accepted unkey alarms instead of
		// quietly entering uncertainty; a cancelled ctx is the healthy
		// operator-initiated shutdown, which keeps the ADR 0051 quiet-uncertain
		// trade (the next connection's defensive recovery verifies).
		s.unkeyOnTeardown(def, client, ctx.Err() == nil)
		_ = client.Close()
		// Release any active tune — the carrier is down (we just unkeyed above,
		// or it dropped with the rig); clear state, cancel the backstop, forget
		// the stale snapshot, and tell the SPA (ADR 0027).
		s.clearTuneOnDisconnect()
		// Release any active FT8 TX the same way — PTT dropped (ADR 0030).
		s.clearFt8TxOnDisconnect()
		// Drop meter observation with the instance that produced it: a reconnect
		// may be a different rig, or the same rig with AI no longer armed, so the
		// "does this rig push meters" answer is re-established rather than
		// inherited — and stale readings never linger as if current (meters.go).
		s.resetMeterObservation()
	}()

	if err := client.WriteCommandBytes(ctx, initBytes); err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("driver", def.ID).
			Msg("bridge: failed to send INIT; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeInitWriteFailed, map[string]string{"driver": def.ID, "error": errMessage(err)})
		// INIT write to a powered-off rig fails at the serial layer
		// — same recovery shape as a missing port file, supervisor
		// retries until the rig is alive.
		return exitTransient
	}

	// Pull a fresh snapshot immediately by sending READ right after
	// INIT. Without this, a supervisor-driven pipeline restart while
	// the SPA's EventSource stays connected leaves catState blank
	// until the operator wiggles a dial — no new SSE-open means no
	// TriggerBootstrap to fire READ. With this, every pipeline cycle
	// (initial OR recovered) pulls a snapshot the alive rig answers
	// immediately, and rigResponding flips true on the first decoded
	// rig-state. Write failure is logged-only (not a bridge-error):
	// if the port is genuinely bad, the readLoop will surface it as
	// a terminal-read error within milliseconds; if it's a transient
	// flake, the readLoop probe re-issues READ on the next timeout.
	civSnapshot := def.Protocol == cat.ProtocolIcomCIV
	if err := s.underCmdMuCIV(civSnapshot, func() error {
		return s.writeSnapshotReads(ctx, client, civSnapshot, readBytes)
	}); err != nil {
		s.logger.WarnWith().
			Err(err).
			Str("driver", def.ID).
			Msg("bridge: post-INIT READ snapshot write failed; relying on readLoop probe")
	}

	// Stash the live client + pre-encoded bootstrap bytes so the SSE
	// handler can fire READ on each new Subscribe via TriggerBootstrap.
	// The defer above clears them on pipeline exit.
	s.mu.Lock()
	s.activeClient = client
	s.bootstrapBytes = readBytes
	s.bootstrapCIV = civSnapshot
	s.pollBytes = pollBytes
	s.mu.Unlock()
	// Fresh pipeline run: clear any no-data strikes carried over from a prior
	// run's drop so RigConnected starts clean (it still gates on
	// identityConfirmed, which the teardown reset to false).
	s.noDataStrikes.Store(0)

	s.logger.InfoWith().
		Str("port", serialCfg.PortName).
		Int("baud", serialCfg.BaudRate).
		Str("driver", def.ID).
		Msg("bridge: pipeline started; AUTO-mode CAT data flow active")

	// Start the state-mirror poll loop (ADR 0035) for a rigdef that declares
	// POLL. It runs for this pipeline instance only; the deferred cancel + wait
	// (registered so they run BEFORE the client.Close defer above) stop and drain
	// it before the port closes, so no poll write can hit a closed port.
	if len(pollBytes) > 0 {
		pollCtx, pollCancel := context.WithCancel(ctx)
		var pollWg sync.WaitGroup
		pollWg.Add(1)
		go func() {
			defer pollWg.Done()
			s.runPollLoop(pollCtx, client, pollBytes)
		}()
		defer pollWg.Wait()
		defer pollCancel()
	}

	// The ADR 0064 FT8 meter poll — same lifecycle discipline as the state-
	// mirror loop above (cancel + drain before the port closes), different
	// keyed-interval policy (it polls THROUGH keyed slots; see meterpoll.go).
	if len(meterPollBytes) > 0 {
		mpCtx, mpCancel := context.WithCancel(ctx)
		var mpWg sync.WaitGroup
		mpWg.Add(1)
		go func() {
			defer mpWg.Done()
			s.runMeterPollLoop(mpCtx, client, meterPollBytes, civSnapshot)
		}()
		defer mpWg.Wait()
		defer mpCancel()
	}

	return s.readLoop(ctx, client, def, initBytes, readBytes)
}

// unkeyOnTeardown is the guaranteed stop for the daemon's OWN exit (F1). When
// runPipeline tears down mid-tune / mid-FT8-TX via ctx cancel (Service.Stop — a
// systemctl restart or task deploy), the rig is HEALTHY, the port is still open,
// and PTT is CAT-keyed up: the carrier does NOT drop with us and the auto-off
// timer dies with the process. So before the port closes, best-effort write
// tx_off. The rig-died paths (dead / closed port) can't be served — that write
// just fails harmlessly — and clearTune/clearFt8 reconcile state on every path.
// tx_off encodes here for the same reason the pre-key gate proves it encodable
// before keying. Runs on context.Background(): the request/parent ctx is already
// cancelled by the time we tear down, and the unkey must not be skipped for that.
//
// fault marks an ERROR-shaped teardown (terminal serial error / read failure —
// the 2026-07-18 incident shape) as opposed to a ctx-cancelled healthy
// shutdown. On a fault the link has just proven untrustworthy, so even a
// write-ACCEPTED unkey alarms (2026-07-19 review P1: the supervisor may never
// reconnect to run the defensive recovery, and "OS accepted" means least
// exactly when the pipe is dying); on a healthy shutdown the quiet
// txUncertain trade of ADR 0051 stands.
func (s *Service) unkeyOnTeardown(def cat.RigDefinition, client serial.Client, fault bool) {
	s.mu.Lock()
	// Include txUncertain (8bd88c1b review): an unconfirmed prior unkey means
	// the PTT may still be owned — the teardown tx_off is exactly the cheap
	// insurance that situation wants.
	keyed := s.tuneActive || s.ft8TxActive || s.txUncertain
	s.mu.Unlock()
	if !keyed {
		return
	}
	txOff, err := cat.Encode(def, tuneTxOffCommand)
	if err != nil {
		s.logger.WarnWith().Err(err).Msg("bridge: teardown unkey encode failed")
		// Couldn't even encode an unkey while keyed: the rig may still be
		// transmitting after we exit — raise the persistent alarm (ADR 0051);
		// the SPA keeps the banner across our restart, and the next confirmed
		// connection's unconditional defensive unkey recovers.
		s.raiseTxAlarm(TxAlarmTeardownUnconfirmed)
		return
	}
	// Take cmdMu on CI-V (half-duplex bus guard) so the unkey frame can't
	// interleave with an in-flight command/key sequence. The poll loop is
	// already drained by this point in teardown; the only remaining cmdMu
	// holder is an in-flight SendCommands awaiting an ACK the now-dead readLoop
	// won't deliver, so this waits at most civAckTimeout per frame — bounded,
	// not unbounded.
	civ := def.Protocol == cat.ProtocolIcomCIV
	if err := s.underCmdMuCIV(civ, func() error {
		return client.WriteCommandBytes(context.Background(), txOff)
	}); err != nil {
		s.logger.WarnWith().Err(err).Msg("bridge: teardown unkey write failed (rig may be gone)")
		// The final unkey never left this host while the rig was keyed — the
		// carrier may still be up (the 2026-07-18 incident's exact shape).
		// Raise the persistent alarm (ADR 0051); the next confirmed
		// connection's defensive unkey + confirmation cycle recovers.
		s.raiseTxAlarm(TxAlarmTeardownUnconfirmed)
		return
	}
	// Write-accepted is NOT confirmed (the incident's exact lesson), and the
	// port closes before any answer could arrive.
	if fault {
		// Error-shaped teardown: the pipe just failed us mid-session, which is
		// precisely when a stalled endpoint swallows writes silently — and the
		// supervisor may never get a reconnect to run the defensive recovery.
		// Alarm now; a later confirmed connection clears it (2026-07-19 P1).
		s.logger.ErrorWith().
			Msg("bridge: teardown unkey on faulted pipeline — write accepted but unconfirmable; raising alarm")
		s.raiseTxAlarm(TxAlarmTeardownUnconfirmed)
		return
	}
	// Healthy shutdown: retain txUncertain rather than declaring the stop
	// guaranteed (8bd88c1b review). No banner alarm (alarm-fatigue trade
	// recorded in ADR 0051); the state survives an in-process reconnect, and
	// the next connection's UNCONDITIONAL defensive recovery confirms — or
	// alarms — either way.
	s.mu.Lock()
	s.txUncertain = true
	s.mu.Unlock()
	s.logger.InfoWith().Msg("bridge: unkeyed TX on pipeline teardown (unconfirmed — next connection verifies)")
}

// runPollLoop fires the rigdef's POLL read-list on a low-rate ticker to mirror
// the Icom state CI-V Transceive never pushes (non-operating VFO, mode data
// flag, split) — ADR 0035. The replies ride the normal readLoop → decode →
// publish path; this loop only writes. It is collision-aware (skips a tick while
// the rig is mid Transceive burst — a dial-turn storm) and serialises with the
// command path through cmdMu so a poll and a SendCommands batch never interleave
// their frames on the half-duplex bus. Doubles as a liveness keepalive: while it
// runs the rig answers each interval, so a stall surfaces as a read timeout.
func (s *Service) runPollLoop(ctx context.Context, client serial.Client, pollBytes []byte) {
	ticker := time.NewTicker(s.civPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			busy := time.Since(s.lastBroadcastAt) < s.civPollQuiet
			keyed := s.tuneActive || s.ft8TxActive
			s.mu.Unlock()
			if busy {
				continue // collision back-off: rig is mid-broadcast (dial spin)
			}
			if keyed {
				// While a transmission is keyed, the multi-frame snapshot would
				// hold cmdMu for seconds (five frames × the inter-frame gap) —
				// directly ahead of an emergency tx_off waiting on the same
				// mutex (2026-07-18 TX-safety review, finding 5). The mirror
				// state can wait a tick; the unkey cannot.
				continue
			}
			err := s.underCmdMuCIV(true, func() error {
				// Re-check keyed UNDER cmdMu (a1d031cf review): a key completing
				// between the tick's snapshot and lock acquisition must still
				// defer the poll — the unkey path waits on this same mutex.
				s.mu.Lock()
				nowKeyed := s.tuneActive || s.ft8TxActive
				s.mu.Unlock()
				if nowKeyed {
					return nil
				}
				return s.writeSnapshotReads(ctx, client, true, pollBytes)
			})
			if err != nil && ctx.Err() == nil {
				s.logger.WarnWith().Str("error", errMessage(err)).
					Msg("bridge: CI-V state-mirror poll write failed")
			}
		}
	}
}

// runSupervisor wraps runPipeline in a retry loop so the bridge
// self-heals across the two operator-visible startup orderings (PC
// up first then rig, or rig down then up) AND across mid-session
// disruptions (power spike, cable reseat). Per the discussion behind
// this change: auto-recovery is load-bearing — a live SPA mid-QSO
// cannot require a daemon restart to reconnect to the rig.
//
// Backoff: starts at supervisorInitialBackoff (1s), doubles per
// failed attempt, caps at supervisorMaxBackoff (30s — same window as
// livenessTimeout). Resets to the initial value if the previous
// pipeline run survived past supervisorSteadyStateThreshold (10s),
// so a flaky session retries fast after a brief disconnect rather
// than waiting out the last cap.
//
// Toast dedup: the publishExit* helpers track the last published
// error key on Service. A retry that fails for the same reason
// publishes nothing; a retry that fails for a different reason
// (e.g. open succeeded then INIT-write failed) publishes once. The
// supervisor clears the key after a steady-state pipeline run.
func (s *Service) runSupervisor(ctx context.Context) {
	defer s.wg.Done()

	backoff := s.supervisorInitialBackoff

	for {
		startTime := time.Now()
		exit := s.runPipeline(ctx)

		switch exit {
		case exitContextCancelled, exitPermanent:
			return
		case exitTransient:
			// Reset backoff (and dedup) if the previous pipeline ran
			// long enough to count as a successful session interrupted
			// by a fault — that operator should see the new failure
			// cleanly, not have it suppressed against an old key from
			// minutes ago.
			if time.Since(startTime) > s.supervisorSteadyStateThreshold {
				backoff = s.supervisorInitialBackoff
				s.clearLastPublishedExitKey()
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > s.supervisorMaxBackoff {
				backoff = s.supervisorMaxBackoff
			}
		default:
			// Unreachable today — every runPipeline return goes
			// through one of the three named classes. Guard against
			// a future fourth class being added without a matching
			// supervisor branch (silent fall-through would spin the
			// loop with no backoff). Log and bail rather than retry
			// with unknown policy.
			s.logger.ErrorWith().
				Int("class", int(exit)).
				Msg("bridge: supervisor saw unexpected pipelineExitClass; exiting to avoid undefined retry behaviour")
			return
		}
	}
}

// readLoop is the steady-state read+decode+publish loop. Split from
// runPipeline so tests can drive it directly with a pre-opened fake
// client without going through the open/init dance. Accepts the
// pre-encoded INIT and READ bytes so the no-data probe can re-issue
// them without re-encoding.
func (s *Service) readLoop(ctx context.Context, client serial.Client, def cat.RigDefinition, initBytes, readBytes []byte) pipelineExitClass {
	announcedDisconnect := false
	identityVerified := false
	// unrecognisedPublished rate-limits the identity-unrecognised bridge-error
	// to once per pipeline instance now that an unrecognised ID no longer
	// latches identityVerified (finding 7) — without it every subsequent frame
	// would re-publish the same error.
	unrecognisedPublished := false
	// identityReprobeAt paces readLoop's active ID re-query (see
	// identityReprobeInterval). Seeded to now so the connect-time READ that
	// runPipeline just sent gets one interval to be answered before we re-ask.
	identityReprobeAt := time.Now()
	// Keep one absolute liveness deadline across ignored frames. In particular,
	// CI-V serial links may echo the controller's own writes; starting a fresh
	// timeout after every echo would let the poll loop keep a powered-off rig
	// "alive" forever. Only a frame proven to come from the configured rig moves
	// this deadline.
	livenessDeadline := time.Now().Add(s.livenessTimeout)
	for {
		readCtx, cancel := context.WithDeadline(ctx, livenessDeadline)
		line, err := client.ReadResponseBytes(readCtx)
		cancel()

		if err != nil {
			// Parent ctx cancel — deliberate shutdown.
			if ctx.Err() != nil {
				return exitContextCancelled
			}
			// Read deadline expired (no data within livenessTimeout).
			// Emit rig-disconnected once and probe the rig — re-send
			// INIT (re-arms AUTO mode in case the rig was off when
			// the original INIT went out and has since been powered
			// on) followed by READ (forces an immediate state-snapshot
			// response from an alive rig). Without the probe, an
			// operator who boots the daemon before the rig then
			// switches the rig on later would sit in permanent silence
			// — the rig never received the original INIT, so AUTO
			// mode is never armed, and the rig pushes nothing. The
			// probe makes "switch the rig on" the only operator
			// action needed for recovery.
			if stderr.Is(err, context.DeadlineExceeded) {
				// Count every consecutive no-data timeout (not just the first
				// announce): RigConnected trips at noDataStrikeLimit so the FT8
				// capture gate releases the mic on a genuine mid-session drop. A
				// quiet-but-alive rig recovers on the probe below (a successful
				// read resets the count), so it never reaches the limit.
				strikes := s.noDataStrikes.Add(1)
				// Liveness dying while a transmission is keyed or unconfirmed
				// is the incident's exact shape: the control path is gone and
				// the PTT may be up — alarm immediately (ADR 0051).
				if strikes >= noDataStrikeLimit {
					s.mu.Lock()
					keyedish := s.tuneActive || s.ft8TxActive || s.txUncertain
					// The authoritative FT8 logging/spot snapshots must not
					// survive a passive disconnect. A recovered rig's READ/POLL
					// replies repopulate them field by field.
					s.clearRigSnapshotLocked()
					s.mu.Unlock()
					if keyedish {
						s.raiseTxAlarm(TxAlarmLivenessLost)
					}
				}
				if !announcedDisconnect {
					s.publishDisconnect(RigCodeNoData, nil)
					announcedDisconnect = true
				}
				// Re-arm + re-probe as ONE sequence: for CI-V the INIT and the
				// READ snapshot are held together under cmdMu so a command/key
				// ACK flow can't slip a frame between them on the half-duplex bus
				// (review 2026-06-19 M2).
				civ := def.Protocol == cat.ProtocolIcomCIV
				if werr := s.underCmdMuCIV(civ, func() error {
					if err := client.WriteCommandBytes(ctx, initBytes); err != nil {
						return err
					}
					return s.writeSnapshotReads(ctx, client, civ, readBytes)
				}); werr != nil {
					if ctx.Err() != nil {
						return exitContextCancelled
					}
					s.publishExitDisconnect(RigCodeSerialError, map[string]string{"error": errMessage(werr)})
					return exitTransient
				}
				// Give the rig a full response window after the multi-frame
				// probe completes; its own controller echoes do not move this.
				livenessDeadline = time.Now().Add(s.livenessTimeout)
				continue
			}
			// Terminal serial error (ErrClosed, EIO, cable yank,
			// power spike). Publish via the supervisor-aware dedup
			// helper, then return exitTransient so the supervisor
			// reopens the port. Recovery is automatic — operator
			// reseats cable / powers rig back on, supervisor's next
			// open attempt succeeds and the session resumes.
			//
			// Log the real cause here: the pipeline returns only an exit
			// classification (not err) to the supervisor, and the SPA renders
			// serial_port_error from the code (ignoring details.error), so this
			// is the one place the underlying reason (now carried out of the
			// reader loop via ReadResponseBytes) reaches smd.log for debugging.
			s.logger.WarnWith().Err(err).Str("driver", def.ID).
				Msg("bridge: rig serial read failed; tearing down for supervisor reopen")
			s.publishExitDisconnect(RigCodeSerialError, map[string]string{"error": errMessage(err)})
			return exitTransient
		}

		// For CI-V, a successful serial read is not necessarily rig traffic: USB
		// adapters can return our controller writes as echoes, and a shared bus can
		// carry other members. Only a structurally valid frame FROM the configured
		// rig proves liveness. The ASCII protocols have no controller-echo frame
		// shape, so every complete response line remains liveness evidence.
		fromRig := def.Protocol != cat.ProtocolIcomCIV || cat.IsCIVRigFrame(def, line)
		if fromRig {
			announcedDisconnect = false
			livenessDeadline = time.Now().Add(s.livenessTimeout)
			// Clear strikes only on genuine rig traffic so RigConnected cannot be
			// held true by controller echoes.
			if s.noDataStrikes.Load() != 0 {
				s.noDataStrikes.Store(0)
			}
		}

		// A frame from the configured rig — decodable or not — proves it is alive
		// and talking. While its identity is still unconfirmed, re-issue READ to
		// re-solicit the ID reply the connect-time READ lost. This runs BEFORE
		// decode on purpose: unparsed AI/telemetry frames (S-meter, etc.) return
		// ErrNoMatch and `continue` below, yet they still reset the liveness
		// deadline — so gating the re-probe on a successful DECODE would let a
		// rig that chatters only unparsed frames strand identity forever (the
		// exact hole the lost connect-time ID opens; readLoop's only other READ
		// re-issue is the no-data probe, which needs full silence). Cadence-
		// bounded; the seed at loop start means CI-V (which confirms on its first
		// decoded frame, well inside one interval) never re-probes here. READ is
		// queries only — no TX; H2 is unaffected (writes stay blocked until the
		// ID actually confirms).
		if fromRig && !identityVerified && time.Since(identityReprobeAt) >= identityReprobeInterval {
			identityReprobeAt = time.Now()
			civ := def.Protocol == cat.ProtocolIcomCIV
			if werr := s.underCmdMuCIV(civ, func() error {
				return s.writeSnapshotReads(ctx, client, civ, readBytes)
			}); werr != nil && ctx.Err() == nil {
				s.logger.DebugWith().Err(werr).Str("driver", def.ID).
					Msg("bridge: identity re-probe READ write failed; will retry")
			}
		}

		// Track unsolicited Transceive broadcasts so the poll loop's collision
		// back-off can defer while the rig is mid dial-turn storm (ADR 0035).
		// Only broadcasts (to 0x00) count — the poll's own replies must not, or
		// it would suppress itself.
		if cat.IsCIVBroadcast(def, line) {
			s.mu.Lock()
			s.lastBroadcastAt = time.Now()
			s.mu.Unlock()
		}

		// CI-V command ACK (ADR 0034): the rig answers a set-command with a bare
		// FB/FA and never broadcasts the change, so route the ACK to the waiting
		// SendCommands instead of decoding it as state (it matches no State, so
		// it would be dropped anyway). Non-ACK frames (broadcasts, polled
		// replies) fall through to the normal decode below.
		if isAck, accepted := cat.CIVAck(def, line); isAck {
			s.deliverAck(accepted)
			continue
		}

		status, decErr := cat.Decode(def, line)
		if decErr != nil {
			// ErrNoMatch is normal — rig pushes lines for tags we
			// don't have a parser for (S-meter, waterfall, etc.).
			// Silent skip per the codec doc; logging would flood.
			continue
		}
		// One successfully decoded frame — the confirm machinery's
		// any-rig-data fallback watermarks against this counter (ADR 0051).
		s.rxFrameCount.Add(1)

		// Identity verification fires once per pipeline lifecycle on
		// the first IDENTITY response (typically arrives in response
		// to the bootstrap READ on the first SSE-open, or to the
		// initial INIT if any rig replies to AI1;). Mismatch is an
		// operator-actionable misconfiguration — `bridge.cat.driver`
		// names a different rig than the one actually wired up.
		//
		// Scope is per-pipeline-instance, NOT per-physical-rig: a
		// hot-swap (park rig 1, plug in rig 2 on the same port
		// without restarting the daemon) won't re-verify against
		// the new identity. Acceptable for v1 — cable yank surfaces
		// as a terminal serial error and the pipeline exits, so a
		// daemon restart is already the recovery path. Per the
		// 2026-05-10 internal-bridge-pipeline review (#9).
		if !identityVerified {
			if def.Protocol == cat.ProtocolIcomCIV {
				// CI-V has no model-ID handshake; the bus address IS the rig's
				// identity. cat.Decode's echo/address filter drops every frame
				// whose `from` isn't the configured civ_address, so reaching here
				// with a successful decode means this frame came from the rig at
				// the configured address by construction (ADR 0034). That is the
				// confirmation — unlock the operator write paths. (A different
				// Icom deliberately set to the same address would pass, but
				// commands route by that address anyway, and TX stays unexposed.)
				identityVerified = true
				// Same safe-order recovery as the Yaesu branch (ADR 0051):
				// the CI-V defensive unkey confirms via its awaited ACK.
				s.beginDefensiveRecovery(def, client)
			} else if v, ok := status["IDENTITY"]; ok {
				switch classifyIdentity(v, def.Model) {
				case identityUnrecognised:
					// Rig answered with an ID code this rigdef doesn't map.
					// Not a definite wrong-rig (could be an unlisted variant),
					// so keep reading state for display + diagnosis — but
					// identity is never confirmed, so the operator write paths
					// stay blocked (H2). Deliberately does NOT latch
					// identityVerified: a garbled first ID (line noise during
					// connect) must not permanently write-block the instance —
					// a later exact-match IDENTITY frame re-classifies and
					// confirms (2026-07-18 TX-safety review, finding 7). The
					// bridge-error publishes once per instance (latch below),
					// not per frame.
					if !unrecognisedPublished {
						unrecognisedPublished = true
						s.publishBridgeError(BridgeErrCodeIdentityUnrecognised, map[string]string{"driver": def.ID})
					}
				case identityMismatch:
					identityVerified = true
					// Definite mismatch: the wired rig is a different model than
					// the configured driver. Halt the pipeline (close the port,
					// no supervisor retry) so commands / tune can never reach the
					// wrong rig (H2, option c). exitPermanent matches its own
					// doc-comment's intent; the operator must fix bridge.cat.driver.
					s.publishExitBridgeError(BridgeErrCodeIdentityMismatch, map[string]string{"driver": def.ID, "expected": def.Model, "actual": v})
					return exitPermanent
				case identityConfirmed:
					// Positively the configured rig. beginDefensiveRecovery
					// unlocks the write paths in the SAFE order (ADR 0051 /
					// 8bd88c1b review): uncertainty first (keys refuse), then
					// identity, then the unconditional defensive unkey with
					// its full confirm-or-alarm cycle.
					identityVerified = true
					s.beginDefensiveRecovery(def, client)
				}
			}
		}

		// ADR 0051 confirmation inputs: a decoded TXSTATUS answers the
		// post-unkey query (or an unsolicited status push); any successful
		// decode is the weaker liveness-fallback for defs without the query.
		if v, ok := status["TXSTATUS"]; ok && v != "" {
			s.observeTxStatus(v)
		}
		// Rig-pushed ALC/PO/SWR, accumulated against the keyed transmission and
		// summarised in one line when it ends (meters.go). Read-only and
		// listen-only: nothing is written to the rig for this.
		s.observeMeter(status)
		// ADR 0064: decoded RM4/RM5 poll answers fan out to the SPA on
		// rig-meters (query answers only — the pushed RM0 stays off it).
		s.publishMeterAnswers(status)
		s.observeRigData()

		payload, hasFields := mapStatusToPayload(status)
		if !hasFields {
			continue
		}
		// Feed the tune controller's current-state snapshot (mode+power)
		// before fanning out, so a later tune can restore them (ADR 0027).
		s.captureTuneSnapshot(payload)
		// Keep the rolling dial snapshot current too — the FT8 QSO sink logs the
		// rig's actual frequency from this, not the SPA's stale start-time value.
		s.captureDialFreq(payload)
		s.hub.publish(Event{Name: EventRigState, Payload: payload})
	}
}

// identityResult classifies a decoded rig IDENTITY against the configured
// driver's expected model. Pure + exported-to-tests so the mismatch decision
// (which can't be reproduced through the shipped rigdefs — neither maps an ID
// code to a model other than its own) is exhaustively unit-testable.
type identityResult int

const (
	// identityConfirmed — the decoded ID matches the configured model.
	identityConfirmed identityResult = iota
	// identityUnrecognised — the rig answered with an ID code this rigdef
	// doesn't map (decodes to ""): could be an unlisted variant, so not a
	// definite wrong-rig.
	identityUnrecognised
	// identityMismatch — the rig identified as a different, known model.
	identityMismatch
)

// classifyIdentity decides the verification outcome for a decoded IDENTITY
// value against the configured driver's model (H2, review 2026-06-04).
func classifyIdentity(decoded, expectedModel string) identityResult {
	switch {
	case decoded == "":
		return identityUnrecognised
	case decoded != expectedModel:
		return identityMismatch
	default:
		return identityConfirmed
	}
}

// setIdentityConfirmed records the outcome of rig identity verification.
// See Service.identityConfirmed for what it gates.
func (s *Service) setIdentityConfirmed(v bool) {
	s.mu.Lock()
	s.identityConfirmed = v
	s.mu.Unlock()
	if v {
		// A now-confirmed identity retires any cached identity_unrecognised
		// bridge-error (a1d031cf review): finding 7's un-latch means a garbled
		// first ID can recover, and a new tab must not be toasted a stale
		// warning about it after the recovery.
		s.hub.clearCachedBridgeError(BridgeErrCodeIdentityUnrecognised)
	}
}

// identityOK reports whether the connected rig has been confirmed as the
// configured driver. The operator write paths (SendCommands / StartTune)
// refuse while this is false (H2, review 2026-06-04).
func (s *Service) identityOK() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identityConfirmed
}

// publishDisconnect emits one rig-disconnected event with the given
// machine-readable code + per-instance substitution details (e.g.
// {"error": "i/o timeout"} for the serial-port-error code). The SPA
// looks the code up in its i18n catalogue and renders the localised
// template with the details. Centralised so the wire shape has only
// one call site.
func (s *Service) publishDisconnect(code RigDisconnectedCode, details map[string]string) {
	s.hub.publish(Event{
		Name:    EventRigDisconnected,
		Payload: RigDisconnectedPayload{Code: code, Details: details},
	})
}

// publishBridgeError emits one bridge-error event for an
// operator-actionable failure (port permission denied, unknown
// driver, rigdef missing INIT/READ, identity mismatch, etc.). The
// SPA's i18n catalogue keys off code + substitutes details into the
// localised template. NOT used for transient retries or per-frame
// protocol hiccups — those stay logged-only so the toast stream
// doesn't flood.
func (s *Service) publishBridgeError(code BridgeErrorCode, details map[string]string) {
	s.hub.publish(Event{
		Name:    EventBridgeError,
		Payload: BridgeErrorPayload{Code: code, Details: details},
	})
}

// publishExitBridgeError is publishBridgeError with supervisor-aware
// dedup. Used at exit-causing sites in runPipeline so the supervisor's
// retry loop doesn't flood the SPA with identical toasts while it
// repeatedly tries to reopen a missing port or re-INIT a powered-off
// rig. Mid-loop publishes (identity mismatch — the rig answered but
// with the wrong ID) keep using publishBridgeError directly because
// they're a one-shot per pipeline run, not a retry-driven repeat.
func (s *Service) publishExitBridgeError(code BridgeErrorCode, details map[string]string) {
	key := "bridge-error:" + string(code)
	s.mu.Lock()
	if s.lastPublishedExitKey == key {
		s.mu.Unlock()
		return
	}
	s.lastPublishedExitKey = key
	s.mu.Unlock()
	s.publishBridgeError(code, details)
}

// publishExitDisconnect is publishDisconnect with supervisor-aware
// dedup. Used by readLoop's terminal-serial-error site. The 30s
// no-data publishDisconnect call inside readLoop is separately
// deduped by its own announcedDisconnect flag (one per pipeline
// instance, not across supervisor retries) — that's the correct
// scope there: a single quiet-rig event per session, with implicit
// recovery on the next decoded line.
func (s *Service) publishExitDisconnect(code RigDisconnectedCode, details map[string]string) {
	key := "rig-disconnected:" + string(code)
	s.mu.Lock()
	if s.lastPublishedExitKey == key {
		s.mu.Unlock()
		return
	}
	s.lastPublishedExitKey = key
	s.mu.Unlock()
	s.publishDisconnect(code, details)
}

// clearLastPublishedExitKey resets the supervisor's dedup state.
// Called from runSupervisor when a pipeline run survives past
// supervisorSteadyStateThreshold so the operator sees the NEXT
// failure cleanly rather than having it suppressed against a key
// set in a previous failure cycle minutes ago.
func (s *Service) clearLastPublishedExitKey() {
	s.mu.Lock()
	s.lastPublishedExitKey = ""
	s.mu.Unlock()
}

// mapStatusToPayload filters a cat.Decode tag map down to the
// SPA-relevant subset (per ADR 0019: vfoA, vfoB, mode, subMode,
// selectedVfo, splitOverride, power, plus rigIdentity from the
// startup ID push). Returns the populated payload and a flag that
// is true iff at least one field was set — empty payloads are
// suppressed at the publish call site so we don't fan out heartbeat
// no-ops.
//
// Field names map per the rigdef tag conventions in
// internal/cat/rigs/*.json:
//
//   - IDENTITY  → RigIdentity   (value-mapped to model name, e.g. "FT-710")
//   - VFOAFREQ  → VfoA          (9-char zero-padded Hz)
//   - VFOBFREQ  → VfoB          (9-char zero-padded Hz)
//   - MAINMODE  → Mode          (mapped: "1"→"LSB", "2"→"USB", ...)
//   - SUBMODE   → SubMode       (same mapping table as MAINMODE)
//   - SELECT    → SelectedVfo   ("VFO-A"/"VFO-B" → "A"/"B")
//   - SPLIT     → SplitOverride ("ON"→true, "OFF"→false)
//   - TXPWR     → Power         (3-digit watts)
//
// Tags not in the map are dropped — that's how the "drop waterfall /
// S-meter" filter from M3a.2's scope is enforced. A future rigdef
// that emits tags we don't recognise gets them dropped, no SPA wire
// change required.
func mapStatusToPayload(status cat.Status) (RigStatePayload, bool) {
	var p RigStatePayload
	var populated bool

	if v, ok := status["IDENTITY"]; ok && v != "" {
		p.RigIdentity = v
		populated = true
	}
	if v, ok := status["VFOAFREQ"]; ok && v != "" {
		if hz, err := parseFreqHz(v); err == nil {
			p.VfoA = hz
			populated = true
		}
	}
	if v, ok := status["VFOBFREQ"]; ok && v != "" {
		if hz, err := parseFreqHz(v); err == nil {
			p.VfoB = hz
			populated = true
		}
	}
	if v, ok := status["MAINMODE"]; ok && v != "" {
		p.Mode = v
		populated = true
	}
	if v, ok := status["SUBMODE"]; ok && v != "" {
		p.SubMode = v
		populated = true
	}
	if v, ok := status["SELECT"]; ok && v != "" {
		p.SelectedVfo = vfoLabelToTag(v)
		populated = true
	}
	if v, ok := status["SPLIT"]; ok && v != "" {
		// *bool rather than bool: the rig pushing OFF must not be
		// indistinguishable on the wire from "not pushed this frame"
		// — see RigStatePayload doc-comment.
		//
		// Any decoded non-OFF state means split is engaged. The FTdx10
		// rigdef maps ST 0/1/2 → OFF/ON/ON+ ("ON+" is quick-split), and
		// EqualFold(v,"ON") would misread ON+ as not-split → the SPA shows
		// split off and the wrong TX/RX freq gets logged. Decode-OFF is the
		// only "not split" state (review 2026-06-04 H1).
		split := !strings.EqualFold(v, "OFF")
		p.SplitOverride = &split
		populated = true
	}
	if v, ok := status["TXPWR"]; ok && v != "" {
		if w, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			p.Power = w
			populated = true
		}
	}
	if v, ok := status[meterSelTag]; ok && v != "" {
		// Not the meter READING (those are filtered out here by design — the
		// "drop waterfall / S-meter" rule above); this is which meter is
		// SELECTED, which decides whether drive-collapse detection can run at
		// all. populated is set so an MS frame arriving ALONE still publishes:
		// turning the rig's meter knob sends no other tag, and without this the
		// operator's banner would stay stale until something unrelated changed.
		p.DriveMonitor = driveMonitorFor(v)
		populated = true
	}
	return p, populated
}

// parseFreqHz converts the rigdef's VFO frequency string (typically
// 9 zero-padded ASCII digits, e.g. "014250000") into an int64 Hz
// value. Tolerates surrounding whitespace and shorter unpadded
// values; rejects non-numeric input by returning the strconv error.
func parseFreqHz(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

// vfoLabelToTag collapses the rigdef-mapped VFO selector value
// ("VFO-A" / "VFO-B") to the single-character tag the SPA's
// catState uses ("A" / "B"). Other values pass through unchanged
// so a future rig that uses a different convention surfaces in
// logs rather than getting silently dropped.
func vfoLabelToTag(label string) string {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "VFO-A":
		return "A"
	case "VFO-B":
		return "B"
	default:
		return label
	}
}

// buildSerialConfig assembles a serial.Config from the operator's
// device-node choice (port) and the rigdef's protocol-determined
// settings (baud rate, data bits, parity, stop bits, line delimiter,
// read timeout). The translation from rigdef-JSON-friendly forms
// (parity as a string, line delimiter as a single-character string,
// stop bits as an int) to serial.Config's typed fields lives here
// rather than in cat or serial — cat is pure codec, serial is pure
// I/O, and the JSON↔enum translation is the bridge's glue work.
//
// RTS/DTR ARE now carried (the trigger the M3 note anticipated: ADR 0034's
// Icom CI-V needs rts:false/dtr:false because USB SEND can map PTT to a control
// line, so opening with it asserted keys the rig). They pass through as the
// rigdef's *bool tri-state — nil leaves go.bug.st's default (both asserted, the
// historical behaviour the Yaesu USB-CDC rigs rely on), an explicit false
// de-asserts at open via serial.Mode.InitialStatusBits. No per-rig override for
// these (not needed); the rigdef is authoritative.
//
// Still deliberately dropped (review 2026-06-04 M3): RigSerial's WriteTimeoutMS.
//
// The rigdef's WriteTimeoutMS (e.g. 20ms on the FTdx10) stays dropped on
// purpose: it reads as an expected per-write latency, far too tight to drive
// the H4 write watchdog (which CLOSES the port on overrun — a 20ms threshold
// would close on any scheduling hiccup). The watchdog uses the separate,
// generous bridge.timeouts.write_watchdog_ms instead, applied in runPipeline.
func buildSerialConfig(brCfg types.BridgeSerialConfig, rigSerial cat.RigSerial) (serial.Config, error) {
	// Per-rig serial overrides (config.md §10, B2): a non-zero/non-empty override
	// field wins over the rigdef default; a zero field inherits. brCfg.Overrides is
	// the active rig's RigConfig.Overrides, projected by Config.ActiveBridge().
	// The effective values are then handed to the shared rigserial composer, which
	// parses (parity / stop-bits / 0xNN delimiter) and validates (baud / data
	// bits) — one source of truth shared with the diagnostic CLIs so they can't
	// disagree on RTS/DTR carry-through or delimiter parsing (review 2026-06-19
	// H2). A bad numeric field is a config error here, surfaced as exitPermanent
	// by the caller, not a transient open failure (review 2026-06-19 M2).
	ov := brCfg.Overrides
	eff := rigSerial
	if ov.BaudRate != 0 {
		eff.BaudRate = ov.BaudRate
	}
	if ov.DataBits != 0 {
		eff.DataBits = ov.DataBits
	}
	if ov.StopBits != 0 {
		eff.StopBits = ov.StopBits
	}
	if ov.Parity != "" {
		eff.Parity = ov.Parity
	}
	if ov.LineDelimiter != "" {
		eff.LineDelimiter = ov.LineDelimiter
	}
	if ov.ReadTimeoutMS != 0 {
		eff.ReadTimeoutMS = ov.ReadTimeoutMS
	}
	// RTS/DTR are tri-state, so "set" is nil-ness rather than non-zero: an
	// override of FALSE is meaningful and must not be read as "unset". These are
	// a PTT source on rigs that map one to a control line, so the override
	// exists mainly to let an operator de-assert a line a future rigdef asserts
	// — the reverse of the 2026-07-23 case, where there was no override at all.
	if ov.RTS != nil {
		eff.RTS = ov.RTS
	}
	if ov.DTR != nil {
		eff.DTR = ov.DTR
	}
	return rigserial.Compose(eff, brCfg.Port)
}

// civFrameDelimiter is the CI-V frame terminator (0xFD). The icom_civ codec
// emits whole FE FE…FD frames, so a READ snapshot's frames are split here on
// this byte. Fixed for the protocol (the rigdef declares it as the serial
// line_delimiter too, but the snapshot splitter doesn't need the serial config
// in scope).
const civFrameDelimiter = 0xFD

// underCmdMuCIV runs fn while holding cmdMu when civ is true, or directly
// otherwise. cmdMu is the CI-V "one command sequence on the half-duplex bus"
// guard (ADR 0034); every CI-V snapshot/keying writer must take it so a
// bootstrap, startup, poll, or liveness-recovery READ burst can't interleave
// its frames with an in-flight SendCommands batch or key/unkey ACK sequence
// (review 2026-06-19 M2). Kenwood is full-duplex confirm-by-push with no
// one-outstanding-command contract, so it writes directly. Callers must hold
// neither mu nor keyMu (lock order keyMu → cmdMu → mu); writeSnapshotReads
// itself takes no lock, so the only ordering edge is cmdMu before any mu fn
// might take.
func (s *Service) underCmdMuCIV(civ bool, fn func() error) error {
	if civ {
		s.cmdMu.Lock()
		defer s.cmdMu.Unlock()
	}
	return fn()
}

// writeSnapshotReads writes a READ state snapshot to the rig. For Kenwood it is
// a single write — the rig queues the ;-delimited burst fine. For CI-V it must
// NOT be: the link is half-duplex and a second read arriving while the rig is
// turning around its reply to the first makes the rig abandon that reply and
// answer only the last read, so a back-to-back read-freq + read-mode burst
// loses the freq reply (bench 2026-06-15 — a fresh SPA tab showed stale freq,
// current mode). So the CI-V frames are split on the frame delimiter and written
// one at a time with civReadGap between them, letting each reply complete.
//
// The gap is inserted only BETWEEN frames (not before the first), so a
// single-frame snapshot has no added latency. ctx cancellation is honoured
// during the gap so shutdown isn't delayed.
func (s *Service) writeSnapshotReads(ctx context.Context, client serial.Client, civ bool, readBytes []byte) error {
	if !civ {
		return client.WriteCommandBytes(ctx, readBytes)
	}
	frames := splitCIVFrames(readBytes)
	for i, f := range frames {
		if i > 0 && s.civReadGap > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.civReadGap):
			}
		}
		if err := client.WriteCommandBytes(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// splitCIVFrames splits a concatenated CI-V byte sequence into its individual
// frame bodies on the frame delimiter (0xFD), dropping the empty tail after the
// final delimiter. The trailing delimiter is stripped from each body;
// WriteCommandBytes re-appends it on write. A buffer with no delimiter is
// returned as a single frame unchanged.
func splitCIVFrames(b []byte) [][]byte {
	var frames [][]byte
	for _, f := range bytes.Split(b, []byte{civFrameDelimiter}) {
		if len(f) == 0 {
			continue
		}
		frames = append(frames, f)
	}
	return frames
}

// Serial parity / stop-bits / delimiter parsing now lives in the shared
// internal/rigserial composer (used by buildSerialConfig above + the diagnostic
// CLIs), so the bridge and catcli can't drift on RTS/DTR or 0xNN delimiters
// (review 2026-06-19 H2).

// errMessage flattens an error to its top-level message. Used in
// rig-disconnected reason strings where the operator-facing toast
// only wants "no such file or directory" rather than the full
// nested "serial.Open: no such file or directory" chain.
func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
