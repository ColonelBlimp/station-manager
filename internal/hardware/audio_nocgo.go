//go:build !cgo

package hardware

// AudioDevices on the static CGO-free build cannot enumerate audio — the
// malgo/miniaudio backend isn't compiled in. It returns ErrAudioUnavailable
// (defined in hardware.go) so the handler reports audio as unavailable rather
// than treating it as a failure. Live audio needs the CGO build (ADR 0024).
func AudioDevices() (capture, playback []AudioDevice, err error) {
	return nil, nil, ErrAudioUnavailable
}
