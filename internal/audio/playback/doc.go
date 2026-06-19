// Package playback wraps the public-domain miniaudio C library (via the
// gen2brain/malgo binding) to provide real-time audio OUTPUT for the
// daemon's FT8 transmit path (ADR 0029 step c).
//
// It is the output mirror of internal/audio/capture: a peer subpackage of
// internal/audio that isolates the CGO build dependency, so importing
// internal/audio for an FFT or WAV read does not force a CGO toolchain on
// the consumer. The real device lives in playback.go behind //go:build cgo;
// the static, CGO-free default build excludes it entirely (this doc.go
// carries the package clause there, alongside the build-tag-free pure
// helpers in buffer.go).
//
// Format matches what the FT8 modulator emits: 12 kHz, mono, signed 16-bit
// (FormatS16) — so the int16 waveform from ft8.EncodeToSlot streams straight
// to the device with no float conversion.
//
// RF safety: this layer produces AUDIO only. It never keys the rig — PTT and
// the daemon-owned guaranteed-stop sequencing live above it in the FT8 transmit
// controller and the bridge keyer (ADR 0030, shipped). Playing a waveform here
// drives a sound card, not a transmitter.
//
// Lifecycle: New → Init → Play(samples) → (wait on the returned done channel)
// → Stop / Close. Stop halts output immediately; Close releases the audio
// context and is terminal — once closed, Init / ListDevices / Play return
// ErrClosed (a fresh Player is created per FT8 session), mirroring
// internal/audio/capture. All terminal-state calls return a sentinel error
// rather than panicking.
package playback
