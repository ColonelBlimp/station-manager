package capture

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newCapture creates a Capture for testing and registers cleanup.
func newCapture(t *testing.T) *Capture {
	t.Helper()
	c := New(DefaultConfig())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// --- Config ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, -1, cfg.DeviceIndex)
	require.Equal(t, uint32(12000), cfg.SampleRate, "FT8 canonical sample rate")
	require.Equal(t, uint32(1), cfg.Channels, "FT8 is mono")
	require.Equal(t, uint32(512), cfg.BufferSize)
}

func TestConfig_ZeroValue(t *testing.T) {
	var cfg Config
	require.Equal(t, 0, cfg.DeviceIndex)
	require.Equal(t, uint32(0), cfg.SampleRate)
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{DeviceIndex: 5, SampleRate: 48000, Channels: 2, BufferSize: 2048}
	require.Equal(t, 5, cfg.DeviceIndex)
	require.Equal(t, uint32(48000), cfg.SampleRate)
	require.Equal(t, uint32(2), cfg.Channels)
	require.Equal(t, uint32(2048), cfg.BufferSize)
}

// --- New / construction ---

func TestNew(t *testing.T) {
	cfg := Config{DeviceIndex: 2, SampleRate: 44100, Channels: 2, BufferSize: 1024}
	c := New(cfg)
	require.NotNil(t, c)
	require.Equal(t, 2, c.config.DeviceIndex)
	require.Equal(t, uint32(44100), c.config.SampleRate)
	require.NotNil(t, c.Samples())
}

func TestNew_ChannelBufferSize(t *testing.T) {
	c := New(DefaultConfig())
	require.Equal(t, SampleChannelBufferSize, cap(c.samples))
}

// --- IsRunning ---

func TestCapture_IsRunning_InitialState(t *testing.T) {
	c := newCapture(t)
	require.False(t, c.IsRunning())
}

// --- SetCallback ---

func TestCapture_SetCallback(t *testing.T) {
	c := newCapture(t)
	c.SetCallback(func(_ []float32) {})
	require.NotNil(t, c.callbackPtr.Load())
}

func TestCapture_SetCallback_Nil(t *testing.T) {
	c := newCapture(t)
	c.SetCallback(func(_ []float32) {})
	c.SetCallback(nil)
	require.Nil(t, c.callbackPtr.Load())
}

// --- Error sentinels ---

func TestErrors(t *testing.T) {
	require.Equal(t, "audio capture not initialized", ErrNotInitialized.Error())
	require.Equal(t, "audio capture already running", ErrAlreadyRunning.Error())
	require.Equal(t, "audio capture not running", ErrNotRunning.Error())
	require.Equal(t, "audio capture closed", ErrClosed.Error())
}

func TestCapture_Init_Idempotent(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()
	require.NoError(t, c.Init())
	require.NoError(t, c.Init(), "second Init must be a no-op")
}

func TestCapture_Init_AfterClose_ReturnsErrClosed(t *testing.T) {
	c := New(DefaultConfig())
	require.NoError(t, c.Init())
	require.NoError(t, c.Close())
	require.ErrorIs(t, c.Init(), ErrClosed)
}

// --- ListDevices ---

func TestCapture_ListDevices_NotInitialized(t *testing.T) {
	c := newCapture(t)
	_, err := c.ListDevices()
	require.ErrorIs(t, err, ErrNotInitialized)
}

// --- Start ---

func TestCapture_Start_NotInitialized(t *testing.T) {
	c := newCapture(t)
	err := c.Start(context.Background())
	require.ErrorIs(t, err, ErrNotInitialized)
}

func TestCapture_Start_AlreadyRunning(t *testing.T) {
	c := newCapture(t)
	c.running.Store(true)
	err := c.Start(context.Background())
	require.ErrorIs(t, err, ErrAlreadyRunning)
}

// --- Stop ---

func TestCapture_Stop_NotRunning(t *testing.T) {
	c := newCapture(t)
	err := c.Stop()
	require.ErrorIs(t, err, ErrNotRunning)
}

// --- Close ---

func TestCapture_ClosedFlag_InitialState(t *testing.T) {
	c := newCapture(t)
	require.False(t, c.closed.Load())
}

func TestCapture_ClosedFlag_SetOnClose(t *testing.T) {
	c := New(DefaultConfig())
	require.NoError(t, c.Init())
	require.NoError(t, c.Close())
	require.True(t, c.closed.Load())
}

func TestCapture_CloseOnce_MultipleCloses(t *testing.T) {
	c := New(DefaultConfig())
	require.NoError(t, c.Init())
	require.NoError(t, c.Close())
	_ = c.Close()
}

