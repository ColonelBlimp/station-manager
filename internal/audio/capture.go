// internal/audio/capture.go
package audio

import (
	"context"
	stderr "errors"
	"log"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ColonelBlimp/station-manager/internal/errors"
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
	DeviceIndex int    // -1 for default device
	SampleRate  uint32 // e.g., 48000
	Channels    uint32 // 1 for mono, 2 for stereo
	BufferSize  uint32 // frames per callback
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
	mu      sync.Mutex  // protects ctx and device

	// Atomic pointer for lock-free callback access in hot path
	callbackPtr atomic.Pointer[SampleCallback]

	// Output channel for audio samples (float32 normalized -1.0 to 1.0)
	Samples   chan []float32
	closeOnce sync.Once // ensures channel is closed only once
}

// New creates a new audio capture instance
func New(cfg Config) *Capture {
	return &Capture{
		config:  cfg,
		Samples: make(chan []float32, SampleChannelBufferSize),
	}
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
		return errors.New(op).Msg("already initialized")
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

	// Store device immediately so it can be cleaned up if Start() fails or panics
	c.mu.Lock()
	c.device = device
	c.mu.Unlock()

	if err := device.Start(); err != nil {
		c.mu.Lock()
		c.device.Uninit()
		c.device = nil
		c.mu.Unlock()
		c.running.Store(false)
		return errors.New(op).Err(err)
	}

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		if err := c.Stop(); err != nil && !stderr.Is(err, ErrNotRunning) {
			log.Printf("audio: stop on context cancel: %v", err)
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
			log.Printf("audio: device stop: %v", err)
		}
		c.device.Uninit()
		c.device = nil
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
		log.Printf("audio: stop on close: %v", err)
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
		close(c.Samples)
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
		}
	}()

	select {
	case c.Samples <- samples:
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
