//go:build integration

package audio

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests require audio output hardware and are skipped by default.
// Run with: go test -tags=integration ./audio/

// buildSineWAV generates a 1s 440 Hz sine wave as a PCM16 mono WAV file.
// 1s is long enough to survive PipeWire/PulseAudio output latency (~100-200ms)
// and still be clearly audible when the integration test runs.
func buildSineWAV(t *testing.T) string {
	t.Helper()
	const (
		sampleRate = 48000
		freq       = 440.0
		duration   = 1 * time.Second
	)
	numSamples := int(sampleRate * float64(duration) / float64(time.Second))
	samples := make([]float32, numSamples)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / sampleRate))
	}
	return buildWAV(t, wavAudioFormatPCM, 1, sampleRate, 16, pcm16Bytes(samples))
}

func TestPlayback_ListDevices_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	devices, err := p.ListDevices()
	require.NoError(t, err)

	t.Logf("Found %d playback device(s):", len(devices))
	for i, d := range devices {
		t.Logf("  [%d] %s", i, d.Name())
	}
}

func TestPlayback_Init_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())
	require.NotNil(t, p.ctx)
}

func TestPlayback_PlayFile_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	path := buildSineWAV(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	start := time.Now()
	require.NoError(t, p.PlayFile(ctx, path))
	elapsed := time.Since(start)

	// A 1s WAV + 500ms drain should take ~1.5s.
	// If it returned in < 200ms the audio callback never fired —
	// check system audio routing (pavucontrol, qpwgraph, system volume).
	t.Logf("PlayFile completed in %v", elapsed)
	require.Greater(t, elapsed, 200*time.Millisecond,
		"PlayFile returned too quickly — audio callback may not have fired; check audio routing")

	require.False(t, p.IsPlaying())
}

func TestPlayback_Stop_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	path := buildSineWAV(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- p.PlayFile(ctx, path)
	}()

	// Give the device time to start.
	time.Sleep(20 * time.Millisecond)
	require.True(t, p.IsPlaying())

	require.NoError(t, p.Stop())

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlayFile did not return after Stop()")
	}
	require.False(t, p.IsPlaying())
}

func TestPlayback_ContextCancel_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	path := buildSineWAV(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- p.PlayFile(ctx, path)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlayFile did not return after context cancel")
	}
}

func TestPlayback_Close_DuringPlay_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())

	path := buildSineWAV(t)
	done := make(chan error, 1)
	go func() {
		done <- p.PlayFile(context.Background(), path)
	}()

	time.Sleep(20 * time.Millisecond)
	require.NoError(t, p.Close())

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlayFile did not return after Close()")
	}
	require.True(t, p.closed.Load())
}
