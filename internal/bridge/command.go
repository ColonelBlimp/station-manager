package bridge

import (
	"context"
	stderr "errors"
	"fmt"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// ErrRigNotConnected is returned by SendCommand when no rig serial client is
// currently active (bridge disabled, pipeline not yet up, or mid-reconnect).
// Unlike TriggerBootstrap — a best-effort state snapshot that no-ops when the
// rig is absent — an operator command fails loudly so the SPA surfaces that
// it never reached the rig (ADR 0026: no silent no-op).
var ErrRigNotConnected = stderr.New("bridge: no active rig connection")

// ErrRigIdentityUnverified is returned by the operator write paths
// (SendCommands, StartTune) when the connected rig has not positively
// identified as the configured driver. It blocks commands / TX from reaching a
// rig SM can't confirm is the one the operator configured — covering a driver
// typo (wrong rig), an unrecognised ID code, and a rig that never sends a
// parseable ID at all (H2, review 2026-06-04). State display still works; only
// mutating writes are gated. Clears when a matching IDENTITY push arrives, or
// stays for the pipeline's lifetime on a definite mismatch (which also halts).
var ErrRigIdentityUnverified = stderr.New("bridge: rig identity not verified; refusing to send")

// ErrTxActive is returned by SendCommands when a tune carrier or FT8
// transmission is keyed. The generic command path must not write while the rig
// is transmitting: it could retune/re-mode the rig mid-transmission, and
// set_power (Exposed) would override the tune-power clamp and transmit at full
// power. Only the keyed-transmission controllers (tune / FT8 TX) may touch the
// rig while PTT is down-to-up; everything else waits (review 2026-06-16).
var ErrTxActive = stderr.New("bridge: transmission active; refusing command")

// ErrCommandRejected is returned by the CI-V command path (ADR 0034
// wait-for-ACK) when the rig answers a command with FA (NG) — it refused the
// command, e.g. an out-of-band frequency. Distinct from an encoding error: the
// command was well-formed and reached the rig, which declined it.
var ErrCommandRejected = stderr.New("bridge: rig rejected command")

// ErrCommandNoAck is returned by the CI-V command path when the rig sends no
// FB/FA ACK within the wait window (bridge.timeouts.civ_ack_ms). The command may
// or may not have applied; the bridge can't confirm, so it surfaces the
// uncertainty rather than synthesizing a state it didn't see acknowledged.
var ErrCommandNoAck = stderr.New("bridge: no command ACK from rig")

// RigCommand is one (op, value) pair in a SendCommands batch. Op is the rigdef
// command name; Value is its single argument as a string.
type RigCommand struct {
	Op    string
	Value string
}

// SendCommands encodes one or more semantic rig operations and writes them to
// the connected rig as a single CAT line (ADR 0026 inbound command path).
// Batching is the same mechanism READ uses (`ID;FA;FB;…;` in one write): each
// command is encoded independently and the bytes are concatenated, so a "tune
// to band" is one atomic `FA…;MD0…;` write — nothing interleaves between the
// frequency and the mode, and the serial write mutex makes the whole line
// uninterruptible. Confirmation is the AUTO-mode push: the rig volunteers a
// push per command (FA, MD0, …) through the existing readLoop → SSE → catState
// chain, so SendCommands does not wait on a reply.
//
// All-or-nothing: every command is encoded first; if any fails the whole batch
// is rejected and nothing is written. op is the rigdef command name directly
// (no resolver); cat.EncodeCommand applies the Exposed gate, value_map
// inversion, and padding, so an internal (INIT/READ) or TX-capable (PLAYBACK)
// command can never be driven from here.
//
// Errors propagate for the API layer to map: cat.ErrUnknownCommand /
// cat.ErrCommandNotExposed / cat.ErrUnmappedValue from encoding,
// ErrRigNotConnected when no rig is up, or a serial write error. The write
// path stays inside internal/bridge (ADR 0013).
func (s *Service) SendCommands(ctx context.Context, cmds []RigCommand) error {
	const errOp errors.Op = "bridge.Service.SendCommands"

	if len(cmds) == 0 {
		return errors.New(errOp).WithMsg("no commands")
	}
	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		return errors.New(errOp).WithMsgf("no rig definition for driver %q", s.cfg.Cat.Driver)
	}

	// Encode every op up front (all-or-nothing): a bad op rejects the whole
	// batch before anything is written, for both protocols.
	encoded := make([]encodedCommand, len(cmds))
	for i, c := range cmds {
		b, err := cat.EncodeCommand(def, c.Op, c.Value)
		if err != nil {
			return errors.New(errOp).WithErr(err).WithMsgf("encode op %q", c.Op)
		}
		encoded[i] = encodedCommand{op: c.Op, value: c.Value, bytes: b}
	}

	s.mu.Lock()
	cl := s.activeClient
	idOK := s.identityConfirmed
	txBusy := s.tuneActive || s.ft8TxActive
	s.mu.Unlock()
	if cl == nil {
		return errors.New(errOp).WithErr(ErrRigNotConnected).WithMsgf("%d command(s)", len(cmds))
	}
	// Never drive a rig whose identity isn't confirmed as the configured
	// driver — a wrong / unrecognised / never-identified rig must not receive
	// commands (H2). State display is unaffected; only this write path gates.
	if !idOK {
		return errors.New(errOp).WithErr(ErrRigIdentityUnverified).WithMsgf("%d command(s)", len(cmds))
	}
	// Never write to a rig that is transmitting (review 2026-06-16): a tune
	// carrier or FT8 TX owns the rig, and a generic command could retune/re-mode
	// it mid-transmission or (via the Exposed set_power) defeat the tune-power
	// clamp. The keyed-transmission controllers are the only writers while PTT is
	// up. Surfaced as a 409 conflict so the SPA can retry once TX ends.
	if txBusy {
		return errors.New(errOp).WithErr(ErrTxActive).WithMsgf("%d command(s)", len(cmds))
	}

	// CI-V (ADR 0034): the rig confirms each command with a bare FB/FA ACK and
	// never broadcasts the change, so write each frame and wait for its ACK,
	// then synthesize the state push from the commanded value. The Kenwood path
	// stays fire-and-forget — confirmation arrives asynchronously as a rig push.
	if def.Protocol == cat.ProtocolIcomCIV {
		return s.sendCommandsCIV(ctx, def, cl, encoded)
	}

	var line []byte
	for _, c := range encoded {
		line = append(line, c.bytes...)
	}
	return cl.WriteCommandBytes(ctx, line)
}

