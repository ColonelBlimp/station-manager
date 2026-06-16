//go:build cgo

package ft8

import (
	"github.com/ColonelBlimp/station-manager/internal/audio/playback"
)

// newTxPlayer builds the real (CGO) FT8 transmit output device: a malgo
// S16/12 kHz/mono playback.Player. deviceName selects the output device by its
// enumerated name (the per-rig RigConfig.Audio.TX model); deviceIndex is the
// legacy raw-index fallback (-1 = system default). The Service Init's it on arm
// and Close's it on disarm; the TxController drives Play/Stop per slot.
//
// Mirrors newCaptureSource — the CGO build wires the live device; the CGO-free
// build (txplayer_nocgo.go) returns an error so TX stays unavailable.
func newTxPlayer(deviceName string, deviceIndex int) (txPlayer, error) {
	cfg := playback.DefaultConfig() // 12 kHz mono S16
	cfg.DeviceName = deviceName
	cfg.DeviceIndex = deviceIndex
	return playback.New(cfg), nil
}