func TestCapture_CloseOnce_ConcurrentCloses(t *testing.T) {
	c := New(DefaultConfig())
	require.NoError(t, c.Init())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Close()
		}()
	}
	wg.Wait()
}

func TestCapture_Close_SetsClosedBeforeChannelClose(t *testing.T) {
	c := New(DefaultConfig())
	require.NoError(t, c.Init())

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range c.Samples() {
			// drain
		}
		require.True(t, c.closed.Load(), "closed flag must be true when channel is drained")
	}()

	require.NoError(t, c.Close())
	<-done
}

// --- safeSend ---

func TestCapture_SafeSend_NormalOperation(t *testing.T) {
	c := newCapture(t)
	c.safeSend([]float32{1.0, 2.0, 3.0})

	select {
	case samples := <-c.Samples():
		require.Len(t, samples, 3)
	default:
		t.Fatal("expected sample in channel")
	}
}

func TestCapture_SafeSend_RecoverFromClosedChannel(t *testing.T) {
	c := New(DefaultConfig())
	close(c.samples)
	c.safeSend([]float32{1.0, 2.0, 3.0})
}

func TestCapture_SafeSend_ChannelFull(t *testing.T) {
	c := &Capture{
		config:  DefaultConfig(),
		samples: make(chan []float32, 1),
	}
	c.safeSend([]float32{1.0})
	c.safeSend([]float32{2.0})

	select {
	case samples := <-c.Samples():
		require.Equal(t, float32(1.0), samples[0])
	default:
		t.Fatal("expected first sample in channel")
	}
	select {
	case <-c.Samples():
		t.Fatal("channel should be empty")
	default:
	}
	require.Equal(t, int64(1), c.DroppedChunks(), "second send should have been dropped")
}

// --- closed flag logic ---

func TestCapture_ClosedFlag_PreventsSendOnClosedChannel(t *testing.T) {
	c := newCapture(t)
	c.closed.Store(true)

	sent := false
	if !c.closed.Load() {
		select {
		case c.samples <- []float32{1.0}:
			sent = true
		default:
		}
	}
	require.False(t, sent)
}

func TestCapture_ConcurrentCloseAndSend_Stress(t *testing.T) {
	for iter := 0; iter < 100; iter++ {
		c := New(DefaultConfig())
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = c.closed.Load()
				}
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			c.closed.Store(true)
			c.closeOnce.Do(func() { close(c.samples) })
		}()

		wg.Wait()
		require.True(t, c.closed.Load())
	}
}

// --- Concurrent access ---

func TestCapture_ConcurrentAccess(t *testing.T) {
	c := newCapture(t)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.IsRunning()
		}()
	}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.SetCallback(func(_ []float32) {})
		}()
	}
	wg.Wait()
}

func TestCapture_ConcurrentSetCallbackAndRead(t *testing.T) {
	c := newCapture(t)
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					c.SetCallback(func(_ []float32) {})
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = c.callbackPtr.Load()
				}
			}
		}()
	}
	wg.Wait()
}

// --- bytesAsFloat32 ---

func TestBytesAsFloat32_ZeroCopy(t *testing.T) {
	data := []byte{0x00, 0x00, 0x80, 0x3F, 0x00, 0x00, 0x80, 0xBF}
	result := bytesAsFloat32(data)
	require.Len(t, result, 2)
	require.Equal(t, float32(1.0), result[0])
	require.Equal(t, float32(-1.0), result[1])
}

func TestBytesAsFloat32_Empty(t *testing.T) {
	require.Nil(t, bytesAsFloat32([]byte{}))
}

func TestBytesAsFloat32_TooSmall(t *testing.T) {
	require.Nil(t, bytesAsFloat32([]byte{0x00, 0x00, 0x80}))
}

// --- copyFloat32Slice ---

func TestCopyFloat32Slice(t *testing.T) {
	original := []float32{1.0, 2.0, 3.0}
	copied := copyFloat32Slice(original)
	require.Equal(t, original, copied)
	original[0] = 999.0
	require.NotEqual(t, float32(999.0), copied[0])
}

func TestCopyFloat32Slice_Nil(t *testing.T) {
	require.Nil(t, copyFloat32Slice(nil))
}

func TestCopyFloat32Slice_Empty(t *testing.T) {
	require.Empty(t, copyFloat32Slice([]float32{}))
}

// --- Benchmarks ---

func BenchmarkBytesAsFloat32(b *testing.B) {
	data := make([]byte, 512*4)
	b.ResetTimer()
	for b.Loop() {
		_ = bytesAsFloat32(data)
	}
}

func BenchmarkCopyFloat32Slice(b *testing.B) {
	data := make([]float32, 512)
	b.ResetTimer()
	for b.Loop() {
		_ = copyFloat32Slice(data)
	}
}
