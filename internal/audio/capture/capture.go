package capture

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
	// Provides buffering between the audio callback and the consumer.
	SampleChannelBufferSize = 64
	// BytesPerFloat32 is the number of bytes in a float32 sample.
	BytesPerFloat32 = 4
)

var (
	ErrNotInitialized = stderr.New("audio capture not initialized")
	ErrAlreadyRunning = stderr.New("audio capture already running")
	ErrNotRunning     = stderr.New("audio capture not running")
	ErrClosed         = stderr.New("audio capture closed")
)

// Config holds audio capture configuration.
type Config struct {
	// DeviceIndex selects a capture device. -1 means the system default.
	DeviceIndex int
	// SampleRate in Hz. FT8 canonical is 12000.
	SampleRate uint32
	// Channels: 1 for mono (FT8), 2 for stereo.
	Channels uint32
	// BufferSize is the frames-per-callback period requested from the
	// audio backend. Smaller = lower latency, higher syscall load.
	BufferSize uint32
	// Logger receives warn-level diagnostics from the capture lifecycle
	// (device-stop errors during teardown, context-cancel handler
	// errors). nil → logging.Noop().
	Logger logging.Logger
}

// DefaultConfig returns the FT8 canonical capture configuration:
// 12 kHz mono float32, default system device, 512-frame callback
// period (~43 ms at 12 kHz).
func DefaultConfig() Config {
	return Config{
		DeviceIndex: -1,
		SampleRate:  12000,
		Channels:    1,
		BufferSize:  512,
	}
}

// SampleCallback is called directly from the audio thread with new
// samples. Use for low-latency processing; the implementation MUST be
// non-blocking and fast.
//
// WARNING: the samples slice aliases an internal buffer that is reused
// on the next callback. Copy before retaining beyond the callback.
type SampleCallback func(samples []float32)

// Capture handles real-time audio sampling from an audio device.
type Capture struct {
	config  Config
	ctx     *malgo.AllocatedContext
	device  *malgo.Device
	running atomic.Bool
	closed  atomic.Bool  // prevents sends to closed channel
	dropped atomic.Int64 // counts chunks dropped by safeSend when channel is full
	mu      sync.Mutex   // protects ctx, device, and cancelInternal

	// cancelInternal cancels the per-Start internal context, signalling the
	// context-watcher goroutine to exit when Stop/Close is called directly
	// rather than via context cancellation.
	cancelInternal context.CancelFunc

	// Atomic pointer for lock-free callback access in the hot path.
	callbackPtr atomic.Pointer[SampleCallback]

	// samples is the internal send/close end of the output channel.
	// External consumers receive from it via the Samples() accessor.
	samples   chan []float32
	closeOnce sync.Once // ensures channel is closed only once
}

// New creates a new audio capture instance.
func New(cfg Config) *Capture {
	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}
	return &Capture{
		config:  cfg,
		samples: make(chan []float32, SampleChannelBufferSize),
	}
}

// Samples returns a receive-only channel that delivers audio sample
// buffers. Each buffer contains float32 samples normalised to
// [-1.0, 1.0]. The channel is closed when Close is called.
func (c *Capture) Samples() <-chan []float32 {
	return c.samples
}

// SetCallback sets a callback for real-time sample processing.
// The callback is invoked directly from the audio thread — it must be
// non-blocking and fast. Pass nil to clear a previously set callback.
func (c *Capture) SetCallback(cb SampleCallback) {
	if cb == nil {
		c.callbackPtr.Store(nil)
	} else {
		c.callbackPtr.Store(&cb)
	}
}

// Init initialises the audio backend. Idempotent: a second call after
// successful initialisation returns nil.
//
// Returns ErrClosed if Close has already been called; a Capture cannot
// be reused after closing because the Samples channel and closeOnce
// are permanently spent.
func (c *Capture) Init() error {
	const op errors.Op = "capture.Capture.Init"

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() {
		return ErrClosed
	}

	if c.ctx != nil {
		return nil
	}

	ctxConfig := malgo.ContextConfig{}
	ctx, err := malgo.InitContext(nil, ctxConfig, nil)
	if err != nil {
		return errors.New(op).WithErr(err)
	}
	c.ctx = ctx

	return nil
}

