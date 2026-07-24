package ft8

import (
	"context"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
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

// TX timing (ADR 0032). FT8 rides a slot-synchronised timebase: symbol 0 of the
// waveform belongs at the slot boundary + txNominalDtSec — the nominal 0.5 s
// start WSJT-X uses (DT ≈ 0 at the receiver). When a rung is initiated after
// that point (the answer-a-CQ case: the partner's decode lands ~0.7 s into our
// slot), the controller drops the already-elapsed head and transmits the
// synchronised remainder in place — "truncate, don't shift" — so the receiver
// re-syncs on the Costas arrays (§8 of the QEX FT4/FT8 paper). txPreKeyLead keys
// PTT slightly before audio so the transmitter is fully up before RF; txPlayTail
// lets the device buffer drain before PTT drops. Lead/tail are package vars so
// tests dial them to zero; ADR 0030 makes them config if real hardware needs it.
const txNominalDtSec = 0.5

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

	// onTransmit, when set, is invoked with the slot boundary immediately after PTT
	// keys successfully (not before — a failed key must not mark the slot). The FT8
	// service wires this to record which slot we keyed so the decode loop can skip
	// decode + occupancy for it — the captured audio of a slot we transmitted in is
	// our own signal (rig TX bleed), meaningless for channel occupancy. Nil = no-op.
	onTransmit func(boundary time.Time)
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
// transmits it on the next UTC slot, on the synchronised timebase (symbol 0 at
// boundary + txNominalDtSec). Blocks until the transmission completes (or ctx is
// cancelled); PTT is guaranteed dropped before it returns. Errors if the message
// isn't an encodable standard message. Used for a manually-initiated CQ, where we
// pick our own slot/parity and start on time (no truncation).
func (c *TxController) TransmitSlot(ctx context.Context, text string, offsetHz float64) error {
	const op errors.Op = "ft8.TxController.TransmitSlot"

	wave, err := EncodeWaveform(text, offsetHz)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	boundary := nextSlotBoundary(time.Now().UTC())
	c.log.InfoWith().
		Str("text", text).
		Float64("offset_hz", offsetHz).
		Time("slot_utc", boundary).
		Msg("ft8 tx: transmitting on next slot")
	return c.transmitAligned(ctx, wave, boundary)
}

// TransmitCurrentSlot encodes a standard FT8 message and transmits it in the
// CURRENT UTC slot on the synchronised timebase (ADR 0032). Answering a CQ must
// land in the slot opposite the worked station's, which is only known after
// decoding their slot (~0.7 s into ours) — so by the time this is called the
// slot's nominal +0.5 s start has usually just passed, and transmitAligned drops
// the elapsed head and sends the synchronised remainder. The caller (the
// sequencer) owns the late-window guard; this shares the guaranteed stop with
// TransmitSlot.
func (c *TxController) TransmitCurrentSlot(ctx context.Context, text string, offsetHz float64) error {
	const op errors.Op = "ft8.TxController.TransmitCurrentSlot"
	wave, err := EncodeWaveform(text, offsetHz)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	// Current slot start = the next boundary minus one slot.
	boundary := nextSlotBoundary(time.Now().UTC()).Add(-SlotDuration)
	return c.transmitAligned(ctx, wave, boundary)
}

// transmitAligned transmits waveform on the synchronised timebase for the given
// slot boundary (ADR 0032). The waveform's symbol 0 belongs at boundary +
// txNominalDtSec; if that nominal start is still ahead we wait and send the full
// waveform (DT ≈ 0), and if it has already passed (a late rung) we drop the
// elapsed head and send the synchronised remainder, re-ramped to suppress the
// click at the new leading edge. PTT is keyed only ~txPreKeyLead before audio
// (never held across a long boundary wait) via the shared transmit() core, which
// guarantees the unkey on every path.
func (c *TxController) transmitAligned(ctx context.Context, waveform []int16, boundary time.Time) error {
	const op errors.Op = "ft8.TxController.transmitAligned"

	nominal := boundary.Add(time.Duration(txNominalDtSec * float64(time.Second)))
	// Audio begins at the nominal start, or as soon as PTT can settle if we're
	// already past it (a late rung).
	audioStart := nominal
	if earliest := time.Now().Add(txPreKeyLead); earliest.After(nominal) {
		audioStart = earliest
	}
	// Sleep until txPreKeyLead before audioStart; transmit() then keys, settles
	// that lead, and plays — so the first sample lands on audioStart.
	if err := sleepUntil(ctx, time.Until(audioStart.Add(-txPreKeyLead))); err != nil {
		return errors.New(op).WithErr(err).WithMsg("wait for slot start")
	}

	// Feasibility ESTIMATE only — the authoritative head-truncation happens in
	// transmit(), AFTER KeyTx + the settle lead (review 2026-07-20 #3): CAT
	// keying latency (mode switch, serial round-trip) postdates this point, so
	// truncating here would start audio late relative to the computed timebase
	// and shift every symbol off its DT. This pre-key check only avoids keying
	// PTT for a rung that already has nothing left to send.
	if skip := int(audioStart.Sub(nominal).Seconds() * float64(goft8.SampleRate)); skip >= len(waveform) {
		return errors.New(op).WithMsg("too late in slot; nothing left to transmit")
	}
	// Record this slot ONLY after PTT keys successfully (onKeyed, below) — not here.
	// A failed key (single-flight held by a tune carrier, a rig blip in the boundary
	// wait) means no RF went out, so the slot is a normal RX slot; marking it would
	// make decodeLoop wrongly skip its occupancy. onTransmit records the slot so the
	// decode loop skips its own-TX audio — see the TxController.onTransmit doc.
	return c.transmit(ctx, waveform, nominal, func() {
		if c.onTransmit != nil {
			c.onTransmit(boundary)
		}
	})
}

// transmit is the key → play → unkey core, independent of slot timing so it is
// unit-testable. onKeyed, if non-nil, is invoked exactly once immediately after
// KeyTx succeeds (before audio) — the caller uses it to mark the slot only once
// RF is actually going out. nominal, when non-zero, is the waveform's symbol-0
// time on the synchronised timebase: the head is truncated against the ACTUAL
// post-key clock (review 2026-07-20 #3 — KeyTx's CAT latency, mode switch and
// scheduler delay land between the caller's estimate and the first sample, and
// an untruncated head would shift every symbol off its computed DT). A zero
// nominal plays the waveform as-is. PTT is unkeyed on EVERY return path
// (success, play error, or ctx cancel) via a deferred guard, on a background
// context so a cancelled parent ctx can't skip the unkey — the bridge's
// auto-off backstop is the further safety net.
func (c *TxController) transmit(ctx context.Context, waveform []int16, nominal time.Time, onKeyed func()) error {
	const op errors.Op = "ft8.TxController.transmit"

	if err := c.keyer.KeyTx(ctx, c.mode); err != nil {
		return errors.New(op).WithErr(err).WithMsg("key tx")
	}
	if onKeyed != nil {
		onKeyed()
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

	// Authoritative head-truncation, against the clock the first sample will
	// actually start on (we are about to Play). Symbol 0 belongs at nominal;
	// whatever of the waveform now lies in the past is dropped so the remainder
	// stays on the synchronised timebase.
	wave := waveform
	if !nominal.IsZero() {
		if late := time.Since(nominal); late > 0 {
			wave = truncateHead(waveform, int(late.Seconds()*float64(goft8.SampleRate)))
		}
		if len(wave) == 0 {
			return errors.New(op).WithMsg("too late after keying; nothing left to transmit")
		}
	}

	done, err := c.player.Play(wave)
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
	// A cancel during the drain (Abandon, disarm, shutdown) halts output early —
	// player.done only means the samples reached the device, so the still-buffered
	// tail is clipped and the rung did NOT finish cleanly on air. Report it like
	// the mid-play cancel above so onDone sees failure and the final-rung QSO is
	// not logged; a bare `return nil` here would mark an interrupted rung complete.
	// This also settles the first-select race: if both done and ctx.Done() were
	// ready and it took done, the interrupted transmit is still reported cancelled.
	if err := ctx.Err(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("transmit cancelled during drain")
	}
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
