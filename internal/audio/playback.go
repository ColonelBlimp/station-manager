// internal/audio/playback.go
package audio

import (
	"context"
	stderr "errors"
	"sync"
	"sync/atomic"
	"time"
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

// ListDevices returns available playback devices.
// Init must be called first.
func (p *Playback) ListDevices() ([]malgo.DeviceInfo, error) {
	const op errors.Op = "audio.Playback.ListDevices"

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx == nil {
		return nil, ErrPlaybackNotInitialized
	}

	infos, err := p.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}
	return infos, nil
}

// IsPlaying returns true if a PlayFile call is currently in progress.
func (p *Playback) IsPlaying() bool {
	return p.playing.Load()
}

// Stop interrupts any in-progress PlayFile, causing it to return immediately.
// Returns ErrPlaybackNotPlaying if nothing is playing.
// If Stop is called in the sub-microsecond window between playing being set true
// and cancelPlay being registered (essentially impossible in practice), it returns
// nil without cancelling — IsPlaying() remains the authoritative "is it running" check.
func (p *Playback) Stop() error {
	if !p.playing.Load() {
		return ErrPlaybackNotPlaying
	}

	p.mu.Lock()
	cancel := p.cancelPlay
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
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

	// Create the play context immediately after the CAS so that Stop() can always
	// cancel when IsPlaying() is true. Previously cancelPlay was set only after
	// readWAV, leaving a window (as long as the file read) where IsPlaying()
	// returned true but Stop() returned ErrPlaybackNotPlaying.
	playCtx, cancelPlay := context.WithCancel(context.Background())
	p.mu.Lock()
	// Second closed check: cover the window between wg.Add and acquiring mu.
	if p.closed.Load() {
		p.mu.Unlock()
		cancelPlay()
		return ErrPlaybackClosed
	}
	if p.ctx == nil {
		p.mu.Unlock()
		cancelPlay()
		return ErrPlaybackNotInitialized
	}
	audioCtx := p.ctx.Context
	p.cancelPlay = cancelPlay
	p.mu.Unlock()
	defer func() {
		cancelPlay()
		p.mu.Lock()
		p.cancelPlay = nil
		p.mu.Unlock()
	}()

	// Decode the WAV file. Stop()/Close() may be called during this read.
	wav, err := readWAV(path)
	if err != nil {
		return errors.New(op).Err(err)
	}

	// If Stop() or Close() arrived during the WAV decode, bail now before
	// touching any audio hardware.
	if playCtx.Err() != nil {
		return nil
	}

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
		p.mu.Lock()
		devices, listErr := p.ctx.Devices(malgo.Playback)
		p.mu.Unlock()
		if listErr != nil {
			return errors.New(op).Err(listErr)
		}
		if p.config.DeviceIndex >= len(devices) {
			return errors.New(op).Msgf("device index %d out of range (have %d playback devices)",
				p.config.DeviceIndex, len(devices))
		}
		deviceConfig.Playback.DeviceID = devices[p.config.DeviceIndex].ID.Pointer()
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
		// All samples have been submitted to the hardware, but the audio data is
		// still in transit through PipeWire/ALSA/driver buffers before reaching
		// the DAC. device.Stop() cuts the stream immediately, so we must wait for
		// the pipeline to drain. PipeWire + ALSA can have 150–250ms total latency;
		// 500ms is a conservative but safe bound.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		case <-playCtx.Done():
		}
	case <-ctx.Done():
		// Caller cancelled the context.
	case <-playCtx.Done():
		// Stop() or Close() was called.
	}

	return nil
}
