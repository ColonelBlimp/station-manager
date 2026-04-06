// internal/audio/playback.go
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

var (
	ErrPlaybackNotInitialized = stderr.New("audio playback not initialized")
	ErrPlaybackAlreadyPlaying = stderr.New("audio playback already playing")
	ErrPlaybackNotPlaying     = stderr.New("audio playback not playing")
	ErrPlaybackClosed         = stderr.New("audio playback closed")
)

// Playback handles audio playback of WAV files.
// A single Playback can play one file at a time; concurrent PlayFile calls return
// ErrPlaybackAlreadyPlaying. A Playback cannot be reused after Close().
type Playback struct {
	config Config

	ctx *malgo.AllocatedContext

	// playing is true while a PlayFile call is in progress.
	// CompareAndSwap in PlayFile prevents concurrent plays.
	playing atomic.Bool

	// closed is set permanently by Close() to prevent reuse after shutdown.
	closed atomic.Bool

	// mu protects ctx and cancelPlay.
	mu sync.Mutex

	// cancelPlay is set by PlayFile and called by Stop() or Close() to interrupt
	// in-progress playback. Nil when no playback is in progress.
	cancelPlay context.CancelFunc

	// wg tracks in-progress PlayFile calls so that Close() can wait for device
	// teardown to complete before uninitialising the malgo context.
	wg sync.WaitGroup
}

// NewPlayback creates a new Playback instance.
// If cfg.Logger is nil it defaults to a no-op logger.
// cfg.SampleRate and cfg.Channels are ignored — these are taken from each WAV file.
func NewPlayback(cfg Config) *Playback {
	if cfg.Logger == nil {
		cfg.Logger = logging.Noop()
	}
	return &Playback{config: cfg}
}

// Init initialises the audio backend.
// Returns ErrPlaybackClosed if Close has already been called.
func (p *Playback) Init() error {
	const op errors.Op = "audio.Playback.Init"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed.Load() {
		return ErrPlaybackClosed
	}
	if p.ctx != nil {
		return errors.New(op).Msg("already initialized")
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return errors.New(op).Err(err)
	}
	p.ctx = ctx
	return nil
}

// IsPlaying returns true if a PlayFile call is currently in progress.
func (p *Playback) IsPlaying() bool {
	return p.playing.Load()
}

// Stop interrupts any in-progress PlayFile, causing it to return immediately.
// Returns ErrPlaybackNotPlaying if nothing is playing.
func (p *Playback) Stop() error {
	p.mu.Lock()
	cancel := p.cancelPlay
	p.mu.Unlock()

	if cancel == nil {
		return ErrPlaybackNotPlaying
	}
	cancel()
	return nil
}

// Close releases all audio resources.
// It cancels any in-progress PlayFile and waits for device teardown before
// releasing the malgo context. Safe to call concurrently with Stop or PlayFile.
func (p *Playback) Close() error {
	const op errors.Op = "audio.Playback.Close"

	p.closed.Store(true)

	// Signal any running PlayFile to stop.
	p.mu.Lock()
	cancel := p.cancelPlay
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Wait for PlayFile to finish device teardown before freeing the context.
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		if err := p.ctx.Uninit(); err != nil {
			return errors.New(op).Err(err)
		}
		p.ctx.Free()
		p.ctx = nil
	}
	return nil
}

// PlayFile plays a WAV file to completion, blocking until the file finishes,
// the context is cancelled, or Stop/Close is called.
//
// The device is configured to match the WAV file's sample rate and channel count.
// cfg.SampleRate and cfg.Channels from the constructor are not used here.
//
// Returns ErrPlaybackAlreadyPlaying if another PlayFile is already in progress.
// Returns ErrPlaybackNotInitialized if Init has not been called.
// Returns ErrPlaybackClosed if Close has been called.
func (p *Playback) PlayFile(ctx context.Context, path string) error {
	const op errors.Op = "audio.Playback.PlayFile"

	// wg.Add before closed check so Close().wg.Wait() is guaranteed to observe
	// this goroutine if it races past the closed check.
	p.wg.Add(1)
	defer p.wg.Done()

	if p.closed.Load() {
		return ErrPlaybackClosed
	}

	if !p.playing.CompareAndSwap(false, true) {
		return ErrPlaybackAlreadyPlaying
	}
	defer p.playing.Store(false)

	// Validate and extract the malgo context under lock, with a second closed check
	// to cover the window between wg.Add and here.
	p.mu.Lock()
	if p.ctx == nil {
		p.mu.Unlock()
		return ErrPlaybackNotInitialized
	}
	if p.closed.Load() {
		p.mu.Unlock()
		return ErrPlaybackClosed
	}
	audioCtx := p.ctx.Context
	p.mu.Unlock()

	// Decode the WAV file before touching any audio hardware.
	wav, err := readWAV(path)
	if err != nil {
		return errors.New(op).Err(err)
	}

	// Create an internal play context that Stop() and Close() can cancel.
	playCtx, cancelPlay := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancelPlay = cancelPlay
	p.mu.Unlock()
	defer func() {
		cancelPlay()
		p.mu.Lock()
		p.cancelPlay = nil
		p.mu.Unlock()
	}()

	// Playback state — only accessed from the single audio callback thread.
	pos := 0
	done := make(chan struct{})
	var doneOnce sync.Once

	onSendFrames := func(outputSamples, _ []byte, _ uint32) {
		if len(outputSamples) < BytesPerFloat32 {
			return
		}
		// Zero-copy view of the output buffer as float32.
		// malgo guarantees 4-byte alignment for output buffers.
		out := unsafe.Slice((*float32)(unsafe.Pointer(&outputSamples[0])), len(outputSamples)/BytesPerFloat32)

		n := copy(out, wav.Samples[pos:])
		pos += n
		// Zero any frames beyond the end of the file.
		for i := n; i < len(out); i++ {
			out[i] = 0
		}
		if n < len(out) {
			// Consumed all samples — signal completion.
			doneOnce.Do(func() { close(done) })
		}
	}

	deviceConfig := malgo.DeviceConfig{
		DeviceType:         malgo.Playback,
		SampleRate:         wav.SampleRate,
		PeriodSizeInFrames: p.config.BufferSize,
		Playback: malgo.SubConfig{
			Format:   malgo.FormatF32,
			Channels: uint32(wav.Channels),
		},
	}
	if p.config.DeviceIndex >= 0 {
		// Non-default device selection would require listing devices here;
		// for now DeviceIndex is treated as a hint: only -1 (default) is honoured.
	}

	device, err := malgo.InitDevice(audioCtx, deviceConfig, malgo.DeviceCallbacks{Data: onSendFrames})
	if err != nil {
		return errors.New(op).Err(err)
	}
	defer func() {
		if err := device.Stop(); err != nil {
			p.config.Logger.WarnWith().Err(err).Msg("device stop on playback exit")
		}
		device.Uninit()
	}()

	if err := device.Start(); err != nil {
		return errors.New(op).Err(err)
	}

	select {
	case <-done:
		// Natural end-of-file.
	case <-ctx.Done():
		// Caller cancelled the context.
	case <-playCtx.Done():
		// Stop() or Close() was called.
	}

	return nil
}
