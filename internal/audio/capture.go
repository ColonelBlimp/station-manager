// internal/audio/capture.go
package audio

import (
	"context"
	stderr "errors"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/gen2brain/malgo"
)

const (
	// SampleChannelBufferSize is the capacity of the Samples channel.
	// Provides buffering between audio callback and consumer.
	SampleChannelBufferSize = 64
	// BytesPerFloat32 is the number of bytes in a float32 sample
	BytesPerFloat32 = 4
)

var (
	ErrNotInitialized = stderr.New("audio capture not initialized")
	ErrAlreadyRunning = stderr.New("audio capture already running")
	ErrNotRunning     = stderr.New("audio capture not running")
	ErrClosed         = stderr.New("audio capture closed")
)

// Config holds audio capture configuration
type Config struct {
	DeviceIndex int            // -1 for default device
	SampleRate  uint32         // e.g., 48000
	Channels    uint32         // 1 for mono, 2 for stereo
	BufferSize  uint32         // frames per callback
	Logger      logging.Logger // nil defaults to no-op
}

// DefaultConfig returns sensible defaults for audio capture
func DefaultConfig() Config {
	return Config{
		DeviceIndex: -1,
		SampleRate:  48000,
		Channels:    1,
		BufferSize:  512,
	}
}

// SampleCallback is called directly from the audio thread with new samples.
// Use for low-latency processing. Must be non-blocking and fast.
// WARNING: The samples slice is only valid for the duration of the callback.
type SampleCallback func(samples []float32)

// Capture handles real-time audio sampling from an audio device
type Capture struct {
	config  Config
	ctx     *malgo.AllocatedContext
	device  *malgo.Device
	running atomic.Bool
	closed  atomic.Bool // prevents sends to closed channel
	mu      sync.Mutex  // protects ctx, device, and cancelInternal

	// cancelInternal cancels the per-Start internal context, signalling the
	// context-watcher goroutine to exit when Stop/Close is called directly
	// rather than via context cancellation.
	cancelInternal context.CancelFunc

	// Atomic pointer for lock-free callback access in hot path
	callbackPtr atomic.Pointer[SampleCallback]

	// samples is the internal send/close end of the output channel.
	// External consumers receive from it via the Samples() accessor.
	samples   chan []float32
	closeOnce sync.Once // ensures channel is closed only once
}

// New creates a new audio capture instance
func New(cfg Config) *Capture {
	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}
	return &Capture{
		config:  cfg,
		samples: make(chan []float32, SampleChannelBufferSize),
	}
}

// Samples returns a receive-only channel that delivers audio sample buffers.
// Each buffer contains float32 samples normalised to [-1.0, 1.0].
// The channel is closed when Close is called.
func (c *Capture) Samples() <-chan []float32 {
	return c.samples
}

// SetCallback sets a callback for real-time sample processing.
// The callback is invoked directly from the audio thread - it must be
// non-blocking and fast. Set before calling Start().
func (c *Capture) SetCallback(cb SampleCallback) {
	if cb == nil {
		c.callbackPtr.Store(nil)
	} else {
		c.callbackPtr.Store(&cb)
	}
}

// Init initializes the audio backend.
// Idempotent: a second call after successful initialisation returns nil.
// Returns ErrClosed if Close has already been called; a Capture cannot be reused
// after closing because the Samples channel and closeOnce are permanently spent.
func (c *Capture) Init() error {
	const op errors.Op = "audio.Capture.Init"

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() {
		return ErrClosed
	}

	if c.ctx != nil {
		return nil // already initialised — idempotent
	}

	ctxConfig := malgo.ContextConfig{}
	ctx, err := malgo.InitContext(nil, ctxConfig, nil)
	if err != nil {
		return errors.New(op).Err(err)
	}
	c.ctx = ctx

	return nil
}

// ListDevices returns available capture devices
func (c *Capture) ListDevices() ([]malgo.DeviceInfo, error) {
	const op errors.Op = "audio.Capture.ListDevices"

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return nil, ErrNotInitialized
	}

	infos, err := c.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	return infos, nil
}

