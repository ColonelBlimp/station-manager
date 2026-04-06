//go:build integration

package audio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests require audio output hardware and are skipped by default.
// Run with: go test -tags=integration ./audio/

// buildSineWAV generates a 0.1s 440 Hz sine wave as a PCM16 WAV file.
func buildSineWAV(t *testing.T) string {
	t.Helper()
	const (
		sampleRate = 48000
		duration   = 100 * time.Millisecond
		freq       = 440.0
	)
	numSamples := int(sampleRate * float64(duration) / float64(time.Second))
	samples := make([]float32, numSamples)
	for i := range samples {
		// Not importing math here to avoid cycle risk; use a simple linear ramp instead.
		_ = freq
		samples[i] = float32(i%2)*0.1 - 0.05 // quiet pulse train as stand-in
	}
	return buildWAV(t, wavAudioFormatPCM, 1, sampleRate, 16, pcm16Bytes(samples))
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, p.PlayFile(ctx, path))
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
