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
	// If it returned in < 100ms the audio callback almost certainly never fired.
	// The threshold is kept low (100ms rather than 200ms) to avoid flakes on
	// slow VMs where device start-up latency eats into the measured time.
	t.Logf("PlayFile completed in %v", elapsed)
	require.Greater(t, elapsed, 100*time.Millisecond,
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

	// Wait for the device to start rather than a fixed sleep.
	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")

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

	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")
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

	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")
	require.NoError(t, p.Close())

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlayFile did not return after Close()")
	}
	require.True(t, p.closed.Load())
}

// --------------- PlaySamples — integration tests -----------------------------

// buildSineSamples generates a 1-second 440 Hz mono sine wave as []float32 at
// the given sample rate. Used for PlaySamples integration tests.
func buildSineSamples(sampleRate int) []float32 {
	const freq = 440.0
	samples := make([]float32, sampleRate) // 1 second
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}
	return samples
}

func TestPlayback_PlaySamples_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	samples := buildSineSamples(48000)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	start := time.Now()
	require.NoError(t, p.PlaySamples(ctx, samples, 48000, 1))
	elapsed := time.Since(start)

	// A 1s buffer + 500ms drain should take ~1.5s.
	// If it returned in < 100ms the audio callback almost certainly never fired.
	t.Logf("PlaySamples completed in %v", elapsed)
	require.Greater(t, elapsed, 100*time.Millisecond,
		"PlaySamples returned too quickly — audio callback may not have fired; check audio routing")

	require.False(t, p.IsPlaying())
}

func TestPlayback_PlaySamples_Stop_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	samples := buildSineSamples(48000)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- p.PlaySamples(ctx, samples, 48000, 1)
	}()

	// Wait for the device to start rather than a fixed sleep.
	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")

	require.NoError(t, p.Stop())

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlaySamples did not return after Stop()")
	}
	require.False(t, p.IsPlaying())
}

func TestPlayback_PlaySamples_ContextCancel_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	samples := buildSineSamples(48000)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- p.PlaySamples(ctx, samples, 48000, 1)
	}()

	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlaySamples did not return after context cancel")
	}
}

func TestPlayback_PlaySamples_Close_DuringPlay_Integration(t *testing.T) {
	p := NewPlayback(DefaultConfig())
	require.NoError(t, p.Init())

	samples := buildSineSamples(48000)
	done := make(chan error, 1)
	go func() {
		done <- p.PlaySamples(context.Background(), samples, 48000, 1)
	}()

	require.Eventually(t, p.IsPlaying, 500*time.Millisecond, 5*time.Millisecond,
		"playback did not start in time")
	require.NoError(t, p.Close())

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("PlaySamples did not return after Close()")
	}
	require.True(t, p.closed.Load())
}
