package ft8

import "context"

// TxKeyer is the PTT seam for FT8 transmit (ADR 0030). The TX controller drives
// PTT through this interface so internal/ft8 never imports internal/bridge —
// preserving the narrow-daemon-scope / sibling-isolation import graph (ADR
// 0013/0021), the same way captureSource isolates the audio-capture device.
//
// internal/bridge.Service satisfies it (via KeyFt8Tx/UnkeyFt8Tx, adapted in
// cmd/smd): it owns the guaranteed-stop machinery — hard auto-off,
// release-on-disconnect, single-flight shared with the tune carrier, and the
// rig-identity gate — reused, not reinvented, here.
//
// KeyTx keys PTT, optionally switching to the rig data-mode literal `mode`
// first (empty = leave the rig's current mode). UnkeyTx drops PTT and restores
// the pre-TX mode; it must be safe to call even after a failed KeyTx (no-op)
// and on any error/cancel path — the controller calls it unconditionally.
//
// TxReady reports whether the keyer can key right now: a rig is connected, its
// identity is verified, AND no other keyed transmission (tune carrier or an
// in-flight FT8 key) currently holds the single PTT. The Service's arm-gate
// consults it so arming is refused — with a retryable error — when the rig is
// off, reconnecting, or already transmitting, rather than failing at the slot
// boundary. It must be cheap and non-blocking.
type TxKeyer interface {
	KeyTx(ctx context.Context, mode string) error
	UnkeyTx(ctx context.Context) error
	TxReady() bool
}

// slotPlayer is the audio-output seam — the subset of internal/audio/playback.
// Player the controller needs. An interface (rather than a direct import) keeps
// the controller CGO-free and unit-testable with a fake; the real malgo Player
// (CGO) is injected by the probe/wirer, already Init'd. Play is non-blocking and
// returns a channel closed when the whole waveform has been handed to the
// device; Stop halts output immediately. The caller (this controller) owns the
// stop.
type slotPlayer interface {
	Play(samples []int16) (<-chan struct{}, error)
	Stop() error
}
