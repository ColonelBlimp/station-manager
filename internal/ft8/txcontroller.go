package ft8

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// FT8 TX controller (ADR 0030 step d) — the first feature that keys real RF.
// It orchestrates one slot's transmission: encode → align to the UTC slot
// boundary → key PTT → play the GFSK waveform → unkey. The safety-critical
// guarantees (hard auto-off, release-on-disconnect, single-flight) live in the
// TxKeyer implementation (the bridge); this controller adds the audio + timing
// and a belt-and-suspenders unconditional unkey on every return path.
//
// It is deliberately standalone (not wired into the daemon's ft8.Service): in
// step (d) only the gated cmd/ft8-tx-probe constructs and drives it, so no SPA
// or daemon path can transmit. Step (e) wires it to the sequencer + SPA.

// TX timing. txSlotDtSec lays the waveform at the very start of the slot
// (EncodeToSlot pads the rest with silence); the controller aligns Play to the
// UTC boundary. txPreKeyLead keys PTT slightly before audio so the transmitter
// is fully up before RF; txPlayTail lets the device buffer drain after the
// waveform is handed over before PTT drops. Lead/tail are package vars so tests
// dial them to zero; ADR 0030 makes them config if real hardware needs tuning.
const txSlotDtSec = 0.0

var (
	txPreKeyLead = 200 * time.Millisecond
	txPlayTail   = 250 * time.Millisecond
)

// TxController transmits standard FT8 messages. One transmission at a time —
// the underlying keyer + player are each single-shot, and the bridge's
// single-flight enforces it at the hardware level.
type TxController struct {
	keyer  TxKeyer
	player slotPlayer
	mode   string // rig data-mode literal to switch to before keying; "" = leave as-is
	log    logging.Logger
}

// NewTxController builds the controller from an injected keyer (PTT) and player
// (audio out). mode is the rig data-mode literal (ft8.tx.mode); empty leaves the
// rig's current mode untouched.
func NewTxController(keyer TxKeyer, player slotPlayer, mode string, log logging.Logger) *TxController {
	if log == nil {
		log = logging.Noop()
	}
	return &TxController{keyer: keyer, player: player, mode: mode, log: log}
}

// TransmitSlot encodes a standard FT8 message at the given base offset and
// transmits it on the next UTC slot boundary. Blocks until the transmission
// completes (or ctx is cancelled); PTT is guaranteed dropped before it returns.
// Errors if the message isn't an encodable standard message.
func (c *TxController) TransmitSlot(ctx context.Context, text string, offsetHz float64) error {
	const op errors.Op = "ft8.TxController.TransmitSlot"

	wave, err := EncodeToSlot(text, offsetHz, txSlotDtSec)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}

	// Align so the waveform's first sample plays at the slot boundary: wake
	// txPreKeyLead early to key PTT, settle, then Play lands on the boundary.
	target := nextSlotBoundary(time.Now().UTC())
	if err := sleepUntil(ctx, time.Until(target.Add(-txPreKeyLead))); err != nil {
		return errors.New(op).WithErr(err).WithMsg("wait for slot boundary")
	}
	c.log.InfoWith().
		Str("text", text).
		Float64("offset_hz", offsetHz).
		Time("slot_utc", target).
		Msg("ft8 tx: transmitting on next slot")
	return c.transmit(ctx, wave)
}

// TransmitNow encodes a standard FT8 message and transmits its bare waveform
// IMMEDIATELY — no wait for a slot boundary (ADR 0031 sequencer timing). Answering
// a CQ must land in the slot opposite the worked station, which is only known
// after decoding their slot (~1.5 s into ours), so the rung is sent in the
// current slot started late. The caller (the sequencer) owns the late-start guard;
// this just key → play → unkey with the same guaranteed stop as TransmitSlot.
func (c *TxController) TransmitNow(ctx context.Context, text string, offsetHz float64) error {
	const op errors.Op = "ft8.TxController.TransmitNow"
	wave, err := EncodeWaveform(text, offsetHz)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	return c.transmit(ctx, wave)
}

// transmit is the key → play → unkey core, independent of slot timing so it is
// unit-testable. PTT is unkeyed on EVERY return path (success, play error, or
// ctx cancel) via a deferred guard, on a background context so a cancelled
// parent ctx can't skip the unkey — the bridge's auto-off backstop is the
// further safety net.
func (c *TxController) transmit(ctx context.Context, waveform []int16) error {
	const op errors.Op = "ft8.TxController.transmit"

	if err := c.keyer.KeyTx(ctx, c.mode); err != nil {
		return errors.New(op).WithErr(err).WithMsg("key tx")
	}
	unkeyed := false
	unkey := func() {
		if unkeyed {
			return
		}
		unkeyed = true
		if err := c.keyer.UnkeyTx(context.Background()); err != nil {
			c.log.ErrorWith().Err(err).Msg("ft8 tx: unkey failed (backstop will retry)")
		}
	}
	defer unkey()

	// Let PTT engage before audio.
	if err := sleepUntil(ctx, txPreKeyLead); err != nil {
		return errors.New(op).WithErr(err).WithMsg("pre-key settle")
	}

	done, err := c.player.Play(waveform)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("play waveform")
	}

	select {
	case <-done:
	case <-ctx.Done():
		_ = c.player.Stop()
		return errors.New(op).WithErr(ctx.Err()).WithMsg("transmit cancelled")
	}

	// Let the device buffer drain, then halt output before dropping PTT so the
	// tail of the waveform isn't clipped.
	if txPlayTail > 0 {
		t := time.NewTimer(txPlayTail)
		select {
		case <-t.C:
		case <-ctx.Done():
		}
		t.Stop()
	}
	_ = c.player.Stop()
	return nil
}

// sleepUntil blocks for d (no-op if non-positive), returning early with the
// context error if ctx is cancelled.
func sleepUntil(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
