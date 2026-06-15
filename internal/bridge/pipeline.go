package bridge

import (
	"bytes"
	"context"
	"encoding/hex"
	stderr "errors"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"

	bugst "go.bug.st/serial"
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
//   - 30s without a line from the rig: publish rig-disconnected
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

	client, err := s.openClient(serialCfg)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("port", serialCfg.PortName).
			Int("baud", serialCfg.BaudRate).
			Msg("bridge: serial open failed; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeSerialOpenFailed, map[string]string{"port": serialCfg.PortName, "error": errMessage(err)})
		// Port file may appear later (FTdx10 / FT-710 with built-in
		// USB CAT exposes /dev/ttyUSBn only when powered on) — let
		// the supervisor retry.
		return exitTransient
	}
	defer func() {
		// Clear activeClient under mu BEFORE closing the port. This
		// makes the invariant "if a TriggerBootstrap caller captured
		// a non-nil cl, the underlying port is still open"
		// enforceable by ordering rather than incidental. A late
		// caller that races us takes the mu, sees nil, returns the
		// no-op branch — no chance of writing to a closed port.
		// Reordered per internal-bridge-pipeline.md review #3.
		s.mu.Lock()
		s.activeClient = nil
		s.bootstrapBytes = nil
		s.bootstrapCIV = false
		// Identity must be re-verified on the next pipeline instance — a
		// hot-swapped rig (or a reconnect after the wrong rig was fixed)
		// must not inherit the previous run's confirmation (H2).
		s.identityConfirmed = false
		s.mu.Unlock()
		_ = client.Close()
		// Release any active tune — the carrier physically dropped with the
		// rig; clear state, cancel the backstop, forget the stale snapshot,
		// and tell the SPA (ADR 0027).
		s.clearTuneOnDisconnect()
		// Release any active FT8 TX the same way — PTT dropped with the rig
		// (ADR 0030).
		s.clearFt8TxOnDisconnect()
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
	if err := s.writeSnapshotReads(ctx, client, civSnapshot, readBytes); err != nil {
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
	s.mu.Unlock()

	s.logger.InfoWith().
		Str("port", serialCfg.PortName).
		Int("baud", serialCfg.BaudRate).
		Str("driver", def.ID).
		Msg("bridge: pipeline started; AUTO-mode CAT data flow active")

	return s.readLoop(ctx, client, def, initBytes, readBytes)
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
	for {
		readCtx, cancel := context.WithTimeout(ctx, s.livenessTimeout)
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
				if !announcedDisconnect {
					s.publishDisconnect(RigCodeNoData, nil)
					announcedDisconnect = true
				}
				if werr := client.WriteCommandBytes(ctx, initBytes); werr != nil {
					if ctx.Err() != nil {
						return exitContextCancelled
					}
					s.publishExitDisconnect(RigCodeSerialError, map[string]string{"error": errMessage(werr)})
					return exitTransient
				}
				if werr := s.writeSnapshotReads(ctx, client, def.Protocol == cat.ProtocolIcomCIV, readBytes); werr != nil {
					if ctx.Err() != nil {
						return exitContextCancelled
					}
					s.publishExitDisconnect(RigCodeSerialError, map[string]string{"error": errMessage(werr)})
					return exitTransient
				}
				continue
			}
			// Terminal serial error (ErrClosed, EIO, cable yank,
			// power spike). Publish via the supervisor-aware dedup
			// helper, then return exitTransient so the supervisor
			// reopens the port. Recovery is automatic — operator
			// reseats cable / powers rig back on, supervisor's next
			// open attempt succeeds and the session resumes.
			s.publishExitDisconnect(RigCodeSerialError, map[string]string{"error": errMessage(err)})
			return exitTransient
		}

		announcedDisconnect = false

		status, decErr := cat.Decode(def, line)
		if decErr != nil {
			// ErrNoMatch is normal — rig pushes lines for tags we
			// don't have a parser for (S-meter, waterfall, etc.).
			// Silent skip per the codec doc; logging would flood.
			continue
		}

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
				s.setIdentityConfirmed(true)
			} else if v, ok := status["IDENTITY"]; ok {
				identityVerified = true
				switch classifyIdentity(v, def.Model) {
				case identityUnrecognised:
					// Rig answered with an ID code this rigdef doesn't map.
					// Not a definite wrong-rig (could be an unlisted variant),
					// so keep reading state for display + diagnosis — but
					// identity is never confirmed, so the operator write paths
					// stay blocked (H2).
					s.publishBridgeError(BridgeErrCodeIdentityUnrecognised, map[string]string{"driver": def.ID})
				case identityMismatch:
					// Definite mismatch: the wired rig is a different model than
					// the configured driver. Halt the pipeline (close the port,
					// no supervisor retry) so commands / tune can never reach the
					// wrong rig (H2, option c). exitPermanent matches its own
					// doc-comment's intent; the operator must fix bridge.cat.driver.
					s.publishExitBridgeError(BridgeErrCodeIdentityMismatch, map[string]string{"driver": def.ID, "expected": def.Model, "actual": v})
					return exitPermanent
				case identityConfirmed:
					// Positively the configured rig — unlock the operator write
					// paths (SendCommands / StartTune).
					s.setIdentityConfirmed(true)
				}
			}
		}

		payload, hasFields := mapStatusToPayload(status)
		if !hasFields {
			continue
		}
		// Feed the tune controller's current-state snapshot (mode+power)
		// before fanning out, so a later tune can restore them (ADR 0027).
		s.captureTuneSnapshot(payload)
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
	ov := brCfg.Overrides
	baudRate := rigSerial.BaudRate
	if ov.BaudRate != 0 {
		baudRate = ov.BaudRate
	}
	dataBits := rigSerial.DataBits
	if ov.DataBits != 0 {
		dataBits = ov.DataBits
	}
	stopBitsN := rigSerial.StopBits
	if ov.StopBits != 0 {
		stopBitsN = ov.StopBits
	}
	parityStr := rigSerial.Parity
	if ov.Parity != "" {
		parityStr = ov.Parity
	}
	delimStr := rigSerial.LineDelimiter
	if ov.LineDelimiter != "" {
		delimStr = ov.LineDelimiter
	}
	readTimeoutMS := rigSerial.ReadTimeoutMS
	if ov.ReadTimeoutMS != 0 {
		readTimeoutMS = ov.ReadTimeoutMS
	}

	parity, err := parityFromString(parityStr)
	if err != nil {
		return serial.Config{}, err
	}
	stopBits, err := stopBitsFromInt(stopBitsN)
	if err != nil {
		return serial.Config{}, err
	}
	delim, err := delimiterFromString(delimStr)
	if err != nil {
		return serial.Config{}, err
	}
	return serial.Config{
		PortName:      brCfg.Port,
		BaudRate:      baudRate,
		DataBits:      dataBits,
		Parity:        parity,
		StopBits:      stopBits,
		LineDelimiter: delim,
		ReadTimeoutMS: readTimeoutMS,
		RTS:           rigSerial.RTS,
		DTR:           rigSerial.DTR,
	}, nil
}