// Start begins audio capture
func (c *Capture) Start(ctx context.Context) error {
	const op errors.Op = "audio.Capture.Start"

	// Use atomic swap to ensure only one caller can start
	if !c.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}

	// Validate and extract all context-dependent data while holding the lock
	c.mu.Lock()
	if c.ctx == nil {
		c.mu.Unlock()
		c.running.Store(false)
		return ErrNotInitialized
	}

	audioCtx := c.ctx.Context

	// Get device list while holding lock if we need a specific device
	var deviceID unsafe.Pointer
	if c.config.DeviceIndex >= 0 {
		devices, err := c.ctx.Devices(malgo.Capture)
		if err != nil {
			c.mu.Unlock()
			c.running.Store(false)
			return errors.New(op).Err(err)
		}
		if c.config.DeviceIndex >= len(devices) {
			c.mu.Unlock()
			c.running.Store(false)
			return errors.New(op).Msgf("device index %d out of range (have %d devices)",
				c.config.DeviceIndex, len(devices))
		}
		deviceID = devices[c.config.DeviceIndex].ID.Pointer()
	}
	c.mu.Unlock()

	deviceConfig := malgo.DeviceConfig{
		DeviceType:         malgo.Capture,
		SampleRate:         c.config.SampleRate,
		PeriodSizeInFrames: c.config.BufferSize,
		Capture: malgo.SubConfig{
			Format:   malgo.FormatF32,
			Channels: c.config.Channels,
		},
	}

	if deviceID != nil {
		deviceConfig.Capture.DeviceID = deviceID
	}

	// Callback receives audio data
	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		if len(inputSamples) == 0 {
			return
		}

		// Zero-copy conversion: reinterpret byte slice as float32 slice
		samples := bytesAsFloat32(inputSamples)

		// Lock-free callback access using atomic pointer
		if cbPtr := c.callbackPtr.Load(); cbPtr != nil {
			(*cbPtr)(samples)
		}

		// For channel consumers, we must copy since the buffer is reused.
		// Check closed flag to prevent send on closed channel.
		if !c.closed.Load() {
			c.safeSend(copyFloat32Slice(samples))
		}
	}

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	device, err := malgo.InitDevice(audioCtx, deviceConfig, deviceCallbacks)
	if err != nil {
		c.running.Store(false)
		return errors.New(op).Err(err)
	}

	// Create an internal context so Stop/Close can terminate the watcher goroutine
	// even when the caller-supplied context is never cancelled (e.g. context.Background()).
	internalCtx, cancelInternal := context.WithCancel(context.Background())

	// Store device and cancel func together so Stop() can reach both.
	c.mu.Lock()
	c.device = device
	c.cancelInternal = cancelInternal
	c.mu.Unlock()

	if err := device.Start(); err != nil {
		c.mu.Lock()
		c.device.Uninit()
		c.device = nil
		c.cancelInternal = nil
		c.mu.Unlock()
		cancelInternal()
		c.running.Store(false)
		return errors.New(op).Err(err)
	}

	// Watch for either external context cancellation or an internal Stop/Close signal.
	go func() {
		select {
		case <-ctx.Done():
		case <-internalCtx.Done():
			return // stopped via Stop() or Close() directly — no further action needed
		}
		if err := c.Stop(); err != nil && !stderr.Is(err, ErrNotRunning) {
			c.config.Logger.WarnWith().Err(err).Msg("stop on context cancel")
		}
	}()

	return nil
}

// Stop stops audio capture
func (c *Capture) Stop() error {
	if !c.running.CompareAndSwap(true, false) {
		return ErrNotRunning
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.device != nil {
		if err := c.device.Stop(); err != nil {
			c.config.Logger.WarnWith().Err(err).Msg("device stop")
		}
		c.device.Uninit()
		c.device = nil
	}

	// Signal the context-watcher goroutine to exit so it doesn't leak when
	// Stop is called directly rather than via context cancellation.
	if c.cancelInternal != nil {
		c.cancelInternal()
		c.cancelInternal = nil
	}

	return nil
}

// Close releases all audio resources.
// It is safe to call concurrently with Stop or the context-cancellation goroutine.
func (c *Capture) Close() error {
	const op errors.Op = "audio.Capture.Close"

	// Set closed flag first to prevent any in-flight callbacks from sending
	// to the channel after we close it below.
	c.closed.Store(true)

	// Delegate device teardown to Stop, which owns the running CAS and is the
	// single authority for the running→false transition. ErrNotRunning is
	// expected when Close is called on an inactive capture.
	if err := c.Stop(); err != nil && !stderr.Is(err, ErrNotRunning) {
		c.config.Logger.WarnWith().Err(err).Msg("stop on close")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		if err := c.ctx.Uninit(); err != nil {
			return errors.New(op).Err(err)
		}
		c.ctx.Free()
		c.ctx = nil
	}

	// Safely close channel only once
	c.closeOnce.Do(func() {
		close(c.samples)
	})
	return nil
}

// IsRunning returns true if capture is active
func (c *Capture) IsRunning() bool {
	return c.running.Load()
}

// safeSend attempts to send samples to the channel without blocking.
// It recovers from panic if the channel is closed between the closed flag check
// and the actual send (TOCTOU race). This is a rare edge case that can occur
// during concurrent context cancellation and Close() calls.
func (c *Capture) safeSend(samples []float32) {
	defer func() {
		if r := recover(); r != nil {
			// Channel was closed between our check and send - this is expected
			// during shutdown and can be safely ignored
			_ = r
		}
	}()

	select {
	case c.samples <- samples:
	default:
		// Drop samples if channel is full (consumer too slow)
	}
}

// bytesAsFloat32 performs zero-copy conversion of a byte slice to float32 slice.
// WARNING: The returned slice shares memory with the input - do not use after
// the input buffer is reused or freed.
// WARNING: The byte slice must be 4-byte aligned. malgo callback buffers satisfy
// this requirement; do not call with arbitrary byte slices from other sources.
func bytesAsFloat32(data []byte) []float32 {
	if len(data) < BytesPerFloat32 {
		return nil
	}
	numSamples := len(data) / BytesPerFloat32
	return unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), numSamples)
}

// copyFloat32Slice creates a copy of a float32 slice.
// Used when samples need to outlive the audio callback.
func copyFloat32Slice(src []float32) []float32 {
	if src == nil {
		return nil
	}
	dst := make([]float32, len(src))
	copy(dst, src)
	return dst
}
