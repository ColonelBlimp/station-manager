//go:build integration && cgo

package playback

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Integration tests require real audio hardware and are skipped by default.
// Run with: go test -tags=integration ./internal/audio/playback/

func TestPlayer_Init_Integration(t *testing.T) {
	p := New(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())
	require.NotNil(t, p.ctx)
}

func TestPlayer_ListDevices_Integration(t *testing.T) {
	p := New(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	devices, err := p.ListDevices()
	require.NoError(t, err)

	t.Logf("Found %d playback device(s):", len(devices))
	for i, d := range devices {
		t.Logf("  [%d] %s", i, d.Name())
	}
}

func TestPlayer_PlaysToCompletion_Integration(t *testing.T) {
	p := New(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	// A short 440 Hz tone (0.25 s at 12 kHz).
	const n = 3000
	tone := make([]int16, n)
	for i := range tone {
		tone[i] = int16(0.3 * math.MaxInt16 * math.Sin(2*math.Pi*440*float64(i)/ft8SampleRateHz))
	}

	done, err := p.Play(tone)
	require.NoError(t, err)
	require.True(t, p.IsPlaying())

	select {
	case <-done:
		// Natural end reached.
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for waveform to finish")
	}

	require.NoError(t, p.Stop())
	require.False(t, p.IsPlaying())
}

func TestPlayer_StopMidWaveform_Integration(t *testing.T) {
	p := New(DefaultConfig())
	defer p.Close()

	require.NoError(t, p.Init())

	// 5 s of silence — long enough that we Stop well before its natural end.
	long := make([]int16, ft8SampleRateHz*5)

	_, err := p.Play(long)
	require.NoError(t, err)
	require.True(t, p.IsPlaying())

	time.Sleep(100 * time.Millisecond)

	require.NoError(t, p.Stop())
	require.False(t, p.IsPlaying())
	require.ErrorIs(t, p.Stop(), ErrNotPlaying)
}
