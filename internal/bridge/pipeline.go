package bridge

import (
	"context"
	stderr "errors"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"

	bugst "go.bug.st/serial"
)

// livenessTimeout is the data-flow silence window after which the
// bridge concludes the rig is unresponsive and emits a single
// rig-disconnected event (per ADR 0010 passive liveness, ADR 0019
// "passive 30s data-flow timeout only"). Package-level var so tests
// can dial it down without waiting half a minute.
var livenessTimeout = 30 * time.Second

// Supervisor backoff bounds. Package vars so tests can dial them
// down to milliseconds without sleeping for real seconds. Defaults
// match the operator's mental model: a first retry within "I just
// glanced at the screen" time, capping at the same 30s window the
// liveness timeout uses (so the worst-case retry cadence is the
// same as the worst-case no-data detection cadence).
var (
	supervisorInitialBackoff       = 1 * time.Second
	supervisorMaxBackoff           = 30 * time.Second
	supervisorSteadyStateThreshold = 10 * time.Second
)

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

	serialCfg, err := buildSerialConfig(s.cfg.Serial, def.Serial)
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("bridge: serial config build failed; pipeline not started")
		s.publishExitBridgeError(BridgeErrCodeSerialConfigInvalid, map[string]string{"error": errMessage(err)})
		return exitPermanent
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
		s.mu.Unlock()
		_ = client.Close()
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

	// Stash the live client + pre-encoded bootstrap bytes so the SSE
	// handler can fire READ on each new Subscribe via TriggerBootstrap.
	// The defer above clears them on pipeline exit.
	s.mu.Lock()
	s.activeClient = client
	s.bootstrapBytes = readBytes
	s.mu.Unlock()

	s.logger.InfoWith().
		Str("port", serialCfg.PortName).
		Int("baud", serialCfg.BaudRate).
		Str("driver", def.ID).
		Msg("bridge: pipeline started; AUTO-mode CAT data flow active")

	return s.readLoop(ctx, client, def)
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

	backoff := supervisorInitialBackoff

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
			if time.Since(startTime) > supervisorSteadyStateThreshold {
				backoff = supervisorInitialBackoff
				s.clearLastPublishedExitKey()
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > supervisorMaxBackoff {
				backoff = supervisorMaxBackoff
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
// client without going through the open/init dance.
func (s *Service) readLoop(ctx context.Context, client serial.Client, def cat.RigDefinition) pipelineExitClass {
	announcedDisconnect := false
	identityVerified := false
	for {
		readCtx, cancel := context.WithTimeout(ctx, livenessTimeout)
		line, err := client.ReadResponseBytes(readCtx)
		cancel()

		if err != nil {
			// Parent ctx cancel — deliberate shutdown.
			if ctx.Err() != nil {
				return exitContextCancelled
			}
			// Read deadline expired (no data within livenessTimeout).
			// Emit rig-disconnected once and keep waiting; the next
			// successful read implicitly clears the disconnected flag.
			if stderr.Is(err, context.DeadlineExceeded) {
				if !announcedDisconnect {
					s.publishDisconnect(RigCodeNoData, nil)
					announcedDisconnect = true
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
			if v, ok := status["IDENTITY"]; ok {
				identityVerified = true
				switch {
				case v == "":
					s.publishBridgeError(BridgeErrCodeIdentityUnrecognised, map[string]string{"driver": def.ID})
				case v != def.Model:
					s.publishBridgeError(BridgeErrCodeIdentityMismatch, map[string]string{"driver": def.ID, "expected": def.Model, "actual": v})
				}
			}
		}

		payload, hasFields := mapStatusToPayload(status)
		if !hasFields {
			continue
		}
		s.hub.publish(Event{Name: EventRigState, Payload: payload})
	}
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
		split := strings.EqualFold(v, "ON")
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
func buildSerialConfig(brCfg types.BridgeSerialConfig, rigSerial cat.RigSerial) (serial.Config, error) {
	parity, err := parityFromString(rigSerial.Parity)
	if err != nil {
		return serial.Config{}, err
	}
	stopBits, err := stopBitsFromInt(rigSerial.StopBits)
	if err != nil {
		return serial.Config{}, err
	}
	delim, err := delimiterFromString(rigSerial.LineDelimiter)
	if err != nil {
		return serial.Config{}, err
	}
	return serial.Config{
		PortName:      brCfg.Port,
		BaudRate:      rigSerial.BaudRate,
		DataBits:      rigSerial.DataBits,
		Parity:        parity,
		StopBits:      stopBits,
		LineDelimiter: delim,
		ReadTimeoutMS: rigSerial.ReadTimeoutMS,
	}, nil
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
	if len(s) != 1 {
		return 0, stderr.New("bridge: line_delimiter must be a single byte, got " + strconv.Quote(s))
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
