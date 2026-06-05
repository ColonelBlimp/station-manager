package bridge

import (
	"context"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
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

	var line []byte
	for _, c := range cmds {
		b, err := cat.EncodeCommand(def, c.Op, c.Value)
		if err != nil {
			return errors.New(errOp).WithErr(err).WithMsgf("encode op %q", c.Op)
		}
		line = append(line, b...)
	}

	s.mu.Lock()
	cl := s.activeClient
	idOK := s.identityConfirmed
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
	return cl.WriteCommandBytes(ctx, line)
}

// SendCommand is the single-op convenience over SendCommands.
func (s *Service) SendCommand(ctx context.Context, op, value string) error {
	return s.SendCommands(ctx, []RigCommand{{Op: op, Value: value}})
}
