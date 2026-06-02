// Package capture wraps the public-domain miniaudio C library (via the
// gen2brain/malgo binding) to provide real-time audio input for the
// daemon's FT8 subsystem.
//
// This package is intentionally a peer subpackage of internal/audio
// (which holds the CGO-free DSP primitives — FFT, WAV, etc.). Keeping
// capture in its own subpackage isolates the CGO build dependency so
// importing internal/audio for an FFT or WAV read does not force a
// CGO toolchain on the consumer.
//
// Defaults match FT8's canonical sample format: 12 kHz, mono, float32.
// Use the Capture.Samples channel for buffered consumption or
// SetCallback for low-latency direct-from-callback processing.
//
// Lifecycle: New → Init → Start(ctx) → Stop / Close. All lifecycle
// methods are idempotent in the sense that a no-op call after the
// terminal state returns a sentinel error (ErrClosed, ErrNotRunning)
// rather than panicking.
//
// Concurrency: Init / Start / Stop / Close are mutex-protected. The
// hot audio callback path uses an atomic.Pointer for callback lookup
// and a TOCTOU-safe send to the Samples channel; both are
// lock-free.
package capture
