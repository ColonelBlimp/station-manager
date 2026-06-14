//go:build cgo

package hardware

import (
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/gen2brain/malgo"
)

// AudioDevices enumerates the host's capture and playback devices via a
// temporary malgo context (allocate → query → free). Enumeration only queries
// the backend's device list; it does NOT open or grab a device, so it is safe
// to call alongside an active FT8 capture session. CGO build only — the
// CGO-free build's AudioDevices (audio_nocgo.go) returns ErrAudioUnavailable.
func AudioDevices() (capture, playback []AudioDevice, err error) {
	const op errors.Op = "hardware.AudioDevices"

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsg("initialising audio context")
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	capInfos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsg("enumerating capture devices")
	}
	playInfos, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsg("enumerating playback devices")
	}

	return toAudioDevices(capInfos), toAudioDevices(playInfos), nil
}

func toAudioDevices(infos []malgo.DeviceInfo) []AudioDevice {
	out := make([]AudioDevice, 0, len(infos))
	for i := range infos {
		out = append(out, AudioDevice{
			Name:      infos[i].Name(),
			IsDefault: infos[i].IsDefault != 0,
		})
	}
	return out
}