// encodedCommand pairs a semantic op (+ its commanded value, for state
// synthesis) with its pre-encoded wire bytes. For CI-V an op's bytes may be
// several concatenated FE FE…FD frames (a data-mode set is two).
type encodedCommand struct {
	op    string
	value string
	bytes []byte
}

// sendCommandsCIV is the icom_civ branch of SendCommands: wait-for-ACK (ADR
// 0034). It holds cmdMu for the whole batch (one outstanding command at a time —
// the half-duplex bus can't service overlapping commands, and it keeps a
// "tune to band" freq+mode pair from interleaving with another caller), writes
// each frame, and waits for the rig's FB/FA. On a fully-ACKed op it reflects the
// new state onto the SSE stream (the rig never broadcasts a commanded change):
// an op with a sets_state synthesizes the push from its commanded value; an op
// without one (e.g. swap_vfo — the new operating freq+mode is "whatever was in
// the other VFO", which we can't synthesize) triggers a one-shot READ-back so
// the SPA updates promptly. FA returns ErrCommandRejected; a missing ACK returns
// ErrCommandNoAck. A mid-batch failure leaves earlier ops applied (CI-V can't be
// atomic across frames) — the state already pushed for them reflects reality;
// the error names the op that failed.
func (s *Service) sendCommandsCIV(ctx context.Context, def cat.RigDefinition, cl serial.Client, cmds []encodedCommand) error {
	const errOp errors.Op = "bridge.Service.SendCommands"

	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	for _, c := range cmds {
		if err := s.writeCIVFramesAwaitAck(ctx, cl, c.bytes, fmt.Sprintf("op %q (value %q)", c.op, c.value)); err != nil {
			return err
		}
		// Every frame of this op ACKed (FB). The rig applied it but won't push it,
		// so reflect the new state ourselves (ADR 0034): synthesize from the
		// commanded value when the op names a state it sets, otherwise read it
		// back (a valueless op like swap_vfo changed state we can't synthesize).
		if cat.CommandSetsState(def, c.op) != "" {
			s.publishCommandedState(def, c.op, c.value)
		} else {
			s.readBackAfterCommand(ctx, cl)
		}
	}
	return nil
}

// writeCIVFramesAwaitAck writes each CI-V frame of b and waits for the rig's
// per-frame FB/FA ACK (ADR 0034 wait-for-ACK). The caller MUST hold cmdMu —
// pendingAck has a single installer and cmdMu is what guarantees that. label
// names the operation for error context. Returns ErrCommandRejected on an FA
// (NG), ErrCommandNoAck on a missing ACK within civAckTimeout, ctx.Err() on
// cancel, or a serial write error. Shared by the generic command path
// (sendCommandsCIV) and the TX key/unkey path (writeKeyedLine), so a CI-V rig's
// key/unkey is confirmed by ACK rather than reported on bytes-written alone
// (review 2026-06-16).
func (s *Service) writeCIVFramesAwaitAck(ctx context.Context, cl serial.Client, b []byte, label string) error {
	const errOp errors.Op = "bridge.Service.writeCIVFramesAwaitAck"
	for _, frame := range splitCIVFrames(b) {
		ackCh := make(chan bool, 1)
		s.mu.Lock()
		s.pendingAck = ackCh
		s.mu.Unlock()

		if err := cl.WriteCommandBytes(ctx, frame); err != nil {
			s.clearPendingAck(ackCh)
			return errors.New(errOp).WithErr(err).WithMsgf("write %s", label)
		}

		select {
		case accepted := <-ackCh:
			s.clearPendingAck(ackCh)
			if !accepted {
				return errors.New(errOp).WithErr(ErrCommandRejected).WithMsgf("rig refused %s", label)
			}
		case <-time.After(s.civAckTimeout):
			s.clearPendingAck(ackCh)
			return errors.New(errOp).WithErr(ErrCommandNoAck).
				WithMsgf("%s: no ACK within %s", label, s.civAckTimeout)
		case <-ctx.Done():
			s.clearPendingAck(ackCh)
			return ctx.Err()
		}
	}
	return nil
}