// ListDevices returns the available capture devices reported by the
// audio backend. Init must have been called first.
func (c *Capture) ListDevices() ([]malgo.DeviceInfo, error) {
	const op errors.Op = "capture.Capture.ListDevices"

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return nil, ErrNotInitialized
	}

	infos, err := c.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}

	return infos, nil
}

// Start begins audio capture. Non-blocking — returns as soon as the
// device is started. The capture runs until ctx is cancelled, Stop is
// called, or Close is called.
func (c *Capture) Start(ctx context.Context) error {
	const op errors.Op = "capture.Capture.Start"

	if !c.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}

	c.mu.Lock()
	if c.ctx == nil {
		c.mu.Unlock()
		c.running.Store(false)
		return ErrNotInitialized
	}

	audioCtx := c.ctx.Context

	var deviceID unsafe.Pointer
	if c.config.DeviceIndex >= 0 {
		devices, err := c.ctx.Devices(malgo.Capture)
		if err != nil {
			c.mu.Unlock()
			c.running.Store(false)
			return errors.New(op).WithErr(err)
		}
		if c.config.DeviceIndex >= len(devices) {
			c.mu.Unlock()
			c.running.Store(false)
			return errors.New(op).WithMsgf("device index %d out of range (have %d devices)",
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

	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		if len(inputSamples) == 0 {
			return
		}

		samples := bytesAsFloat32(inputSamples)

		if cbPtr := c.callbackPtr.Load(); cbPtr != nil {
			(*cbPtr)(samples)
		}

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
		return errors.New(op).WithErr(err)
	}

	internalCtx, cancelInternal := context.WithCancel(context.Background())

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
		return errors.New(op).WithErr(err)
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-internalCtx.Done():
			return
		}
		if err := c.Stop(); err != nil && !stderr.Is(err, ErrNotRunning) {
			c.config.Logger.WarnWith().Err(err).Msg("stop on context cancel")
		}
	}()

	return nil
}

// Stop stops audio capture without releasing the underlying audio
// context (Close releases the context). Returns ErrNotRunning if the
// capture is not currently active.
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

	if c.cancelInternal != nil {
		c.cancelInternal()
		c.cancelInternal = nil
	}

	return nil
}

// Close releases all audio resources. Safe to call concurrently with
// Stop or the context-cancellation goroutine.
func (c *Capture) Close() error {
	const op errors.Op = "capture.Capture.Close"

	c.closed.Store(true)

	if err := c.Stop(); err != nil && !stderr.Is(err, ErrNotRunning) {
		c.config.Logger.WarnWith().Err(err).Msg("stop on close")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		if err := c.ctx.Uninit(); err != nil {
			return errors.New(op).WithErr(err)
		}
		c.ctx.Free()
		c.ctx = nil
	}

	c.closeOnce.Do(func() {
		close(c.samples)
	})
	return nil
}

// IsRunning returns true if capture is active.
func (c *Capture) IsRunning() bool {
	return c.running.Load()
}

// DroppedChunks returns the total number of audio chunks dropped
// because the Samples channel was full (consumer too slow). A non-zero
// value indicates that audio data has been lost — typically because
// the consumer blocked during processing instead of draining the
// channel promptly.
func (c *Capture) DroppedChunks() int64 {
	return c.dropped.Load()
}

// safeSend attempts to send samples to the channel without blocking.
// Recovers from the rare TOCTOU panic that can occur if the channel
// is closed between the c.closed check and the actual send, which can
// happen during concurrent context cancellation and Close calls.
func (c *Capture) safeSend(samples []float32) {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()

	select {
	case c.samples <- samples:
	default:
		c.dropped.Add(1)
	}
}

// bytesAsFloat32 performs zero-copy conversion of a byte slice to a
// float32 slice.
//
// WARNING: the returned slice shares memory with the input — do not
// use after the input buffer is reused or freed.
// WARNING: the byte slice must be 4-byte aligned. malgo callback
// buffers satisfy this; do not call with arbitrary byte slices.
func bytesAsFloat32(data []byte) []float32 {
	if len(data) < BytesPerFloat32 {
		return nil
	}
	numSamples := len(data) / BytesPerFloat32
	return unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), numSamples)
}

// copyFloat32Slice creates a copy of a float32 slice. Used when
// samples need to outlive the audio callback.
func copyFloat32Slice(src []float32) []float32 {
	if src == nil {
		return nil
	}
	dst := make([]float32, len(src))
	copy(dst, src)
	return dst
}
