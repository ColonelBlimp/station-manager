//go:build cgo

package playback

import (
	stderr "errors"
	"testing"
)

// These tests pin the Player lifecycle contract WITHOUT a real output device:
// every case exercises an error/terminal path that returns before InitDevice,
// so they run in the normal CGO-on package suite (and the race pass) rather than
// only under the //go:build integration device tests. They are the regression
// the review (2026-06-19 L2) found missing — and what pins the now-terminal
// Close / ErrClosed contract (L1).

func TestDefaultConfig_FT8Canonical(t *testing.T) {
	c := DefaultConfig()
	if c.SampleRate != ft8SampleRateHz || c.Channels != 1 || c.DeviceIndex != -1 ||
		c.BufferSize != defaultCallbackFrames {
		t.Errorf("DefaultConfig = %+v, want 12kHz mono, default device, 512-frame period", c)
	}
}

func TestNew_NilLoggerGetsNoop(t *testing.T) {
	p := New(Config{})
	if p.config.Logger == nil {
		t.Error("New must default a nil Logger to the noop logger, not leave it nil")
	}
}

func TestErrorSentinelStrings(t *testing.T) {
	cases := map[error]string{
		ErrNotInitialized: "audio playback not initialized",
		ErrAlreadyPlaying: "audio playback already playing",
		ErrNotPlaying:     "audio playback not playing",
		ErrClosed:         "audio playback closed",
	}
	for err, want := range cases {
		if err.Error() != want {
			t.Errorf("sentinel = %q, want %q", err.Error(), want)
		}
	}
}

func TestListDevices_BeforeInit_NotInitialized(t *testing.T) {
	if _, err := New(DefaultConfig()).ListDevices(); !stderr.Is(err, ErrNotInitialized) {
		t.Errorf("ListDevices before Init = %v, want ErrNotInitialized", err)
	}
}

func TestPlay_BeforeInit_NotInitialized(t *testing.T) {
	p := New(DefaultConfig())
	if _, err := p.Play([]int16{0, 0}); !stderr.Is(err, ErrNotInitialized) {
		t.Errorf("Play before Init = %v, want ErrNotInitialized", err)
	}
	// The playing flag must be released on the early-return path so a later Play
	// isn't wrongly rejected as ErrAlreadyPlaying.
	if p.IsPlaying() {
		t.Error("playing flag left set after a failed Play")
	}
}

func TestStop_WhileIdle_NotPlaying(t *testing.T) {
	if err := New(DefaultConfig()).Stop(); !stderr.Is(err, ErrNotPlaying) {
		t.Errorf("Stop while idle = %v, want ErrNotPlaying", err)
	}
}

func TestPlay_AlreadyPlaying(t *testing.T) {
	p := New(DefaultConfig())
	p.playing.Store(true) // simulate an in-flight waveform without a real device
	if _, err := p.Play([]int16{0, 0}); !stderr.Is(err, ErrAlreadyPlaying) {
		t.Errorf("Play while playing = %v, want ErrAlreadyPlaying", err)
	}
}

func TestClose_IdempotentAndTerminal(t *testing.T) {
	p := New(DefaultConfig())

	// Close on a never-initialised Player is a clean no-op (Stop → ErrNotPlaying
	// is swallowed; the nil context is skipped).
	if err := p.Close(); err != nil {
		t.Fatalf("first Close = %v, want nil", err)
	}
	// Second Close must not panic and must stay nil.
	if err := p.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}

	// Terminal: every entry point returns ErrClosed after Close.
	if err := p.Init(); !stderr.Is(err, ErrClosed) {
		t.Errorf("Init after Close = %v, want ErrClosed", err)
	}
	if _, err := p.ListDevices(); !stderr.Is(err, ErrClosed) {
		t.Errorf("ListDevices after Close = %v, want ErrClosed", err)
	}
	if _, err := p.Play([]int16{0, 0}); !stderr.Is(err, ErrClosed) {
		t.Errorf("Play after Close = %v, want ErrClosed", err)
	}
}