// civFrameDelimiter is the CI-V frame terminator (0xFD). The icom_civ codec
// emits whole FE FE…FD frames, so a READ snapshot's frames are split here on
// this byte. Fixed for the protocol (the rigdef declares it as the serial
// line_delimiter too, but the snapshot splitter doesn't need the serial config
// in scope).
const civFrameDelimiter = 0xFD

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

// parityFromString maps the rigdef's parity string to the
// go.bug.st/serial enum. Empty string defaults to NoParity (matches
// serial.validateConfig's zero-value treatment); unknown values
// surface as an error so a typo in a future rigdef fails loudly.
func parityFromString(p string) (bugst.Parity, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "none":
		return bugst.NoParity, nil
	case "odd":
		return bugst.OddParity, nil
	case "even":
		return bugst.EvenParity, nil
	case "mark":
		return bugst.MarkParity, nil
	case "space":
		return bugst.SpaceParity, nil
	default:
		return 0, stderr.New("bridge: unknown parity " + strconv.Quote(p))
	}
}

// stopBitsFromInt maps the rigdef's stop_bits integer to the
// go.bug.st/serial enum. Zero defaults to OneStopBit (matches
// serial.validateConfig). FT-710 / FTDX10 rigdefs use 1.
func stopBitsFromInt(n int) (bugst.StopBits, error) {
	switch n {
	case 0, 1:
		return bugst.OneStopBit, nil
	case 2:
		return bugst.TwoStopBits, nil
	default:
		return 0, stderr.New("bridge: unsupported stop_bits " + strconv.Itoa(n))
	}
}

// delimiterFromString extracts the single-byte line delimiter from
// the rigdef's string form. The bridge requires every rigdef to
// declare an explicit delimiter — leaving it empty would lean on a
// serial-package-private fallback (zero byte → '\r'), which is the
// kind of cross-package contract that drifts silently. Per the
// 2026-05-10 internal-bridge-pipeline review (#7), an empty value
// is a rigdef config error and surfaces loudly at startup. Both
// shipping rigdefs declare ';' so this branch is unreachable for
// supported drivers; it guards against a future rigdef that forgets
// the field.
func delimiterFromString(s string) (byte, error) {
	if s == "" {
		return 0, stderr.New("bridge: rigdef serial.line_delimiter is required (no implicit default)")
	}
	// Hex-byte form "0xFD" (case-insensitive) for binary protocols whose frame
	// delimiter isn't a printable character: a raw 0xFD byte can't be a JSON
	// string (not valid UTF-8), and "ý" would unmarshal to the 2-byte UTF-8 of
	// U+00FD, not the single byte. The CI-V rigdef declares "0xFD" (ADR 0034).
	if len(s) == 4 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		b, err := hex.DecodeString(s[2:])
		if err != nil || len(b) != 1 {
			return 0, stderr.New("bridge: line_delimiter hex form must be 0x followed by two hex digits, got " + strconv.Quote(s))
		}
		return b[0], nil
	}
	if len(s) != 1 {
		return 0, stderr.New("bridge: line_delimiter must be a single byte or 0xNN hex form, got " + strconv.Quote(s))
	}
	return s[0], nil
}

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
