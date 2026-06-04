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

// SendCommand encodes a semantic rig operation and writes it to the connected
// rig (ADR 0026 inbound command path). It is rig-agnostic: op names a command
// in the daemon's one configured rigdef (cfg.Cat.Driver), and confirmation
// comes for free — the rig's AUTO-mode push flows back through the existing
// readLoop → SSE → catState chain, so SendCommand does not wait on a reply.
//
// op is the rigdef command name directly (no resolver); value is its single
// argument as a string (decimal Hz for set_freq, a rig mode literal for
// set_mode). The Exposed gate, value_map inversion, and padding are
// cat.EncodeCommand's job, so an internal (INIT/READ) or TX-capable
// (PLAYBACK) command can never be driven from here.
//
// Errors propagate for the API layer to map to the i18n envelope:
// cat.ErrUnknownCommand / cat.ErrCommandNotExposed / cat.ErrUnmappedValue
// from encoding, ErrRigNotConnected when no rig is up, or a serial write
// error. The write path stays inside internal/bridge (ADR 0013).
func (s *Service) SendCommand(ctx context.Context, op, value string) error {
	const errOp errors.Op = "bridge.Service.SendCommand"

	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		return errors.New(errOp).WithMsgf("no rig definition for driver %q", s.cfg.Cat.Driver)
	}
	b, err := cat.EncodeCommand(def, op, value)
	if err != nil {
		return errors.New(errOp).WithErr(err).WithMsgf("encode op %q", op)
	}

	s.mu.Lock()
	cl := s.activeClient
	s.mu.Unlock()
	if cl == nil {
		return errors.New(errOp).WithErr(ErrRigNotConnected).WithMsgf("op %q", op)
	}
	return cl.WriteCommandBytes(ctx, b)
}