// writeKeyedLine writes a keyed-transmission CAT line (a tune/FT8 key, unkey, or
// mode/power restore) with protocol-appropriate confirmation. For a CI-V rig it
// waits for the rig's per-frame FB/FA ACK so a rejected or dropped key/unkey is
// surfaced as an error rather than reported on bytes-written alone (review
// 2026-06-16) — critical for tx_off, whose silent failure would otherwise look
// like a clean unkey and let the release cancel the auto-off backstop, stranding
// PTT. For other protocols (Yaesu/Kenwood, which send no command ACK) it is a
// single fire-and-forget write, unchanged. The caller holds keyMu; the CI-V
// branch additionally takes cmdMu (lock order keyMu → cmdMu) for the ACK
// sequence, which is free of the generic command path because a keyed
// transmission already blocks SendCommands (ErrTxActive).
func (s *Service) writeKeyedLine(ctx context.Context, def cat.RigDefinition, cl serial.Client, line []byte, label string) error {
	if def.Protocol == cat.ProtocolIcomCIV {
		s.cmdMu.Lock()
		defer s.cmdMu.Unlock()
		return s.writeCIVFramesAwaitAck(ctx, cl, line, label)
	}
	return cl.WriteCommandBytes(ctx, line)
}

// readBackAfterCommand re-snapshots rig state after a valueless command (no
// sets_state, e.g. swap_vfo) that changed state in a way the bridge can't
// synthesize from the commanded value. It reuses the cached READ frames and the
// half-duplex-safe snapshot writer; the replies ride the normal decode→push
// path, so the SPA updates in ~100 ms instead of waiting for the liveness probe.
// Event-driven (fired by the ACK), not a timer poll. Best-effort: the command
// already succeeded (FB received), so a read-back write fault is logged, not
// returned as a command failure.
func (s *Service) readBackAfterCommand(ctx context.Context, cl serial.Client) {
	s.mu.Lock()
	bb := s.bootstrapBytes
	s.mu.Unlock()
	if len(bb) == 0 {
		return
	}
	if err := s.writeSnapshotReads(ctx, cl, true, bb); err != nil && s.logger != nil {
		s.logger.WarnWith().Str("error", errMessage(err)).Msg("bridge: CI-V read-back after command failed")
	}
}

// deliverAck routes a CI-V ACK from the readLoop to the SendCommands waiter.
// The channel is buffered (1) and we send non-blocking, so a stray ACK with no
// waiter (or a duplicate) is dropped rather than stalling the read loop.
func (s *Service) deliverAck(accepted bool) {
	s.mu.Lock()
	ch := s.pendingAck
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- accepted:
	default:
	}
}

// clearPendingAck detaches the waiter channel once SendCommands is done with it,
// but only if it's still the installed one — cmdMu already serialises batches,
// so this is just defensive against clobbering a successor.
func (s *Service) clearPendingAck(ch chan bool) {
	s.mu.Lock()
	if s.pendingAck == ch {
		s.pendingAck = nil
	}
	s.mu.Unlock()
}

// publishCommandedState synthesizes a rig-state push from a commanded (op,
// value) after the rig ACKs it — the CI-V confirm-by-ACK analogue of the
// Kenwood confirm-by-push (ADR 0034). The op's SetsState names the marker the
// value sets; the value is already in decoded state form (Hz digits for freq,
// the rig mode literal for mode), so it feeds the same mapStatusToPayload →
// hub.publish path the readLoop uses for a decoded frame. captureTuneSnapshot
// keeps the tune-restore snapshot current (a commanded mode change is a real
// state change). A no-op when the op declares no SetsState.
func (s *Service) publishCommandedState(def cat.RigDefinition, op, value string) {
	tag := cat.CommandSetsState(def, op)
	if tag == "" {
		return
	}
	payload, ok := mapStatusToPayload(cat.Status{tag: value})
	if !ok {
		return
	}
	s.captureTuneSnapshot(payload)
	s.hub.publish(Event{Name: EventRigState, Payload: payload})
}

// SendCommand is the single-op convenience over SendCommands.
func (s *Service) SendCommand(ctx context.Context, op, value string) error {
	return s.SendCommands(ctx, []RigCommand{{Op: op, Value: value}})
}
