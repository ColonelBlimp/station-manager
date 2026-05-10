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

// initCommandName is the rigdef command-table entry that puts the
// rig into AUTO-mode push state and asks for its identity. Both
// shipping rigdefs (yaesu-ft710, yaesu-ftdx10) define it; future
// rigdefs that lack it will surface a loud bridge-error at startup
// rather than running mute (see runPipeline).
const initCommandName = "INIT"

// runPipeline replaces M3a.1's runStubEmitter with the real
// serial+CAT pipeline. Opens the serial port via s.openClient,
// looks up the rig def via cat.Lookup, sends the rigdef's INIT
// command, then loops decoding push lines and publishing typed
// events to the hub.
//
// Termination semantics:
//
//   - Parent ctx cancelled (Service.Stop / daemon shutdown): exit
//     silently. SSE subscribers see the hub close cleanly.
//   - 30s without a line from the rig: publish rig-disconnected
//     once, keep waiting. If data resumes, subsequent rig-state
//     events implicitly tell the SPA the rig is alive again
//     (bridge.svelte.ts flips rigResponding=true on any rig-state
//     arrival per ADR 0009).
//   - Terminal serial error (port closed/EIO/permission revoked):
//     publish rig-disconnected with the error reason, exit. SSE
//     subscribers stay connected; the next operator action (rig
//     power-cycle + daemon restart, or — once M3a.3 lands —
//     bridge-error event surfacing) closes the loop.
//   - Initial open or rig-def lookup failure: log loudly and exit.
//     M3a.3 promotes these to bridge-error events; for M3a.2 they're
//     logged-and-bail (the daemon stays up; the bridge just doesn't
//     stream).
func (s *Service) runPipeline(ctx context.Context) {
	defer s.wg.Done()

	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		s.logger.ErrorWith().
			Str("driver", s.cfg.Cat.Driver).
			Msg("bridge: unknown CAT driver; pipeline not started")
		return
	}

	serialCfg, err := buildSerialConfig(s.cfg.Serial, def.Serial)
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("bridge: serial config build failed; pipeline not started")
		return
	}

	client, err := s.openClient(serialCfg)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("port", serialCfg.PortName).
			Int("baud", serialCfg.BaudRate).
			Msg("bridge: serial open failed; pipeline not started")
		return
	}
	defer func() { _ = client.Close() }()

	initBytes, err := cat.Encode(def, initCommandName)
	if err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("driver", def.ID).
			Str("command", initCommandName).
			Msg("bridge: rigdef has no INIT command; pipeline not started")
		return
	}
	if err := client.WriteCommandBytes(ctx, initBytes); err != nil {
		s.logger.ErrorWith().
			Err(err).
			Str("driver", def.ID).
			Msg("bridge: failed to send INIT; pipeline not started")
		return
	}

	s.logger.InfoWith().
		Str("port", serialCfg.PortName).
		Int("baud", serialCfg.BaudRate).
		Str("driver", def.ID).
		Msg("bridge: pipeline started; AUTO-mode CAT data flow active")

	s.readLoop(ctx, client, def)
}

// readLoop is the steady-state read+decode+publish loop. Split from
// runPipeline so tests can drive it directly with a pre-opened fake
// client without going through the open/init dance.
func (s *Service) readLoop(ctx context.Context, client serial.Client, def cat.RigDefinition) {
	announcedDisconnect := false
	for {
		readCtx, cancel := context.WithTimeout(ctx, livenessTimeout)
		line, err := client.ReadResponseBytes(readCtx)
		cancel()

		if err != nil {
			// Parent ctx cancel — deliberate shutdown.
			if ctx.Err() != nil {
				return
			}
			// Read deadline expired (no data within livenessTimeout).
			// Emit rig-disconnected once and keep waiting; the next
			// successful read implicitly clears the disconnected flag.
			if stderr.Is(err, context.DeadlineExceeded) {
				if !announcedDisconnect {
					s.publishDisconnect("no CAT data received within timeout window")
					announcedDisconnect = true
				}
				continue
			}
			// Terminal serial error (ErrClosed, EIO, etc.). Emit and
			// exit; the bridge can't recover from a closed port
			// without re-opening, which is M3a.3-and-beyond territory.
			s.publishDisconnect("serial port error: " + errMessage(err))
			return
		}

		announcedDisconnect = false

		status, decErr := cat.Decode(def, line)
		if decErr != nil {
			// ErrNoMatch is normal — rig pushes lines for tags we
			// don't have a parser for (or stray bytes during link
			// startup). Log at debug-level via the trace helper would
			// flood; silent skip is the correct treatment per the
			// codec doc.
			continue
		}
		payload, hasFields := mapStatusToPayload(status)
		if !hasFields {
			continue
		}
		s.hub.publish(Event{Name: EventRigState, Payload: payload})
	}
}

// publishDisconnect emits one rig-disconnected event with the given
// human-readable reason. Centralised so the wire shape (and the
// payload's Reason field) only has one call site.
func (s *Service) publishDisconnect(reason string) {
	s.hub.publish(Event{
		Name:    EventRigDisconnected,
		Payload: RigDisconnectedPayload{Reason: reason},
	})
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
		p.SplitOverride = strings.EqualFold(v, "ON")
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
// per-rig overrides (port, baud) and the rigdef's protocol-determined
// defaults (data bits, parity, stop bits, line delimiter, read
// timeout). The translation from rigdef-JSON-friendly forms (parity
// as a string, line delimiter as a single-character string, stop bits
// as an int) to serial.Config's typed fields lives here rather than
// in cat or serial — cat is pure codec, serial is pure I/O, and the
// JSON↔enum translation is the bridge's glue work.
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
		BaudRate:      brCfg.Baud,
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
// the rigdef's string form. Empty string defaults to '\r' to match
// serial.newPort's fallback. Multi-byte delimiters aren't supported
// by serial.Port today, so a longer string is a config error.
func delimiterFromString(s string) (byte, error) {
	if s == "" {
		return 0, nil // serial.newPort defaults zero to '\r'
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
