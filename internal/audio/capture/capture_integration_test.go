//go:build integration && cgo

package capture

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Integration tests require real audio hardware and are skipped by
// default. Run with: go test -tags=integration ./internal/audio/capture/

func TestCapture_Init_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())
	require.NotNil(t, c.ctx)
}

func TestCapture_ListDevices_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())

	devices, err := c.ListDevices()
	require.NoError(t, err)

	t.Logf("Found %d capture device(s):", len(devices))
	for i, d := range devices {
		t.Logf("  [%d] %s", i, d.Name())
	}
}

func TestCapture_StartStop_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.Start(ctx))
	require.True(t, c.IsRunning())

	time.Sleep(100 * time.Millisecond)

	require.NoError(t, c.Stop())
	require.False(t, c.IsRunning())
}

func TestCapture_ReceivesSamples_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, c.Start(ctx))

	select {
	case samples := <-c.Samples():
		t.Logf("Received %d samples", len(samples))
		require.NotEmpty(t, samples)
	case <-ctx.Done():
		t.Fatal("timeout waiting for samples")
	}
}

func TestCapture_Callback_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())

	called := make(chan struct{}, 1)
	c.SetCallback(func(_ []float32) {
		select {
		case called <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, c.Start(ctx))

	select {
	case <-called:
		t.Log("callback invoked")
	case <-ctx.Done():
		t.Fatal("timeout waiting for callback")
	}
}

func TestCapture_Close_Integration(t *testing.T) {
	c := New(DefaultConfig())

	require.NoError(t, c.Init())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, c.Start(ctx))
	require.NoError(t, c.Close())
	require.False(t, c.IsRunning())
}

func TestCapture_ContextCancellation_Integration(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()

	require.NoError(t, c.Init())

	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, c.Start(ctx))
	require.True(t, c.IsRunning())

	cancel()
	time.Sleep(100 * time.Millisecond)

	require.False(t, c.IsRunning())
}
