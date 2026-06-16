//go:build !cgo

package ft8

import "github.com/ColonelBlimp/station-manager/internal/errors"

// newTxPlayer on the CGO-free (static default) build has no audio output: FT8
// transmit needs the malgo (CGO) playback device, so arming returns
// "tx unavailable" (ErrTxUnavailable) and the static daemon never keys the rig.
// Mirrors newCaptureSource leaving capture idle on this build (ADR 0024/0030).
func newTxPlayer(string, int) (txPlayer, error) {
	const op errors.Op = "ft8.newTxPlayer"
	return nil, errors.New(op).WithErr(ErrTxUnavailable).
		WithMsg("built without CGO audio output")
}
